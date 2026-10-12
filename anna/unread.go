package anna

import (
	"sync"

	"github.com/SokolskyNikita/annas-mcp/anna/internal/parse"
)

// maxUnreadSeen bounds how many logged examples a run remembers.
const maxUnreadSeen = 200

// unreadLog remembers which examples of unread page parts were logged, so a
// change Anna made for good is reported once per run rather than on every
// search.
type unreadLog struct {
	mu   sync.Mutex
	seen map[string]bool
}

// fresh keeps the examples in report that were not logged before, and
// remembers them.
func (l *unreadLog) fresh(report parse.Report) parse.Report {
	l.mu.Lock()
	defer l.mu.Unlock()
	return report.Filter(func(kind, sample string) bool {
		key := kind + "\x00" + sample
		if l.seen[key] {
			return false
		}
		if len(l.seen) < maxUnreadSeen {
			l.seen[key] = true
		}
		return true
	})
}

// logUnread logs, once per example, what the parsers could not read on a
// page. The call goes on with what was read. The log line names the kind of
// page and its number, never the search or the account.
func (c *Client) logUnread(page string, pageNumber int, report parse.Report) {
	if report.Empty() {
		return
	}
	fresh := c.unread.fresh(report)
	if fresh.Empty() {
		return
	}
	fields := []any{"page", page, "unread", fresh.String()}
	if pageNumber > 0 {
		fields = append(fields, "page_number", pageNumber)
	}
	c.log.Warn("Anna's Archive sent a page with parts this version could not read; check for a newer release", fields...)
}
