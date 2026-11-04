package openalex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const defaultBaseURL = "https://api.openalex.org"
const maxResponseBytes = 16 << 20

type Config struct {
	APIKey     string
	HTTPClient *http.Client
	BaseURL    string
}
type Client struct {
	key       string
	http      *http.Client
	base      string
	configErr error
}
type Error struct {
	Code       string `json:"code"`
	Reason     string `json:"reason"`
	Message    string `json:"message"`
	StatusCode int    `json:"status_code,omitempty"`
	RetryAfter int    `json:"retry_after,omitempty"`
	Retryable  bool   `json:"retryable,omitempty"`
	Err        error  `json:"-"`
}

func (e *Error) Error() string     { return e.Message }
func (e *Error) Unwrap() error     { return e.Err }
func (e *Error) ErrorCode() string { return e.Code }
func NewClient(config Config) *Client {
	base := strings.TrimRight(config.BaseURL, "/")
	if base == "" {
		base = defaultBaseURL
	}
	var configErr error
	endpoint, parseErr := url.Parse(base)
	if parseErr != nil || endpoint.Host == "" || (endpoint.Scheme != "https" && endpoint.Scheme != "http") || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" || endpoint.Path != "" {
		configErr = &Error{Code: "CONFIG", Reason: "invalid_endpoint", Message: "OpenAlex API endpoint must be an HTTP or HTTPS origin"}
	}
	client := config.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	copied := *client
	previous := client.CheckRedirect
	copied.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many OpenAlex redirects")
		}
		if len(via) > 0 && (!strings.EqualFold(req.URL.Host, via[0].URL.Host) || !strings.EqualFold(req.URL.Scheme, via[0].URL.Scheme)) {
			req.Header.Del("Authorization")
			return errors.New("OpenAlex API redirected outside its origin")
		}
		if previous != nil {
			return previous(req, via)
		}
		return nil
	}
	return &Client{key: strings.TrimSpace(config.APIKey), http: &copied, base: base, configErr: configErr}
}
func (c *Client) Lookup(ctx context.Context, id string) (*Work, error) {
	canonical, err := CanonicalID(id)
	if err != nil {
		return nil, err
	}
	var work Work
	if err = c.get(ctx, "/works/"+canonical, nil, &work); err != nil {
		return nil, err
	}
	if _, err = CanonicalID(work.ID); err != nil || !IsWorkID(work.ID) {
		return nil, &Error{Code: "UPSTREAM", Reason: "invalid_response", Message: "OpenAlex returned a work without a valid ID"}
	}
	if doi := NormalizeDOI(id); doi != "" && !work.HasDOI(doi) {
		return nil, &Error{Code: "UPSTREAM", Reason: "identity_mismatch", Message: "OpenAlex returned a different DOI"}
	}
	return &work, nil
}
func (c *Client) Search(ctx context.Context, options SearchOptions) (SearchResult, error) {
	values, err := searchValues(options)
	if err != nil {
		return SearchResult{}, err
	}
	var result SearchResult
	err = c.get(ctx, "/works", values, &result)
	if result.Results == nil {
		result.Results = []Work{}
	}
	return result, err
}
func (c *Client) Status(ctx context.Context) (*Status, error) {
	var status Status
	if err := c.get(ctx, "/rate-limit", nil, &status); err != nil {
		return nil, err
	}
	redactStatus(map[string]any(status), c.key)
	return &status, nil
}

func redactStatus(values map[string]any, key string) {
	for name, value := range values {
		lower := strings.ToLower(name)
		if lower == "key" || lower == "api_key" || lower == "authorization" || strings.Contains(lower, "token") || strings.Contains(lower, "secret") || strings.Contains(lower, "password") {
			delete(values, name)
			continue
		}
		values[name] = redactStatusValue(value, key)
	}
}

func redactStatusValue(value any, key string) any {
	switch typed := value.(type) {
	case string:
		return strings.ReplaceAll(typed, key, "[redacted]")
	case map[string]any:
		redactStatus(typed, key)
	case []any:
		for i, item := range typed {
			typed[i] = redactStatusValue(item, key)
		}
	}
	return value
}
func (c *Client) get(ctx context.Context, route string, values url.Values, target any) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if c.configErr != nil {
		return c.configErr
	}
	if c.key == "" {
		return &Error{Code: "CONFIG", Reason: "missing_key", Message: "set OPENALEX_API_KEY to use OpenAlex"}
	}
	parsed, err := url.Parse(c.base)
	if err != nil {
		return &Error{Code: "CONFIG", Reason: "invalid_endpoint", Message: "invalid OpenAlex API endpoint"}
	}
	parsed.Path = route
	parsed.RawQuery = values.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return &Error{Code: "UPSTREAM", Reason: "request_failed", Message: "could not create OpenAlex request", Err: err}
	}
	req.Header.Set("Authorization", "Bearer "+c.key)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "annas-mcp")
	response, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return &Error{Code: "UPSTREAM", Reason: "request_failed", Message: "OpenAlex request failed", Retryable: true, Err: err}
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return responseError(response, c.key)
	}
	limited := io.LimitReader(response.Body, maxResponseBytes+1)
	raw, err := io.ReadAll(limited)
	if err != nil {
		return &Error{Code: "UPSTREAM", Reason: "invalid_response", Message: "could not read OpenAlex response", Err: err}
	}
	if len(raw) > maxResponseBytes {
		return &Error{Code: "UPSTREAM", Reason: "response_too_large", Message: "OpenAlex response exceeded the size limit"}
	}
	if err = json.Unmarshal(raw, target); err != nil {
		return &Error{Code: "UPSTREAM", Reason: "invalid_response", Message: "OpenAlex returned invalid JSON", Err: err}
	}
	return nil
}
func responseError(response *http.Response, key string) error {
	raw, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
	body := strings.ToLower(strings.ReplaceAll(string(raw), key, "[redacted]"))
	result := &Error{Code: "UPSTREAM", Reason: "provider_error", StatusCode: response.StatusCode, Message: fmt.Sprintf("OpenAlex returned HTTP %d", response.StatusCode)}
	switch response.StatusCode {
	case http.StatusBadRequest:
		result.Code = "INVALID_ARGUMENT"
		result.Reason = "invalid_request"
		result.Message = "OpenAlex rejected the query or filters"
	case http.StatusNotFound:
		result.Code = "NOT_FOUND"
		result.Reason = "work_not_found"
		result.Message = "OpenAlex could not find this work"
	case http.StatusUnauthorized, http.StatusForbidden:
		result.Code = "CONFIG"
		result.Reason = "invalid_key"
		result.Message = "OpenAlex rejected OPENALEX_API_KEY"
	case http.StatusTooManyRequests:
		result.Reason = "rate_limited"
		result.Message = "OpenAlex temporarily limited requests"
		result.Retryable = true
		if strings.Contains(body, "budget") || strings.Contains(body, "daily") || strings.Contains(body, "credits") || strings.Contains(body, "allowance") {
			result.Reason = "budget_exhausted"
			result.Message = "OpenAlex daily allowance is exhausted"
			result.Retryable = false
		}
		if result.Reason == "rate_limited" && (strings.Contains(body, "upgrade") || strings.Contains(body, "paid plan") || strings.Contains(body, "premium plan")) {
			result.Reason = "plan_required"
			result.Message = "OpenAlex requires a different plan for this operation"
			result.Retryable = false
		}
	default:
		result.Retryable = response.StatusCode >= 500
	}
	result.RetryAfter = parseRetryAfter(response.Header.Get("Retry-After"))
	return result
}
func parseRetryAfter(value string) int {
	if n, err := strconv.Atoi(value); err == nil && n > 0 {
		return n
	}
	if until, err := http.ParseTime(value); err == nil {
		seconds := int(time.Until(until).Seconds())
		if seconds > 0 {
			return seconds
		}
	}
	return 0
}
