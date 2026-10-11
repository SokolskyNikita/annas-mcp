package anna

import (
	"context"
	"strings"
	"time"

	"github.com/SokolskyNikita/annas-mcp/internal/anna/parse"
	"github.com/SokolskyNikita/annas-mcp/internal/apperr"
)

// AccountStatus is the signed-in account's membership and fast download
// allowance. Anna's Archive counts fast downloads over a rolling window of
// WindowHours, and downloading a file again within it is free.
type AccountStatus struct {
	Member         bool     `json:"member"`
	DownloadsLeft  int      `json:"downloads_left"`
	DownloadsLimit int      `json:"downloads_limit,omitempty"`
	WindowHours    int      `json:"window_hours,omitempty"`
	Memberships    []string `json:"memberships,omitempty"`
}

// AccountStatus reads the account page, which shows active memberships and
// "Fast downloads used (last 18 hours): 4 / 125".
func (c *Client) AccountStatus(ctx context.Context, timeout time.Duration) (*AccountStatus, error) {
	ctx, cancel := operationContext(ctx, timeout)
	defer cancel()
	if err := c.requireArchiveAccess(ctx); err != nil {
		return nil, err
	}
	var status *AccountStatus
	err := c.onMirror(ctx, func(base string) error {
		var err error
		status, err = c.accountStatusOn(ctx, base)
		return err
	})
	return status, err
}

func (c *Client) accountStatusOn(ctx context.Context, base string) (*AccountStatus, error) {
	page, err := c.fetchAccountPage(ctx, base)
	if err != nil {
		return nil, err
	}
	status := &AccountStatus{
		Member:         page.Limit > 0,
		DownloadsLimit: page.Limit,
		WindowHours:    page.WindowHours,
		Memberships:    page.Memberships,
	}
	if page.Limit > 0 {
		status.DownloadsLeft = max(0, page.Limit-page.Used)
		return status, nil
	}
	// No allowance line: the account has no membership, or the page changed.
	// The statistics endpoint answers either way.
	summary, err := c.fetchSummary(ctx, base, strings.Repeat("0", 32))
	if err != nil {
		return nil, err
	}
	status.Member, status.DownloadsLeft = summary.IsMember == 1, summary.DownloadsLeft
	return status, nil
}

// fetchAccountPage signs in again once if the archive no longer recognizes
// the session: the account page answers 200 either way.
func (c *Client) fetchAccountPage(ctx context.Context, base string) (parse.AccountPage, error) {
	accountURL := buildPathURL(base, "/account/")
	for renewed := false; ; renewed = true {
		cookie := c.sessionCookie()
		doc, _, err := c.fetchDocument(ctx, accountURL)
		if err != nil {
			return parse.AccountPage{}, err
		}
		page := parse.ParseAccountPage(doc)
		if page.SignedIn {
			return page, nil
		}
		if !renewed {
			retry, err := c.signIn(ctx, cookie)
			if err != nil {
				return parse.AccountPage{}, err
			}
			if retry {
				continue
			}
		}
		return parse.AccountPage{}, apperr.New(apperr.UpstreamBlocked, "Anna's Archive shows this session as signed out; check ANNAS_SECRET_KEY, or set ANNAS_ACCOUNT_COOKIE: "+manualCookieSteps)
	}
}
