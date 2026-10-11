package match

import "testing"

// nameCases are testdata/cases/names.json.
type nameCases struct {
	Comment    string `json:"comment"`
	PersonKeys []struct {
		Name string `json:"name"`
		Key  string `json:"key"`
	} `json:"person_keys"`
	Normalized []struct {
		Name       string `json:"name"`
		Normalized string `json:"normalized"`
	} `json:"normalized"`
	AuthorSets []struct {
		Expected []string `json:"expected"`
		Actual   []string `json:"actual"`
		Same     bool     `json:"same"`
	} `json:"author_sets"`
}

func TestPersonKey(t *testing.T) {
	t.Parallel()

	var cases nameCases
	loadCases(t, "names", &cases)
	for _, tc := range cases.PersonKeys {
		if got := personKey(tc.Name); got != tc.Key {
			t.Errorf("personKey(%q) = %q, want %q", tc.Name, got, tc.Key)
		}
	}
}

func TestNormalizePersonNameFoldsLatinAccents(t *testing.T) {
	t.Parallel()

	var cases nameCases
	loadCases(t, "names", &cases)
	for _, tc := range cases.Normalized {
		if got := normalizePersonName(tc.Name); got != tc.Normalized {
			t.Errorf("normalizePersonName(%q) = %q, want %q", tc.Name, got, tc.Normalized)
		}
	}
}

func TestSameAuthorSet(t *testing.T) {
	t.Parallel()

	var cases nameCases
	loadCases(t, "names", &cases)
	for _, tc := range cases.AuthorSets {
		if got := sameAuthorSet(tc.Expected, tc.Actual); got != tc.Same {
			t.Errorf("sameAuthorSet(%q, %q) = %v, want %v", tc.Expected, tc.Actual, got, tc.Same)
		}
	}
}
