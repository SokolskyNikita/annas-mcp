package papers

import (
	"context"
	"errors"
	"strings"

	"github.com/SokolskyNikita/annas-mcp/anna"
	"github.com/SokolskyNikita/annas-mcp/internal/filetransfer"
	"github.com/SokolskyNikita/annas-mcp/internal/openalex"
)

type Client struct {
	config Config
	oa     *openalex.Client
	files  *filetransfer.Client
}

// Status is the provider's reported usage. Values keep the upstream units.
type Status = openalex.Status

// NewClient creates a coordinator without network requests. Provider credentials
// and download destinations are checked only by operations that need them.
func NewClient(config Config) (*Client, error) {
	config.OpenAlexAPIKey = strings.TrimSpace(config.OpenAlexAPIKey)
	files, err := filetransfer.New(filetransfer.Config{Directory: config.DownloadPath, CacheDir: config.CacheDir, APIKey: config.OpenAlexAPIKey, HTTPClient: config.ContentHTTPClient})
	if err != nil {
		return nil, err
	}
	return &Client{config: config, oa: openalex.NewClient(openalex.Config{APIKey: config.OpenAlexAPIKey, HTTPClient: config.HTTPClient}), files: files}, nil
}
func (c *Client) Status(ctx context.Context) (*Status, error) {
	s, err := c.oa.Status(ctx)
	return s, c.wrap(err, "openalex")
}
func (c *Client) lookup(ctx context.Context, id string) (*Work, error) {
	w, err := c.oa.Lookup(ctx, id)
	if err != nil {
		return nil, c.wrap(err, "openalex")
	}
	return mapWork(w, true), nil
}
func (c *Client) wrap(err error, provider string) error {
	if err == nil {
		return nil
	}
	var current *Error
	if errors.As(err, &current) {
		return err
	}
	out := &Error{Code: anna.CodeOf(err), Message: err.Error(), Provider: provider, Reason: "provider_error", Err: err}
	var remote *openalex.Error
	if errors.As(err, &remote) {
		out.Code = remote.Code
		out.Reason = remote.Reason
		out.Retryable = remote.Retryable
		out.RetryAfter = remote.RetryAfter
	}
	var transfer *filetransfer.Error
	if errors.As(err, &transfer) {
		out.Code = transfer.Code
		out.Reason = "file_transfer_failed"
		if transfer.Code == "LOCAL_IO" {
			out.Reason = "local_io"
		}
		if transfer.Code == "CONFIG" {
			out.Reason = "download_configuration"
		}
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		out.Code = anna.RequestTimeout
		out.Reason = "canceled"
		if errors.Is(err, context.DeadlineExceeded) {
			out.Reason = "deadline_exceeded"
		}
	}
	return out
}
func invalid(message string) error {
	return &Error{Code: anna.InvalidArgument, Reason: "invalid_argument", Message: message}
}
func provider(raw string) (string, error) {
	p := strings.ToLower(strings.TrimSpace(raw))
	if p == "" {
		p = "auto"
	}
	if p != "auto" && p != "anna" && p != "openalex" {
		return "", invalid("provider must be auto, anna or openalex")
	}
	return p, nil
}
func missingArchive() error {
	return &Error{Code: anna.CodeConfig, Provider: "anna", Reason: "missing_key", Message: "set ANNAS_SECRET_KEY or ANNAS_ACCOUNT_COOKIE to use Anna's Archive"}
}
func csv(raw string) []string {
	var result []string
	for _, s := range strings.Split(raw, ",") {
		if s = strings.TrimSpace(s); s != "" {
			result = append(result, s)
		}
	}
	return result
}
