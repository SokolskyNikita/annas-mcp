package parse

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

// The parsers read every captured page completely.
func TestCapturedPagesHaveNothingUnread(t *testing.T) {
	t.Parallel()

	for _, page := range readManifest(t) {
		switch page.Kind {
		case "books", "articles", "metadata":
			doc := pageDocument(t, page.file())
			if report := CheckSearchPage(doc, SearchCards(doc)); !report.Empty() {
				t.Errorf("%s: %s", page.Name, report)
			}
		case "record":
			if report := CheckRecordDetails(ReadRecordDetails(pageDocument(t, page.file()))); !report.Empty() {
				t.Errorf("%s: %s", page.Name, report)
			}
		}
	}
}

func problemKinds(report Report) []string {
	kinds := make([]string, len(report.Problems))
	for i, problem := range report.Problems {
		kinds[i] = problem.Kind
	}
	return kinds
}

func TestCheckSearchPageNoticesWhatItCannotRead(t *testing.T) {
	t.Parallel()

	t.Run("an unknown metadata part", func(t *testing.T) {
		page := strings.Replace(string(readPage(t, "search_books.html.gz")), "📘\u00a0Book (non-fiction) · ", "📘\u00a0Book (non-fiction) · Ms. Word ½ · ", 1)
		doc := htmlDocument(t, page)
		report := CheckSearchPage(doc, SearchCards(doc))
		if len(report.Problems) != 1 || report.Problems[0].Kind != ProblemUnparsedMeta || report.Problems[0].Samples[0] != "Ms. Word ½" {
			t.Fatalf("report = %s", report)
		}
	})
	t.Run("a card that no longer parses", func(t *testing.T) {
		doc := pageDocument(t, "search_books.html.gz")
		doc.Find(".js-aarecord-list-outer div.max-w-full").First().Remove()
		report := CheckSearchPage(doc, SearchCards(doc))
		if kinds := problemKinds(report); strings.Join(kinds, "|") != ProblemDroppedCard+"|"+ProblemResultCount {
			t.Fatalf("report = %s", report)
		}
		if !strings.HasPrefix(report.Problems[0].Samples[0], "/md5/") || report.Problems[1].Samples[0] != "read 49, the page shows 50" {
			t.Fatalf("report = %s", report)
		}
	})
	t.Run("a grey-out without the issues line", func(t *testing.T) {
		doc := pageDocument(t, "search_books.html.gz")
		doc.Find("div.opacity-40").First().RemoveClass("opacity-40")
		report := CheckSearchPage(doc, SearchCards(doc))
		if kinds := problemKinds(report); len(kinds) != 1 || kinds[0] != ProblemIssueMismatch {
			t.Fatalf("report = %s", report)
		}
	})
	t.Run("a missing partial match", func(t *testing.T) {
		doc := pageDocument(t, "search_filtered_partial.html.gz")
		doc.Find(".js-partial-matches-show a.custom-a.block").First().Parent().Remove()
		report := CheckSearchPage(doc, SearchCards(doc))
		if len(report.Problems) != 1 || report.Problems[0].Samples[0] != "read 40, the page shows 41" {
			t.Fatalf("report = %s", report)
		}
	})
	if report := CheckSearchPage(nil, nil); !report.Empty() {
		t.Fatalf("no page: %s", report)
	}
}

func TestCheckRecordDetails(t *testing.T) {
	t.Parallel()

	report := CheckRecordDetails(RecordDetails{Meta: ParseCardMeta("English [en] · PDF · 1MB · 2020 · 📄 Journal article · odd/Part · lgli")})
	if len(report.Problems) != 1 || report.Problems[0].Samples[0] != "odd/Part" {
		t.Fatalf("report = %s", report)
	}
}

func TestReportLimits(t *testing.T) {
	t.Parallel()

	var report Report
	long := strings.Repeat("<div class=\"x\">\x00markup\t\n</div> ", 2000)
	for i := 0; i < 10; i++ {
		report.Add("first kind", string(rune('a'+i))+long)
	}
	report.Add("first kind", "a"+long)
	problem := report.Problems[0]
	if problem.Count != 11 || len(problem.Samples) != maxReportSamples {
		t.Fatalf("count = %d, samples = %d", problem.Count, len(problem.Samples))
	}
	for _, sample := range problem.Samples {
		if utf8.RuneCountInString(sample) > maxReportSampleRunes || !strings.HasSuffix(sample, "…") || strings.ContainsAny(sample, "\x00\t\n") {
			t.Fatalf("sample not cut or cleaned: %q", sample)
		}
	}
	for i := 0; i < 20; i++ {
		report.Add(fmt.Sprintf("kind %d", i), long)
	}
	if line := report.String(); len(line) > maxReportBytes || !utf8.ValidString(line) || !strings.HasSuffix(line, "…") {
		t.Fatalf("line of %d bytes: %q", len(line), line)
	}
	short := Report{}
	short.Add("kind", "  spaced \n out ")
	if got := short.String(); got != `kind: 1 (e.g. "spaced out")` {
		t.Fatalf("String = %q", got)
	}
	kept := report.Filter(func(kind, sample string) bool { return kind == "first kind" && strings.HasPrefix(sample, "b") })
	if len(kept.Problems) != 1 || len(kept.Problems[0].Samples) != 1 || kept.Problems[0].Count != 11 {
		t.Fatalf("filtered = %+v", kept)
	}
	if !(Report{}).Empty() || report.Empty() {
		t.Fatal("Empty is wrong")
	}
}
