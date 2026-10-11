package parse

import (
	"net/url"
	"testing"
)

type doiCases struct {
	Comment string
	Queries []struct {
		Query string
		DOI   bool
	}
	Suffixes         []string
	Wrapped          []string
	RepeatedPrefixes []string `json:"repeated_prefixes"`
	Valid            []struct {
		DOI   string
		Valid bool
	}
	ArxivIDs           []struct{ DOI, Want string } `json:"arxiv_ids"`
	NormalizedArxivIDs []struct{ In, Want string }  `json:"normalized_arxiv_ids"`
}

func loadDOICases(t *testing.T) doiCases {
	t.Helper()
	var cases doiCases
	loadCases(t, "doi", &cases)
	return cases
}

func TestIsDOIQueryRecognizesDOIForms(t *testing.T) {
	t.Parallel()

	for _, tc := range loadDOICases(t).Queries {
		if got := IsDOIQuery(tc.Query); got != tc.DOI {
			t.Errorf("IsDOIQuery(%q) = %v, want %v", tc.Query, got, tc.DOI)
		}
	}
}

func TestNormalizeDOIPreservesSuffixPunctuation(t *testing.T) {
	t.Parallel()

	cases := loadDOICases(t)
	if got := NormalizeDOI("https://doi.org/10.1000%2Fexample?utm_source=test#fragment"); got != "10.1000/example" {
		t.Fatalf("DOI URL was not decoded/cleaned: %q", got)
	}
	for _, suffix := range cases.Suffixes {
		want := "10.1000/example" + suffix
		for _, input := range []string{want + " ", "doi: " + want, "https://doi.org/" + url.PathEscape(want)} {
			got, valid := ParseDOI(input)
			if got != want || !valid {
				t.Errorf("DOI suffix changed: input=%q got=%q valid=%v want=%q", input, got, valid, want)
			}
		}
	}
	for _, input := range cases.Wrapped {
		got, valid := ParseDOI(input)
		if got != "10.1000/example(02)" || !valid || !IsDOIQuery(input) {
			t.Errorf("enclosing DOI wrapper was not recognized: input=%q got=%q valid=%v", input, got, valid)
		}
	}
}

func TestArxivIDRequiresCanonicalDOIPrefixAndPreservesSuffix(t *testing.T) {
	t.Parallel()

	for _, tc := range loadDOICases(t).ArxivIDs {
		if got := ArxivID(tc.DOI); got != tc.Want {
			t.Errorf("ArxivID(%q) = %q, want %q", tc.DOI, got, tc.Want)
		}
	}
}

func TestNormalizeArxivIDStripsOnlyExplicitNumericVersion(t *testing.T) {
	t.Parallel()

	for _, tc := range loadDOICases(t).NormalizedArxivIDs {
		if got := NormalizeArxivID(tc.In); got != tc.Want {
			t.Errorf("NormalizeArxivID(%q) = %q, want %q", tc.In, got, tc.Want)
		}
	}
}

func TestValidDOI(t *testing.T) {
	t.Parallel()

	for _, tc := range loadDOICases(t).Valid {
		if got := ValidDOI(tc.DOI); got != tc.Valid {
			t.Errorf("ValidDOI(%q) = %v, want %v", tc.DOI, got, tc.Valid)
		}
	}
	// A DOI URL that does not parse still loses its prefix.
	if got := NormalizeDOI("https://doi.org/10.1000/%zz"); got != "10.1000/%zz" {
		t.Errorf("NormalizeDOI kept the prefix: %q", got)
	}
	if got := NormalizeArxivID("arxiv.1706.03762v1"); got != "1706.03762" {
		t.Errorf("NormalizeArxivID = %q", got)
	}
}

func TestNormalizeDOIRemovesRepeatedPrefixes(t *testing.T) {
	t.Parallel()

	for _, input := range loadDOICases(t).RepeatedPrefixes {
		if doi, valid := ParseDOI(input); doi != "10.1000/x" || !valid {
			t.Errorf("ParseDOI(%q) = %q, %v", input, doi, valid)
		}
	}
}
