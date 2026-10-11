package parse

import (
	"net/url"
	"testing"
)

func TestIsDOIQueryRecognizesDOIForms(t *testing.T) {
	t.Parallel()

	tests := map[string]bool{
		"10.1000/example":                          true,
		" DOI:10.1000/example ":                    true,
		"https://doi.org/10.1000/example":          true,
		"https://dx.doi.org/10.1000/example":       true,
		"attention is all you need":                false,
		"doi.org/10.1000/example without protocol": false,
	}
	for query, want := range tests {
		if got := IsDOIQuery(query); got != want {
			t.Errorf("IsDOIQuery(%q) = %v, want %v", query, got, want)
		}
	}
}

func TestNormalizeDOIPreservesSuffixPunctuation(t *testing.T) {
	t.Parallel()

	if got := NormalizeDOI("https://doi.org/10.1000%2Fexample?utm_source=test#fragment"); got != "10.1000/example" {
		t.Fatalf("DOI URL was not decoded/cleaned: %q", got)
	}
	for _, suffix := range []string{"(02)", "(02).", ").", ".", ",", ";", ":", "]", "}", ">", "'", "\"", "?", "#"} {
		want := "10.1000/example" + suffix
		for _, input := range []string{want + " ", "doi: " + want, "https://doi.org/" + url.PathEscape(want)} {
			got, valid := ParseDOI(input)
			if got != want || !valid {
				t.Errorf("DOI suffix changed: input=%q got=%q valid=%v want=%q", input, got, valid, want)
			}
		}
	}
	for _, input := range []string{"<https://doi.org/10.1000/example(02)>", "\"10.1000/example(02)\"", "doi: <10.1000/example(02)>", "(10.1000/example(02))", "doi: https://doi.org/10.1000/example(02)"} {
		got, valid := ParseDOI(input)
		if got != "10.1000/example(02)" || !valid || !IsDOIQuery(input) {
			t.Errorf("enclosing DOI wrapper was not recognized: input=%q got=%q valid=%v", input, got, valid)
		}
	}
}

func TestArxivIDRequiresCanonicalDOIPrefixAndPreservesSuffix(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		doi, want string
	}{
		{"10.48550/arXiv.1706.03762", "1706.03762"},
		{"https://doi.org/10.48550/arXiv.1706.03762v3", "1706.03762"},
		{"10.48550/arXiv.1706.03762.", "1706.03762."},
		{"10.1234/example.arxiv.1706.03762", ""},
		{"10.48550/other.1706.03762", ""},
		{"prefix-10.48550/arxiv.1706.03762", ""},
		{"10.48550/arxiv.", ""},
	} {
		if got := ArxivID(test.doi); got != test.want {
			t.Errorf("arxivID(%q) = %q, want %q", test.doi, got, test.want)
		}
	}
}

func TestNormalizeArxivIDStripsOnlyExplicitNumericVersion(t *testing.T) {
	t.Parallel()

	for input, want := range map[string]string{
		"arxiv:1706.03762v2":       "1706.03762",
		"1706.03762.":              "1706.03762.",
		"notarxiv.1706.03762":      "notarxiv.1706.03762",
		"10.1234/arxiv.1706.03762": "10.1234/arxiv.1706.03762",
	} {
		if got := NormalizeArxivID(input); got != want {
			t.Errorf("normalizeArxivID(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestValidDOI(t *testing.T) {
	t.Parallel()

	for doi, want := range map[string]bool{
		"10.1000/example": true, "10.48550/arXiv.1706.03762": true, "10.123/short-registrant": false,
		"10.1000/": false, "11.1000/x": false, "10.1000/with space": false, "": false,
	} {
		if got := ValidDOI(doi); got != want {
			t.Errorf("ValidDOI(%q) = %v, want %v", doi, got, want)
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

	for _, input := range []string{"doi:doi:10.1000/x", "https://doi.org/doi:10.1000/x", "<https://doi.org/https://dx.doi.org/10.1000/x>"} {
		if doi, valid := ParseDOI(input); doi != "10.1000/x" || !valid {
			t.Errorf("ParseDOI(%q) = %q, %v", input, doi, valid)
		}
	}
}
