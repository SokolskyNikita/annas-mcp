package env

import (
	"os"
	"strconv"
	"strings"

	"github.com/SokolskyNikita/annas-mcp/internal/apperr"
	"github.com/SokolskyNikita/annas-mcp/internal/mirror"
)

const DefaultAnnasBaseURL = "annas-archive.gl"

type Env struct {
	SecretKey     string `json:"-"`
	DownloadPath  string `json:"-"`
	AnnasBaseURL  string `json:"annas_base_url"`
	AccountCookie string `json:"-"`
	AutoBaseURL   bool   `json:"auto_base_url"`
}

// Load reads and normalizes configuration without doing network work. Search
// and download settings are returned together; callers validate the settings
// required by the operation they are about to perform.
func Load() (*Env, error) {
	baseURL := DefaultAnnasBaseURL
	rawBaseURL := strings.TrimSpace(os.Getenv("ANNAS_BASE_URL"))
	if rawBaseURL != "" {
		parsed, err := mirror.ParseBaseURL(rawBaseURL)
		if err != nil {
			return nil, apperr.Wrap(apperr.Config, "invalid ANNAS_BASE_URL", err)
		}
		baseURL = parsed
	}

	autoBaseURL := true
	rawAutoBaseURL := strings.TrimSpace(os.Getenv("ANNAS_AUTO_BASE_URL"))
	if rawAutoBaseURL != "" {
		parsed, err := strconv.ParseBool(rawAutoBaseURL)
		if err != nil {
			return nil, apperr.Wrap(apperr.Config, "invalid ANNAS_AUTO_BASE_URL", err)
		}
		autoBaseURL = parsed
	}

	accountCookie, err := loadAccountCookieHeader()
	if err != nil {
		return nil, err
	}

	return &Env{
		SecretKey:     os.Getenv("ANNAS_SECRET_KEY"),
		DownloadPath:  os.Getenv("ANNAS_DOWNLOAD_PATH"),
		AnnasBaseURL:  baseURL,
		AccountCookie: accountCookie,
		AutoBaseURL:   autoBaseURL,
	}, nil
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
