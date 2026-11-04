package anna

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/SokolskyNikita/annas-mcp/anna/internal/parse"
)

// AccountStatus is the signed-in account's membership and fast download
// allowance. Anna's Archive counts fast downloads over a rolling window of
// WindowHours, and downloading a file again within it is free.
// SearchAllowed says whether a membership allows searching, which needs Lucky
// Librarian or higher; it is nil when the page does not show the tiers.
type AccountStatus struct {
	Member         bool     `json:"member"`
	DownloadsLeft  int      `json:"downloads_left"`
	DownloadsLimit int      `json:"downloads_limit,omitempty"`
	WindowHours    int      `json:"window_hours,omitempty"`
	Memberships    []string `json:"memberships,omitempty"`
	SearchAllowed  *bool    `json:"search_allowed,omitempty"`
}

// searchTier is the lowest membership tier allowed to search: Lucky
// Librarian. Brilliant Bookworm, tier 2, can only download.
const searchTier = 3

// searchAllowed reads the membership tiers; it is nil when any is unknown.
func searchAllowed(tiers []int) *bool {
	if len(tiers) == 0 {
		return nil
	}
	allowed := false
	for _, tier := range tiers {
		if tier <= 0 {
			return nil
		}
		allowed = allowed || tier >= searchTier
	}
	return &allowed
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
		SearchAllowed:  searchAllowed(page.Tiers),
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
		return parse.AccountPage{}, NewError(UpstreamBlocked, "Anna's Archive shows this session as signed out; check SecretKey, or set AccountCookie: "+manualCookieSteps)
	}
}

// explainSearchRefusal replaces a 403 on a search with the reason when the
// account page shows that no membership allows searching. Any other error,
// or a failure to read the account page, leaves err as it was.
func (c *Client) explainSearchRefusal(ctx context.Context, err error) error {
	var forbidden *forbiddenError
	if !errors.As(err, &forbidden) || ctx.Err() != nil {
		return err
	}
	var page parse.AccountPage
	if c.onMirror(ctx, func(base string) error {
		var accountErr error
		page, accountErr = c.fetchAccountPage(ctx, base)
		return accountErr
	}) != nil {
		return err
	}
	if allowed := searchAllowed(page.Tiers); allowed == nil || *allowed {
		return err
	}
	return &Error{Code: UpstreamBlocked, Field: "Membership", Message: "Anna's Archive refused the search because this account's membership (" + strings.Join(page.Memberships, "; ") +
		") can download but not search. Searching needs Lucky Librarian or higher, which skips the browser checks that searches otherwise get. Downloads by file hash or /md5/ page URL still work"}
}
