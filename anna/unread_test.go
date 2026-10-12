package anna

import (
	"bytes"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/SokolskyNikita/annas-mcp/anna/internal/parse"
)

func TestUnreadLogReportsEachExampleOnce(t *testing.T) {
	t.Parallel()

	log := &unreadLog{seen: map[string]bool{}}
	var report parse.Report
	report.Add(parse.ProblemUnparsedMeta, "KFX")
	report.Add(parse.ProblemUnparsedMeta, "Ms. Word")
	if fresh := log.fresh(report); len(fresh.Problems) != 1 || len(fresh.Problems[0].Samples) != 2 {
		t.Fatalf("first report: %+v", fresh)
	}
	if fresh := log.fresh(report); !fresh.Empty() {
		t.Fatalf("logged the same examples twice: %+v", fresh)
	}
	report.Add(parse.ProblemUnparsedMeta, "LIT")
	if fresh := log.fresh(report); len(fresh.Problems) != 1 || fmt.Sprint(fresh.Problems[0].Samples) != "[LIT]" {
		t.Fatalf("a new example: %+v", fresh)
	}
	// The log remembers a bounded number of examples.
	for i := range 2 * maxUnreadSeen {
		var many parse.Report
		many.Add(parse.ProblemDroppedCard, fmt.Sprintf("/md5/%032x", i))
		log.fresh(many)
	}
	if len(log.seen) != maxUnreadSeen {
		t.Fatalf("remembered %d examples", len(log.seen))
	}
	mustClient(Config{}).logUnread("search", 1, parse.Report{})
}

func TestUnreadDiagnosticsBelongToEachClient(t *testing.T) {
	var first, second bytes.Buffer
	a := mustClient(Config{Logger: slog.New(slog.NewJSONHandler(&first, nil))})
	b := mustClient(Config{Logger: slog.New(slog.NewJSONHandler(&second, nil))})
	var report parse.Report
	report.Add(parse.ProblemUnparsedMeta, "KFX")
	a.logUnread("search", 1, report)
	a.logUnread("search", 1, report)
	b.logUnread("search", 1, report)
	if strings.Count(first.String(), "KFX") != 1 || strings.Count(second.String(), "KFX") != 1 {
		t.Fatalf("diagnostics shared across clients: %s / %s", &first, &second)
	}
}
