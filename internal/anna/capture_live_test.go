package anna

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/SokolskyNikita/annas-mcp/internal/anna/parse"
	"github.com/joho/godotenv"
)

// TestCapturePages refreshes parse/testdata/pages from the live archive. It
// signs in with ANNAS_SECRET_KEY and reads search, record and SciDB pages as
// that account, but downloads no files, so it uses no fast downloads.
// ANNAS_MCP_CAPTURE=1 captures every page in the manifest; a comma-separated
// list of names captures only those. Afterwards, run TestCapturedPages in
// package parse with -update and review what changed.
func TestCapturePages(t *testing.T) {
	selection := os.Getenv("ANNAS_MCP_CAPTURE")
	if selection == "" {
		t.Skip("set ANNAS_MCP_CAPTURE=1, or to page names, to refresh parse/testdata/pages from Anna's Archive")
	}
	wanted := map[string]bool{}
	if selection != "1" {
		for _, name := range strings.Split(selection, ",") {
			wanted[strings.TrimSpace(name)] = true
		}
	}
	key := liveSetting("ANNAS_SECRET_KEY")
	if key == "" {
		t.Fatal("provide ANNAS_SECRET_KEY, in the environment or the repository .env, to capture pages")
	}
	client := NewClient(Config{BaseURL: "https://annas-archive.gl", AutoBaseURL: true, SecretKey: key})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	if err := client.requireArchiveAccess(ctx); err != nil {
		t.Fatal(err)
	}
	accountID, err := captureAccountID(ctx, client)
	if err != nil {
		t.Fatal(err)
	}
	_, cookie, _ := strings.Cut(client.sessionCookie(), "=")
	secrets := []string{key, cookie, accountID}

	selected := len(wanted) > 0
	for _, page := range readManifest(t) {
		if selected && !wanted[page.Name] {
			continue
		}
		delete(wanted, page.Name)
		var body []byte
		var err error
		if page.Kind == "csl" {
			body, err = captureCSL(ctx, page.DOI)
		} else {
			body, err = captureArchivePage(ctx, client, page.Path)
		}
		if err != nil {
			t.Fatalf("%s: %v", page.Name, err)
		}
		body = scrubCapture(body, secrets)
		for _, secret := range secrets {
			if secret != "" && bytes.Contains(body, []byte(secret)) {
				t.Fatalf("%s still contains account data after scrubbing", page.Name)
			}
		}
		if err := writePage(filepath.Join("parse", "testdata", "pages", page.file()), body); err != nil {
			t.Fatal(err)
		}
		t.Logf("%-24s %8d bytes", page.Name, len(body))
	}
	for name := range wanted {
		t.Errorf("no page %q in the manifest", name)
	}
}

// liveSetting reads a setting from the environment, then the repository .env.
func liveSetting(name string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	dotenv, _ := godotenv.Read("../../.env")
	return dotenv[name]
}

var accountIDPattern = regexp.MustCompile(`Account ID:\s*([A-Za-z0-9]+)`)

// captureAccountID reads the account's id from the account page, so it can be
// removed from captured pages that show it.
func captureAccountID(ctx context.Context, client *Client) (string, error) {
	body, err := captureArchivePage(ctx, client, "/account/")
	if err != nil {
		return "", err
	}
	match := accountIDPattern.FindSubmatch(body)
	if match == nil {
		return "", errors.New("the account page shows no account id; is the session signed in?")
	}
	return string(match[1]), nil
}

// captureArchivePage fetches path from a working mirror. A redirect fails the
// capture: the manifest should name each page in its final form.
func captureArchivePage(ctx context.Context, client *Client, path string) ([]byte, error) {
	var body []byte
	err := client.onMirror(ctx, func(base string) error {
		requestClient := client.requestClient(0)
		requestClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		response, err := client.doGet(ctx, requestClient, buildPathURL(base, path))
		if err != nil {
			return err
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			return fmt.Errorf("%s answered HTTP %d", path, response.StatusCode)
		}
		body, err = io.ReadAll(response.Body)
		if err != nil {
			return err
		}
		doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
		if err != nil || !parse.IsArchivePage(doc) {
			return fmt.Errorf("%s did not answer with an Anna's Archive page", path)
		}
		return nil
	})
	return body, err
}

func captureCSL(ctx context.Context, doi string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, (&url.URL{Scheme: "https", Host: "doi.org", Path: "/" + doi}).String(), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/vnd.citationstyles.csl+json")
	response, err := (&http.Client{Timeout: 30 * time.Second}).Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("doi.org answered HTTP %d for %s", response.StatusCode, doi)
	}
	return io.ReadAll(response.Body)
}

// literal: a pattern is code, not data.
var sciDBViewerFile = regexp.MustCompile(`/pdfjs/web/viewer\.html\?file=https(?:%3A|:)(?://|%2F%2F)([^/%"'\s<>]+)`)

// scrubCapture removes account data and SciDB's signed file links, which
// work for anyone holding them until they expire. Both the viewer's file
// parameter and the plain download link point at the same signed URL.
func scrubCapture(body []byte, secrets []string) []byte {
	text := string(body)
	for _, secret := range secrets {
		if secret == "" {
			continue
		}
		for _, form := range []string{secret, url.QueryEscape(secret), url.PathEscape(secret)} {
			text = strings.ReplaceAll(text, form, "REDACTED")
		}
	}
	for _, match := range sciDBViewerFile.FindAllStringSubmatch(text, -1) {
		host := regexp.QuoteMeta(match[1])
		text = regexp.MustCompile(`https(?:%3A|:)(?://|%2F%2F)`+host+`[^"'\s<>]*`).ReplaceAllStringFunc(text, func(signed string) string {
			if strings.HasPrefix(signed, "https://") {
				return "https://files.invalid/scidb/paper.pdf"
			}
			return "https%3A//files.invalid/scidb/paper.pdf"
		})
	}
	return []byte(text)
}

// writePage saves a captured page: HTML gzipped, DOI.org's JSON as it came.
func writePage(path string, body []byte) error {
	if strings.HasSuffix(path, ".gz") {
		return writeGzip(path, body)
	}
	return os.WriteFile(path, body, 0o644)
}

// writeGzip writes a reproducible gzip file: the same page gives the same bytes.
func writeGzip(path string, body []byte) error {
	var buffer bytes.Buffer
	writer, err := gzip.NewWriterLevel(&buffer, gzip.BestCompression)
	if err != nil {
		return err
	}
	if _, err := writer.Write(body); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	return os.WriteFile(path, buffer.Bytes(), 0o644)
}
