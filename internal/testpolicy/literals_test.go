// Package testpolicy holds checks on the repository's own tests.
package testpolicy

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// maxInlineLength is the longest string a test may write inline, outside
// failure messages and subtest names. Test data longer than this, and any
// markup, JSON or text spanning lines, belongs in a testdata file.
const maxInlineLength = 60

// maxTableStrings is the most strings one composite literal may hold. A
// table of inputs and expected outputs belongs in a testdata file.
const maxTableStrings = 5

var (
	markup     = regexp.MustCompile(`<(?:[A-Za-z][A-Za-z0-9-]*|/[A-Za-z])[^<>]*>`)
	jsonObject = regexp.MustCompile(`^\s*(?:\{\s*"|\[\s*[\[{"])`)
)

// reporting are the calls whose string arguments are messages, not data:
// test failures and logs, subtest names, and errors a test helper returns.
// Calls into package flag are messages as well.
//
// literal: the names of the calls.
var reporting = map[string]bool{
	"Fatal": true, "Fatalf": true, "Error": true, "Errorf": true, "Log": true, "Logf": true,
	"Skip": true, "Skipf": true, "Run": true, "New": true, "Wrap": true,
}

// TestTestDataLivesInFiles keeps test data out of Go test files: page
// snippets, API answers, case tables and long strings go in testdata, where
// they can be read and updated without touching code. A literal the test
// is about, such as the one string a bug report quoted, can stay inline
// with a comment containing "literal:" and the reason, on its line or the
// line above.
func TestTestDataLivesInFiles(t *testing.T) {
	root := filepath.Join("..", "..")
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && (entry.Name() == "node_modules" || entry.Name() == "dist" || strings.HasPrefix(entry.Name(), ".")) && path != root {
			return filepath.SkipDir
		}
		if entry.IsDir() || !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		for _, problem := range inlineData(t, path) {
			t.Error(problem)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestOnlyCapturedPagesAndCorpusAreGzipped keeps test data readable: cases,
// fixtures and expected outputs are plain text, and only the captured HTML
// pages and the corpus, which are large, are gzipped.
func TestOnlyCapturedPagesAndCorpusAreGzipped(t *testing.T) {
	root := filepath.Join("..", "..")
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && (entry.Name() == "node_modules" || entry.Name() == "dist" || strings.HasPrefix(entry.Name(), ".")) && path != root {
			return filepath.SkipDir
		}
		slash := filepath.ToSlash(path)
		if entry.IsDir() || !strings.Contains(slash, "/testdata/") {
			return nil
		}
		page := strings.Contains(slash, "/testdata/pages/") && (strings.HasSuffix(slash, ".html") || strings.HasSuffix(slash, ".html.gz"))
		corpus := strings.Contains(slash, "/testdata/corpus/")
		zipped := strings.HasSuffix(slash, ".gz")
		switch {
		case (page || corpus) && !zipped:
			t.Errorf("%s: gzip captured pages and the corpus, as %s.gz", path, entry.Name())
		case zipped && !page && !corpus:
			t.Errorf("%s: keep test data as plain text; only captured HTML pages and the corpus are gzipped", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func inlineData(t *testing.T, path string) []string {
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, source, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	justified := map[int]bool{}
	for _, group := range file.Comments {
		for _, comment := range group.List {
			if strings.Contains(comment.Text, "literal:") {
				line := fset.Position(comment.Pos()).Line
				justified[line], justified[line+1] = true, true
			}
		}
	}
	messages := map[*ast.BasicLit]bool{}
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		// A flag's usage text is a message too.
		if pkg, ok := selector.X.(*ast.Ident); !reporting[selector.Sel.Name] && (!ok || pkg.Name != "flag") {
			return true
		}
		for i, arg := range call.Args {
			if selector.Sel.Name == "Run" && i > 0 {
				break
			}
			if lit, ok := arg.(*ast.BasicLit); ok {
				messages[lit] = true
			}
		}
		return true
	})
	var problems []string
	report := func(position token.Position, why, snippet string) {
		if len(snippet) > 50 {
			snippet = snippet[:50] + "…"
		}
		// literal: the message for a problem.
		problems = append(problems, position.String()+": test data "+why+"; move it to testdata or justify it with a literal: comment: "+strconv.Quote(snippet))
	}
	// Case tables: the outermost composite literals holding many strings.
	ast.Inspect(file, func(node ast.Node) bool {
		composite, ok := node.(*ast.CompositeLit)
		if !ok {
			return true
		}
		switch composite.Type.(type) {
		case *ast.ArrayType, *ast.MapType:
		default:
			return true
		}
		position := fset.Position(composite.Pos())
		if justified[position.Line] {
			return false
		}
		count := 0
		ast.Inspect(composite, func(inner ast.Node) bool {
			if lit, ok := inner.(*ast.BasicLit); ok && lit.Kind == token.STRING && !messages[lit] {
				count++
			}
			return true
		})
		if count > maxTableStrings {
			start := fset.Position(composite.Pos()).Offset
			line, _, _ := strings.Cut(string(source[start:]), "\n")
			report(position, "is a table of "+strconv.Itoa(count)+" strings", line)
			return false
		}
		return true
	})
	ast.Inspect(file, func(node ast.Node) bool {
		lit, ok := node.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING || messages[lit] {
			return true
		}
		value, err := strconv.Unquote(lit.Value)
		if err != nil {
			return true
		}
		position := fset.Position(lit.Pos())
		if justified[position.Line] {
			return true
		}
		var why string
		switch {
		case strings.Contains(strings.TrimSpace(value), "\n"):
			why = "spans lines"
		case markup.MatchString(value):
			why = "holds markup"
		case jsonObject.MatchString(value):
			why = "holds JSON"
		case len(value) > maxInlineLength:
			why = "is longer than " + strconv.Itoa(maxInlineLength) + " bytes"
		default:
			return true
		}
		report(position, why, strings.ReplaceAll(value, "\n", `\n`))
		return true
	})
	return problems
}
