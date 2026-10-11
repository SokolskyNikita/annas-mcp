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

	"github.com/SokolskyNikita/annas-mcp/internal/apperr"
	"github.com/SokolskyNikita/annas-mcp/internal/mirror"
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
	baseURL, err := mirror.ParseBaseURL(raw)
	if err != nil {
		return "", apperr.Wrap(apperr.Config, "invalid ANNAS_BASE_URL", err)
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
		return false, apperr.Wrap(apperr.Config, "invalid ANNAS_AUTO_BASE_URL", err)
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

const accountCookieName = "aa_account_id2"

func loadAccountCookieHeader() (string, error) {
	raw := strings.Trim(strings.TrimSpace(os.Getenv("ANNAS_ACCOUNT_COOKIE")), `"'`)
	if raw == "" {
		return "", nil
	}
	if hasInvalidCookieChars(raw) {
		return "", apperr.New(apperr.Config, "ANNAS_ACCOUNT_COOKIE contains invalid cookie characters")
	}

	// A semicolon unambiguously means that the value is a Cookie header. In
	// that form, require the session cookie rather than accidentally selecting
	// an unrelated cookie from the header.
	if strings.ContainsRune(raw, ';') {
		value, ok := cookieValue(raw, accountCookieName)
		if !ok {
			return "", apperr.New(apperr.Config, "ANNAS_ACCOUNT_COOKIE must be a raw aa_account_id2 value or a Cookie header containing aa_account_id2")
		}
		return accountCookieName + "=" + value, nil
	}

	// A single name=value pair is supported when it names the session cookie
	// directly. Values may themselves contain base64 padding (=).
	if strings.HasPrefix(raw, accountCookieName+"=") {
		value := strings.TrimPrefix(raw, accountCookieName+"=")
		if !validCookieValue(value) {
			return "", apperr.New(apperr.Config, "ANNAS_ACCOUNT_COOKIE contains an empty or invalid aa_account_id2 value")
		}
		return accountCookieName + "=" + value, nil
	}

	// A raw value is allowed to use base64 padding. If an equals sign appears
	// anywhere other than the end, the input looks like an unrelated
	// name=value pair and is rejected instead of being sent as a cookie value.
	if strings.ContainsRune(raw, '=') && !isPaddedRawCookieValue(raw) {
		return "", apperr.New(apperr.Config, "ANNAS_ACCOUNT_COOKIE must be a raw aa_account_id2 value or a Cookie header containing aa_account_id2")
	}
	if !validCookieValue(raw) {
		return "", apperr.New(apperr.Config, "ANNAS_ACCOUNT_COOKIE contains an empty or invalid aa_account_id2 value")
	}
	return accountCookieName + "=" + raw, nil
}

func cookieValue(header, name string) (string, bool) {
	if !strings.Contains(header, name+"=") {
		return "", false
	}
	for _, part := range strings.Split(header, ";") {
		key, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if ok && strings.TrimSpace(key) == name {
			value = strings.TrimSpace(value)
			if !validCookieValue(value) {
				return "", false
			}
			return value, true
		}
	}
	return "", false
}

func hasInvalidCookieChars(value string) bool {
	return strings.IndexFunc(value, func(r rune) bool {
		return r < 0x20 || r == 0x7f
	}) >= 0
}

func validCookieValue(value string) bool {
	if value == "" {
		return false
	}
	for i := 0; i < len(value); i++ {
		char := value[i]
		if char < 0x21 || char > 0x7e || char == '"' || char == ',' || char == ';' || char == '\\' {
			return false
		}
	}
	return true
}

func isPaddedRawCookieValue(value string) bool {
	payload := strings.TrimRight(value, "=")
	padding := len(value) - len(payload)
	if padding == 0 || padding > 2 || payload == "" || strings.ContainsRune(payload, '=') {
		return false
	}
	for _, r := range payload {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '+' && r != '/' && r != '-' && r != '_' {
			return false
		}
	}
	return true
}
