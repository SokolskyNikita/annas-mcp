package anna

import (
	"context"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
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

var (
	downloadsUsedPattern = regexp.MustCompile(`^\s*(\d+)\s*/\s*(\d+)\s*$`)
	firstNumberPattern   = regexp.MustCompile(`\d+`)
)

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
		Member:         page.limit > 0,
		DownloadsLimit: page.limit,
		WindowHours:    page.windowHours,
		Memberships:    page.memberships,
	}
	if page.limit > 0 {
		status.DownloadsLeft = max(0, page.limit-page.used)
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

type accountPage struct {
	signedIn    bool
	used, limit int
	windowHours int
	memberships []string
}

// fetchAccountPage signs in again once if the archive no longer recognizes
// the session: the account page answers 200 either way.
func (c *Client) fetchAccountPage(ctx context.Context, base string) (accountPage, error) {
	accountURL := buildPathURL(base, "/account/")
	for renewed := false; ; renewed = true {
		cookie := c.sessionCookie()
		doc, _, err := c.fetchDocument(ctx, accountURL)
		if err != nil {
			return accountPage{}, err
		}
		page := parseAccountPage(doc)
		if page.signedIn {
			return page, nil
		}
		if !renewed {
			retry, err := c.signIn(ctx, cookie)
			if err != nil {
				return accountPage{}, err
			}
			if retry {
				continue
			}
		}
		return accountPage{}, apperr.New(apperr.UpstreamBlocked, "Anna's Archive shows this session as signed out; check ANNAS_SECRET_KEY, or set ANNAS_ACCOUNT_COOKIE: "+manualCookieSteps)
	}
}

func parseAccountPage(doc *goquery.Document) accountPage {
	var page accountPage
	if doc == nil {
		return page
	}
	page.signedIn = doc.Find("a[href='/account/secret_key']").Length() > 0
	doc.Find("a[href='/account/downloaded']").EachWithBreak(func(_ int, link *goquery.Selection) bool {
		line := link.Parent()
		match := downloadsUsedPattern.FindStringSubmatch(line.Find("strong").First().Text())
		if match == nil {
			return true
		}
		page.used, _ = strconv.Atoi(match[1])
		page.limit, _ = strconv.Atoi(match[2])
		if hours := firstNumberPattern.FindString(ownText(line)); hours != "" {
			page.windowHours, _ = strconv.Atoi(hours)
		}
		return false
	})
	// Expired memberships stay listed with line-through.
	doc.Find("a[href^='/donate?tier=']").Each(func(_ int, link *goquery.Selection) {
		line := link.Parent()
		tier := strings.Join(strings.Fields(line.Find("strong").First().Text()), " ")
		if tier == "" || hasClass(line, "line-through") {
			return
		}
		text := strings.Join(strings.Fields(line.Clone().Find("a").Remove().End().Text()), " ")
		if i := strings.Index(text, tier); i >= 0 {
			text = text[i:]
		}
		page.memberships = append(page.memberships, text)
	})
	return page
}
