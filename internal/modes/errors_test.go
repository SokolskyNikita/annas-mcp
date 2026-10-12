package modes

import (
	"errors"
	"io/fs"
	"strings"
	"testing"

	"github.com/SokolskyNikita/annas-mcp/anna"
)

func TestApplicationErrorAddsFieldGuidanceAndPreservesCause(t *testing.T) {
	cause := &anna.Error{Code: anna.CodeConfig, Field: "DownloadPath", Message: "cannot write", Err: fs.ErrPermission}
	err := applicationError(cause)
	if !errors.Is(err, fs.ErrPermission) || anna.CodeOf(err) != anna.CodeConfig || !strings.Contains(err.Error(), "ANNAS_DOWNLOAD_PATH") {
		t.Fatalf("folder guidance: %v", err)
	}
	// literal: the table maps SDK configuration fields to application settings.
	for field, setting := range map[string]string{"SessionFile": "ANNAS_MCP_CACHE_DIR", "BaseURL": "ANNAS_BASE_URL", "SecretKey": "ANNAS_SECRET_KEY", "AccountCookie": "ANNAS_ACCOUNT_COOKIE"} {
		err := applicationError(&anna.Error{Code: anna.CodeConfig, Field: field, Message: "invalid"})
		if !strings.Contains(err.Error(), setting) {
			t.Fatalf("missing %s guidance: %v", field, err)
		}
	}
	blocked := anna.NewError(anna.UpstreamBlocked, "blocked")
	if !strings.Contains(applicationError(blocked).Error(), "ANNAS_ACCOUNT_COOKIE") {
		t.Fatal("missing blocked access guidance")
	}
	ordinary := errors.New("ordinary")
	if applicationError(ordinary) != ordinary {
		t.Fatal("unexpected error replacement")
	}
}
