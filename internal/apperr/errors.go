// Package apperr defines the stable error categories exposed by the CLI and MCP.
package apperr

import (
	"context"
	"errors"
	"net"
)

const (
	InvalidArgument = "INVALID_ARGUMENT"
	Config          = "CONFIG"
	NotFound        = "NOT_FOUND"
	UpstreamBlocked = "UPSTREAM_BLOCKED"
	RequestTimeout  = "REQUEST_TIMEOUT"
	Upstream        = "UPSTREAM"
)

// Error preserves a machine-readable code and an optional underlying cause.
type Error struct {
	Code    string
	Message string
	Err     error
}

func (e *Error) Error() string {
	if e.Err == nil {
		return e.Message
	}
	if e.Message == "" {
		return e.Err.Error()
	}
	return e.Message + ": " + e.Err.Error()
}

func (e *Error) Unwrap() error { return e.Err }

func New(code, message string) error { return &Error{Code: code, Message: message} }

func Wrap(code, message string, err error) error {
	return &Error{Code: code, Message: message, Err: err}
}

// CodeOf classifies causes, never user-controlled text such as DOI identifiers.
func CodeOf(err error) string {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return RequestTimeout
	}
	// Compound diagnostics may expose all retry causes while retaining the
	// final attempt's classification. Do this before walking those causes,
	// so an earlier network timeout or access failure does not mask the final
	// result. Cancellation of the overall operation still takes precedence.
	var classified interface{ ErrorCode() string }
	if errors.As(err, &classified) {
		return classified.ErrorCode()
	}
	var networkError net.Error
	if errors.As(err, &networkError) && networkError.Timeout() {
		return RequestTimeout
	}
	var coded *Error
	if errors.As(err, &coded) && coded.Code != "" {
		return coded.Code
	}
	return Upstream
}
