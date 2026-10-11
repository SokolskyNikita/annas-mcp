package parse

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

// loadCases reads testdata/cases/<name>.json into cases. Unknown fields are
// an error, so a misspelled field in a case cannot pass unnoticed.
func loadCases(t testing.TB, name string, cases any) {
	t.Helper()
	data, err := readTestdata("cases/" + name + ".json")
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(cases); err != nil {
		t.Fatalf("cases/%s.json: %v", name, err)
	}
}

// snippet returns testdata/snippets/<name>, a fragment of a page or an API
// answer.
func snippet(t testing.TB, name string) string {
	t.Helper()
	data, err := readTestdata("snippets/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// snippetDocument parses testdata/snippets/<name>.
func snippetDocument(t testing.TB, name string) *goquery.Document {
	t.Helper()
	return htmlDocument(t, snippet(t, name))
}
