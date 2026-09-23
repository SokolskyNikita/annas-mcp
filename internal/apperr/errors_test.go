package apperr

import (
	"context"
	"errors"
	"fmt"
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
	} {
		if got := CodeOf(tc.err); got != tc.code {
			t.Errorf("%v: got %s want %s", tc.err, got, tc.code)
		}
	}
	if !errors.Is(Wrap(Upstream, "download failed", context.Canceled), context.Canceled) {
		t.Fatal("lost underlying cause")
	}
}
