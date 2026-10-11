package parse

import (
	"reflect"
	"testing"
)

type authorCases struct {
	Comment string
	Clean   []struct{ In, Want string }
	Split   []struct{ Values, Want []string }
}

func TestCleanAuthorsDropsAffiliations(t *testing.T) {
	t.Parallel()

	var cases authorCases
	loadCases(t, "authors", &cases)
	for _, tc := range cases.Clean {
		if got := CleanAuthors(tc.In); got != tc.Want {
			t.Errorf("CleanAuthors(%q) = %q, want %q", tc.In, got, tc.Want)
		}
	}
}

func TestSplitAuthors(t *testing.T) {
	t.Parallel()

	var cases authorCases
	loadCases(t, "authors", &cases)
	for _, tc := range cases.Split {
		if got := splitAuthors(tc.Values); !reflect.DeepEqual(got, tc.Want) {
			t.Errorf("splitAuthors(%q) = %q, want %q", tc.Values, got, tc.Want)
		}
	}
}
