package papers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

type readFixtureParagraph struct {
	Section string `json:"section"`
	Text    string `json:"text"`
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "read", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestExtractTEIBodyAndLegacyWrapper(t *testing.T) {
	var cases []struct {
		Name     string                 `json:"name"`
		Expected []readFixtureParagraph `json:"expected"`
		Error    bool                   `json:"error"`
	}
	if err := json.Unmarshal(readFixture(t, "extraction_cases.json"), &cases); err != nil {
		t.Fatal(err)
	}
	for _, testCase := range cases {
		t.Run(testCase.Name, func(t *testing.T) {
			actual, err := extractTEI(context.Background(), strings.NewReader(string(readFixture(t, testCase.Name))))
			if testCase.Error {
				if err == nil {
					t.Fatal("invalid or unsafe XML was accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			expected := make([]textParagraph, len(testCase.Expected))
			for i, paragraph := range testCase.Expected {
				expected[i] = textParagraph{section: paragraph.Section, text: paragraph.Text}
			}
			if !reflect.DeepEqual(actual, expected) {
				t.Fatalf("got %#v, want %#v", actual, expected)
			}
		})
	}
}

func TestExtractTEILimitsAndCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := extractTEI(ctx, strings.NewReader("")); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want cancellation", err)
	}
	// literal: build adversarial nesting rather than maintaining a huge fixture.
	nested := strings.Repeat("<x>", 257)
	if _, err := extractTEI(context.Background(), strings.NewReader(nested)); err == nil {
		t.Fatal("excessive XML nesting was accepted")
	}
	if _, err := extractTEI(context.Background(), strings.NewReader(strings.Repeat(" ", maxReadXMLBytes+1))); err == nil {
		t.Fatal("oversized structured text was accepted")
	}
}

func pagingParagraphs(t *testing.T) []textParagraph {
	t.Helper()
	var fixtures []readFixtureParagraph
	if err := json.Unmarshal(readFixture(t, "paragraphs.json"), &fixtures); err != nil {
		t.Fatal(err)
	}
	paragraphs := make([]textParagraph, len(fixtures))
	for i, paragraph := range fixtures {
		paragraphs[i] = textParagraph{section: paragraph.Section, text: paragraph.Text}
	}
	return paragraphs
}

func TestReadPagingPreservesUnicodeAndParagraphIdentity(t *testing.T) {
	paragraphs := pagingParagraphs(t)
	options := ReadOptions{ID: "W123", Limit: 8}
	actual := make([]string, len(paragraphs))
	for count := 0; ; count++ {
		if count > 20 {
			t.Fatal("continuation did not terminate")
		}
		passages, text, next, err := readPage("W123", "digest", paragraphs, options)
		if err != nil {
			t.Fatal(err)
		}
		if !utf8.ValidString(text) || utf8.RuneCountInString(text) > options.Limit {
			t.Fatalf("invalid or oversized passage %q", text)
		}
		for _, passage := range passages {
			actual[passage.Paragraph-1] += passage.Text
			if passage.Section != paragraphs[passage.Paragraph-1].section {
				t.Fatalf("unexpected section %q", passage.Section)
			}
		}
		if next == nil {
			break
		}
		if next.ID != options.ID || next.Limit != options.Limit {
			t.Fatalf("next arguments lost input context: %#v", next)
		}
		options = *next
	}
	for i, paragraph := range paragraphs {
		if actual[i] != paragraph.text {
			t.Fatalf("paragraph %d changed: %q", i, actual[i])
		}
	}
}

func TestReadPagingKeepsSmallParagraphsWhole(t *testing.T) {
	paragraphs := pagingParagraphs(t)
	passages, _, next, err := readPage("W123", "digest", paragraphs, ReadOptions{ID: "W123", Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(passages) != 1 || passages[0].Text != paragraphs[0].text || next == nil {
		t.Fatalf("unexpected paragraph page: %#v, %#v", passages, next)
	}
}

func TestReadContinuationRejectsDifferentWorkOrArtifact(t *testing.T) {
	paragraphs := pagingParagraphs(t)
	_, _, next, err := readPage("W123", "digest", paragraphs, ReadOptions{ID: "W123", Limit: 8})
	if err != nil || next == nil {
		t.Fatalf("no continuation: %v", err)
	}
	if _, _, _, err := readPage("W456", "digest", paragraphs, *next); err == nil {
		t.Fatal("continuation was accepted for a different work")
	}
	if _, _, _, err := readPage("W123", "changed", paragraphs, *next); err == nil {
		t.Fatal("continuation was accepted after artifact changed")
	}
	next.Continuation = base64.RawURLEncoding.EncodeToString([]byte("invalid"))
	if _, _, _, err := readPage("W123", "digest", paragraphs, *next); err == nil {
		t.Fatal("malformed continuation was accepted")
	}
}

func TestReadArtifactHashesAndRejectsUnsupportedFiles(t *testing.T) {
	actual, digest, err := readArtifact(context.Background(), filepath.Join("testdata", "read", "standard.xml"))
	if err != nil || len(actual) != 3 || len(digest) != 64 {
		t.Fatalf("artifact read failed: %v, %d paragraphs, %q", err, len(actual), digest)
	}
	if _, _, err := readArtifact(context.Background(), t.TempDir()); err == nil {
		t.Fatal("directory was accepted as an XML artifact")
	}
}

type readTransport func(*http.Request) (*http.Response, error)

func (transport readTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func readerClient(t *testing.T, metadata string, contentGets *int, metadataGets ...*int) *Client {
	t.Helper()
	client := &http.Client{Transport: readTransport(func(request *http.Request) (*http.Response, error) {
		var body []byte
		switch request.URL.Host {
		case "api.openalex.org":
			if len(metadataGets) != 0 {
				(*metadataGets[0])++
			}
			body = readFixture(t, metadata)
		case "content.openalex.org":
			(*contentGets)++
			if request.Header.Get("Authorization") != "Bearer test-key" {
				t.Error("hosted reading request was not authenticated")
			}
			body = readFixture(t, "standard.xml")
		default:
			return nil, fmt.Errorf("unexpected host %s", request.URL.Host)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header),
			Body: io.NopCloser(strings.NewReader(string(body))), ContentLength: int64(len(body)), Request: request}, nil
	})}
	reader, err := NewClient(Config{OpenAlexAPIKey: "test-key", DownloadPath: t.TempDir(), HTTPClient: client, ContentHTTPClient: client})
	if err != nil {
		t.Fatal(err)
	}
	return reader
}

func TestReadContinuesWithoutFetchingContentAgain(t *testing.T) {
	contentGets, metadataGets := 0, 0
	reader := readerClient(t, "work.json", &contentGets, &metadataGets)
	first, err := reader.Read(context.Background(), ReadOptions{ID: "W123", Limit: 8})
	if err != nil {
		t.Fatal(err)
	}
	if first.Source != "openalex_tei" || first.NextArgs == nil || len(first.Digest) != 64 || metadataGets != 1 {
		t.Fatalf("unexpected reading result or duplicate metadata: %#v, %d lookups", first, metadataGets)
	}
	second, err := reader.Read(context.Background(), *first.NextArgs)
	if err != nil {
		t.Fatal(err)
	}
	if first.Digest != second.Digest || second.Text == first.Text || contentGets != 1 || metadataGets != 2 {
		t.Fatalf("continuation repeated requests: %d content, %d metadata", contentGets, metadataGets)
	}
}

func TestReadLabelsAbstractFallbackWithoutDownloading(t *testing.T) {
	contentGets := 0
	reader := readerClient(t, "abstract.json", &contentGets)
	result, err := reader.Read(context.Background(), ReadOptions{ID: "W123"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Source != "abstract" || len(result.Warnings) == 0 || contentGets != 0 {
		t.Fatalf("abstract fallback was not explicit: %#v", result)
	}
	if result.Text != "Only the abstract is available." {
		t.Fatalf("unexpected abstract %q", result.Text)
	}
}

func TestReadRetainsLandingPageWhenNoTextExists(t *testing.T) {
	contentGets := 0
	reader := readerClient(t, "empty.json", &contentGets)
	_, err := reader.Read(context.Background(), ReadOptions{ID: "W123"})
	var detail *Error
	if !errors.As(err, &detail) || detail.Reason != "no_structured_text" || detail.LandingPage == "" || contentGets != 0 {
		t.Fatalf("missing reading recovery evidence: %v", err)
	}
}

func TestReadRejectsInvalidArgumentsAndContinuation(t *testing.T) {
	contentGets, metadataGets := 0, 0
	reader := readerClient(t, "work.json", &contentGets, &metadataGets)
	_, err := reader.Read(context.Background(), ReadOptions{})
	var detail *Error
	if !errors.As(err, &detail) || detail.Code != "INVALID_ARGUMENT" {
		t.Fatalf("missing ID was accepted: %v", err)
	}
	_, err = reader.Read(context.Background(), ReadOptions{ID: "W123", Limit: maxReadCharacters + 1})
	if !errors.As(err, &detail) || detail.Code != "INVALID_ARGUMENT" {
		t.Fatalf("oversized limit was accepted: %v", err)
	}
	_, err = reader.Read(nil, ReadOptions{ID: "W123", Continuation: "malformed"})
	if !errors.As(err, &detail) || detail.Code != "INVALID_ARGUMENT" || contentGets != 0 || metadataGets != 0 {
		t.Fatalf("malformed continuation caused a request: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := reader.Read(ctx, ReadOptions{ID: "W123"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want cancellation", err)
	}
}
