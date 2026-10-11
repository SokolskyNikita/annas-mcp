package match

import (
	"testing"

	"github.com/SokolskyNikita/annas-mcp/internal/anna/parse"
)

func TestSameWork(t *testing.T) {
	t.Parallel()

	var file struct {
		Comment string `json:"comment"`
		Cases   []struct {
			Name     string         `json:"name"`
			DOI      string         `json:"doi"`
			Citation parse.Citation `json:"citation"`
			Record   parse.Identity `json:"record"`
			Want     bool           `json:"want"`
		} `json:"cases"`
	}
	loadCases(t, "same_work", &file)
	for _, tc := range file.Cases {
		if got := SameWork(tc.DOI, tc.Citation, tc.Record); got != tc.Want {
			t.Errorf("%s: SameWork = %v, want %v\ncitation: %+v\nrecord: %+v", tc.Name, got, tc.Want, tc.Citation, tc.Record)
		}
	}
}

func TestSameYear(t *testing.T) {
	t.Parallel()

	var file struct {
		Comment string `json:"comment"`
		Cases   []struct {
			DOI  string `json:"doi"`
			Want string `json:"want"`
			Got  string `json:"got"`
			Same bool   `json:"same"`
		} `json:"cases"`
	}
	loadCases(t, "years", &file)
	for _, tc := range file.Cases {
		if got := sameYear(tc.DOI, tc.Want, tc.Got); got != tc.Same {
			t.Errorf("sameYear(%q, %q, %q) = %v", tc.DOI, tc.Want, tc.Got, got)
		}
	}
}

func TestNormalizeTitle(t *testing.T) {
	t.Parallel()

	var file struct {
		Comment string `json:"comment"`
		Cases   []struct {
			Title      string `json:"title"`
			Normalized string `json:"normalized"`
		} `json:"cases"`
	}
	loadCases(t, "titles", &file)
	for _, tc := range file.Cases {
		if got := NormalizeTitle(tc.Title); got != tc.Normalized {
			t.Errorf("NormalizeTitle(%q) = %q, want %q", tc.Title, got, tc.Normalized)
		}
	}
}
