package parse

import (
	"net/url"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

func TestSciDBViewerExtraction(t *testing.T) {
	t.Parallel()
	const page = "https://annas.test/scidb/10.1000/example/"
	const hash = "0123456789abcdef0123456789abcdef"
	const file = "https://downloads.example/paper%20name.pdf?token=a%2Bb&part=1"
	frame := `<iframe src="/pdfjs/web/viewer.html?file=` + url.QueryEscape(file) + `"></iframe>`
	link := `<a href="/md5/` + hash + `">Archive record</a>`
	for _, test := range []struct {
		name, html string
		valid      bool
	}{
		{"current viewer", frame + link, true},
		{"duplicate links", frame + frame + link + link, true},
		{"unrelated PDF anchor", `<a href="` + file + `">PDF</a>` + link, false},
		{"foreign viewer", strings.Replace(frame, `src="/pdfjs`, `src="https://evil.example/pdfjs`, 1) + link, false},
		{"HTTP PDF", strings.Replace(frame, url.QueryEscape(file), url.QueryEscape("http://downloads.example/paper.pdf"), 1) + link, false},
		{"credential-bearing PDF", strings.Replace(frame, url.QueryEscape(file), url.QueryEscape("https://secret@downloads.example/paper.pdf"), 1) + link, false},
		{"protocol relative foreign PDF", strings.Replace(frame, url.QueryEscape(file), url.QueryEscape("//downloads.example/paper.pdf"), 1) + link, false},
		{"ambiguous PDFs", frame + strings.Replace(frame, "part%3D1", "part%3D2", 1) + link, false},
		{"missing checksum", frame, false},
		{"invalid checksum", frame + `<a href="/md5/not-a-hash">Record</a>`, false},
		{"ambiguous checksums", frame + link + strings.ReplaceAll(link, hash, "abcdef0123456789abcdef0123456789"), false},
		{"missing PDF", link, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(test.html))
			if err != nil {
				t.Fatal(err)
			}
			got, md5, err := SciDBFile(doc, page)
			if !test.valid {
				if err == nil {
					t.Fatalf("accepted invalid SciDB page: %q, %q", got, md5)
				}
				return
			}
			if err != nil || got != file || md5 != hash {
				t.Fatalf("got %q, %q, %v; want exactly one query decode", got, md5, err)
			}
		})
	}
}

func TestSciDBFileErrors(t *testing.T) {
	t.Parallel()

	page := pageDocument(t, "scidb_equity_premium.html.gz")
	for _, tc := range []struct {
		name, pageURL string
		doc           bool
		want          error
	}{
		{"no page", "https://annas.test/scidb/10.1002/jae.2939/", false, ErrSciDBEmpty},
		{"plain HTTP page", "http://annas.test/scidb/10.1002/jae.2939/", true, ErrSciDBPageURL},
		{"credentials in the page URL", "https://user@annas.test/scidb/10.1002/jae.2939/", true, ErrSciDBPageURL},
		{"unparsable page URL", "https://annas.test/%zz", true, ErrSciDBPageURL},
	} {
		doc := page
		if !tc.doc {
			doc = nil
		}
		if _, _, err := SciDBFile(doc, tc.pageURL); err != tc.want {
			t.Errorf("%s: err = %v, want %v", tc.name, err, tc.want)
		}
	}
	if _, _, err := SciDBFile(htmlDocument(t, `<a href="/md5/0123456789abcdef0123456789abcdef">record</a>`), "https://annas.test/scidb/x/"); err != ErrSciDBNoFile {
		t.Errorf("err = %v, want %v", err, ErrSciDBNoFile)
	}
	viewer := `<iframe src="/pdfjs/web/viewer.html?file=%2Fpaper.pdf"></iframe>`
	if file, _, err := SciDBFile(htmlDocument(t, viewer), "https://annas.test/scidb/x/"); err != ErrSciDBNoRecord || file != "" {
		t.Errorf("file = %q, err = %v, want %v", file, err, ErrSciDBNoRecord)
	}
	// A relative file link resolves on the page's own origin.
	file, hash, err := SciDBFile(htmlDocument(t, viewer+`<a href="/md5/0123456789ABCDEF0123456789ABCDEF">record</a>`), "https://annas.test/scidb/x/")
	if err != nil || file != "https://annas.test/paper.pdf" || hash != "0123456789abcdef0123456789abcdef" {
		t.Fatalf("relative file: %q %q %v", file, hash, err)
	}
	if _, _, err := SciDBFile(htmlDocument(t, `<iframe src="%zz"></iframe><iframe src="/pdfjs/web/viewer.html?file="></iframe>`), "https://annas.test/scidb/x/"); err != ErrSciDBNoFile {
		t.Errorf("unusable viewers: err = %v", err)
	}
}

func TestSciDBFileReadsCapturedPage(t *testing.T) {
	t.Parallel()

	file, hash, err := SciDBFile(pageDocument(t, "scidb_equity_premium.html.gz"), "https://annas-archive.gl/scidb/10.1002/jae.2939/")
	if err != nil || file != "https://files.invalid/scidb/paper.pdf" || hash != "eff1d22d32721d0fcc05d6c7e332bf54" {
		t.Fatalf("SciDB page: %q %q %v", file, hash, err)
	}
}

func TestMD5HashAndResolveFile(t *testing.T) {
	t.Parallel()

	if hash, ok := md5Hash(" 0123456789ABCDEF0123456789ABCDEF "); !ok || hash != "0123456789abcdef0123456789abcdef" {
		t.Errorf("md5Hash = %q %v", hash, ok)
	}
	for _, value := range []string{"0123", "zz23456789abcdef0123456789abcdef"} {
		if _, ok := md5Hash(value); ok {
			t.Errorf("md5Hash(%q) accepted", value)
		}
	}
	page, _ := url.Parse("https://annas.test/scidb/x/")
	for _, raw := range []string{"", "%zz", "https://other.example:bad/x.pdf"} {
		if file, ok := resolveFile(page, raw); ok {
			t.Errorf("resolveFile(%q) = %q", raw, file)
		}
	}
}
