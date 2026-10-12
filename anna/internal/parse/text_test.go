package parse

import "testing"

func TestCleanText(t *testing.T) {
	var cases struct {
		Comment string
		Cases   []struct{ In, Want, Note string }
	}
	loadCases(t, "text", &cases)
	for _, tc := range cases.Cases {
		if got := CleanText(tc.In); got != tc.Want {
			t.Errorf("CleanText(%q) = %q, want %q", tc.In, got, tc.Want)
		}
	}
}
