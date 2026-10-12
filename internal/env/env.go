// Package env reads the ANNAS_* environment variables and locates the file
// that keeps the signed-in session between runs.
package env

import (
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/SokolskyNikita/annas-mcp/anna"
)

// DefaultAnnasBaseURL is used when ANNAS_BASE_URL is unset, and as the
// fallback when automatic mirror selection fails.
const DefaultAnnasBaseURL = "annas-archive.gl"

// DefaultDownloadFolder is the folder in the user's home folder that
// downloads go to when ANNAS_DOWNLOAD_PATH is unset. A new folder there works
// on every platform and, unlike Desktop, Documents and Downloads, needs no
// macOS privacy permission.
const DefaultDownloadFolder = "annas-archive"

// Env is the normalized configuration. Credentials are excluded from JSON so
// the struct can be logged safely.
type Env struct {
	AnnasBaseURL  string `json:"annas_base_url"`
	AutoBaseURL   bool   `json:"auto_base_url"`
	SecretKey     string `json:"-"`
	AccountCookie string `json:"-"`
	DownloadPath  string `json:"-"`
	SessionFile   string `json:"-"`
}

// Load reads and normalizes configuration without doing network work. Search
// and download settings are returned together; callers validate the settings
// required by the operation they are about to perform.
func Load() (*Env, error) {
	config := &Env{
		SecretKey:    os.Getenv("ANNAS_SECRET_KEY"),
		DownloadPath: downloadPathSetting(),
		SessionFile:  sessionFile(),
	}
	var err error
	if config.AnnasBaseURL, err = baseURLSetting(); err != nil {
		return nil, err
	}
	if config.AutoBaseURL, err = autoBaseURLSetting(); err != nil {
		return nil, err
	}
	if config.AccountCookie, err = loadAccountCookieHeader(); err != nil {
		return nil, err
	}
	return config, nil
}

func baseURLSetting() (string, error) {
	raw := strings.TrimSpace(os.Getenv("ANNAS_BASE_URL"))
	if raw == "" {
		return DefaultAnnasBaseURL, nil
	}
	baseURL, err := anna.NormalizeBaseURL(raw)
	if err != nil {
		return "", anna.WrapError(anna.CodeConfig, "invalid ANNAS_BASE_URL", err)
	}
	return baseURL, nil
}

// autoBaseURLSetting defaults to true: the configured base URL is then only
// the fallback for automatic mirror selection.
func autoBaseURLSetting() (bool, error) {
	raw := strings.TrimSpace(os.Getenv("ANNAS_AUTO_BASE_URL"))
	if raw == "" {
		return true, nil
	}
	enabled, err := strconv.ParseBool(raw)
	if err != nil {
		return false, anna.WrapError(anna.CodeConfig, "invalid ANNAS_AUTO_BASE_URL", err)
	}
	return enabled, nil
}

// downloadPathSetting expands a leading ~, which MCP client configs pass
// through unexpanded, and defaults to DefaultDownloadFolder in the home
// folder. Without a home folder it returns the raw value, which downloads
// then reject as missing or relative.
func downloadPathSetting() string {
	raw := strings.TrimSpace(os.Getenv("ANNAS_DOWNLOAD_PATH"))
	tilde := raw == "~" || strings.HasPrefix(raw, "~/") || (runtime.GOOS == "windows" && strings.HasPrefix(raw, `~\`))
	if raw != "" && !tilde {
		return raw
	}
	home := homeDir()
	if home == "" {
		return raw
	}
	if raw == "" {
		return filepath.Join(home, DefaultDownloadFolder)
	}
	return filepath.Join(home, raw[1:])
}

// sessionFile is where the signed-in session is kept between runs. It sits in
// the npx launcher's cache directory, so ANNAS_MCP_CACHE_DIR moves both, and
// is empty when no directory can be determined.
func sessionFile() string {
	dir := os.Getenv("ANNAS_MCP_CACHE_DIR")
	if dir == "" {
		base, fallback := os.Getenv("XDG_CACHE_HOME"), ".cache"
		if runtime.GOOS == "windows" {
			base, fallback = os.Getenv("LOCALAPPDATA"), filepath.Join("AppData", "Local")
		}
		if base == "" {
			home := homeDir()
			if home == "" {
				return ""
			}
			base = filepath.Join(home, fallback)
		}
		dir = filepath.Join(base, "annas-mcp")
	}
	dir, err := filepath.Abs(dir)
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "session.json")
}

// homeDir falls back to the account database like Node's os.homedir(),
// because MCP clients may start servers without HOME.
func homeDir() string {
	if home, err := os.UserHomeDir(); err == nil {
		return home
	}
	if current, err := user.Current(); err == nil {
		return current.HomeDir
	}
	return ""
}

func loadAccountCookieHeader() (string, error) {
	cookie, err := anna.NormalizeAccountCookie(os.Getenv("ANNAS_ACCOUNT_COOKIE"))
	if err != nil {
		return "", anna.WrapError(anna.CodeConfig, "invalid ANNAS_ACCOUNT_COOKIE", err)
	}
	return cookie, nil
}
