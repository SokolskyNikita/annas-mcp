package apperr

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"
)

func TestClassificationPreservesCausesAndIgnoresMessageText(t *testing.T) {
	for _, tc := range []struct {
		err  error
		code string
	}{
		{New(NotFound, "no paper for 10.1403/example"), NotFound},
		{errors.New("403 must be an invalid argument"), Upstream},
		{fmt.Errorf("wrapped: %w", New(Config, "missing path")), Config},
		{Wrap(Upstream, "download failed", context.Canceled), RequestTimeout},
		{fmt.Errorf("request failed: %w", &net.DNSError{IsTimeout: true}), RequestTimeout},
		{Wrap(Upstream, "lookup failed", &net.DNSError{IsNotFound: true}), Upstream},
		{New("", "unclassified error"), Upstream},
	} {
		if got := CodeOf(tc.err); got != tc.code {
			t.Errorf("%v: got %s want %s", tc.err, got, tc.code)
		}
	}
	if !errors.Is(Wrap(Upstream, "download failed", context.Canceled), context.Canceled) {
		t.Fatal("lost underlying cause")
	}
}

func TestWrappedErrorsRetainDiagnosticAndCause(t *testing.T) {
	cause := errors.New("connection reset")
	for _, message := range []string{"", "download failed"} {
		err := Wrap(Upstream, message, cause)
		want := cause.Error()
		if message != "" {
			want = message + ": " + want
		}
		if err.Error() != want || !errors.Is(err, cause) {
			t.Fatalf("incorrect wrapped error: %v", err)
		}
	}
}
