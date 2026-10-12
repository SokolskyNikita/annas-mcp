package modes

import (
	"errors"
	"fmt"
	"io/fs"
	"runtime"

	"github.com/SokolskyNikita/annas-mcp/anna"
	"github.com/SokolskyNikita/annas-mcp/internal/env"
)

func applicationError(err error) error {
	var coded *anna.Error
	if errors.As(err, &coded) {
		switch coded.Field {
		case "DownloadPath":
			advice := "Set ANNAS_DOWNLOAD_PATH to an absolute folder you can write to."
			if errors.Is(err, fs.ErrPermission) && runtime.GOOS == "darwin" {
				advice = "On macOS, Desktop, Documents, Downloads and iCloud Drive need permission: allow your MCP client (for example Claude or Cursor) in System Settings > Privacy & Security > Files and Folders, or set ANNAS_DOWNLOAD_PATH to a folder directly in your home folder, such as ~/" + env.DefaultDownloadFolder + "."
			}
			return fmt.Errorf("%w. %s", err, advice)
		case "SessionFile":
			return fmt.Errorf("%w. Check ANNAS_MCP_CACHE_DIR", err)
		case "BaseURL":
			return fmt.Errorf("%w. Check ANNAS_BASE_URL", err)
		case "SecretKey", "AccountCookie":
			return fmt.Errorf("%w. Set ANNAS_SECRET_KEY from your Anna's Archive account page, or copy the browser's aa_account_id2 cookie into ANNAS_ACCOUNT_COOKIE and restart the MCP server. Details: https://github.com/SokolskyNikita/annas-mcp#signing-in", err)
		}
	}
	if anna.CodeOf(err) == anna.UpstreamBlocked {
		return fmt.Errorf("%w. Check ANNAS_SECRET_KEY and membership; if browser access works, copy aa_account_id2 into ANNAS_ACCOUNT_COOKIE and restart the MCP server", err)
	}
	return err
}
