package parse

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseCardMetaReadsEveryPart(t *testing.T) {
	t.Parallel()

	var cases struct {
		Comment string
		Cases   []struct {
			Line string
			Want CardMeta
		}
	}
	loadCases(t, "card_meta", &cases)
	tests := cases.Cases
	for _, tc := range tests {
		if got := ParseCardMeta(tc.Line); !reflect.DeepEqual(got, tc.Want) {
			t.Errorf("ParseCardMeta(%q)\n got %+v\nwant %+v", tc.Line, got, tc.Want)
		}
	}
}

func TestSnippetNormalizes(t *testing.T) {
	t.Parallel()

	text := strings.Repeat("long description ", 40)
	if got := Snippet(text); len(got) > 480 || strings.Contains(got, "  ") {
		t.Fatalf("snippet was not normalized: length=%d value=%q", len(got), got)
	}
}
