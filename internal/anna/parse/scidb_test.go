package parse

import (
	"net/url"
	"testing"
)

// sciDBErrors names the errors a case in testdata/cases/scidb.json may
// expect.
var sciDBErrors = map[string]error{"": nil, "empty": ErrSciDBEmpty, "page_url": ErrSciDBPageURL, "no_file": ErrSciDBNoFile, "no_record": ErrSciDBNoRecord}

func TestSciDBFile(t *testing.T) {
	t.Parallel()

	var cases struct {
		Comment string
		PageURL string `json:"page_url"`
		Pages   []struct {
			Name, HTML, File, Hash, Error string
		}
		InvalidPageURLs []string `json:"invalid_page_urls"`
	}
	loadCases(t, "scidb", &cases)
	for _, test := range cases.Pages {
		t.Run(test.Name, func(t *testing.T) {
			want, known := sciDBErrors[test.Error]
			if !known {
				t.Fatalf("unknown error %q", test.Error)
			}
			file, hash, err := SciDBFile(htmlDocument(t, test.HTML), cases.PageURL)
			if err != want || file != test.File || hash != test.Hash {
				t.Fatalf("got %q, %q, %v; want %q, %q, %v", file, hash, err, test.File, test.Hash, want)
			}
		})
	}
	page := pageDocument(t, "scidb_equity_premium.html.gz")
	for _, pageURL := range cases.InvalidPageURLs {
		if _, _, err := SciDBFile(page, pageURL); err != ErrSciDBPageURL {
			t.Errorf("%s: err = %v, want %v", pageURL, err, ErrSciDBPageURL)
		}
	}
	if _, _, err := SciDBFile(nil, cases.PageURL); err != ErrSciDBEmpty {
		t.Errorf("no page: err = %v, want %v", err, ErrSciDBEmpty)
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
