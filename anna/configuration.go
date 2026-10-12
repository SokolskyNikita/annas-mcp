package anna

import (
	"strings"

	"github.com/SokolskyNikita/annas-mcp/anna/internal/mirror"
	"github.com/SokolskyNikita/annas-mcp/anna/internal/parse"
)

// NormalizeBaseURL validates an archive origin and returns its host.
func NormalizeBaseURL(value string) (string, error) {
	host, err := mirror.ParseBaseURL(value)
	if err != nil {
		return "", &Error{Code: CodeConfig, Message: "invalid archive base URL", Err: err, Field: "BaseURL"}
	}
	return host, nil
}

// IsDOIQuery reports whether a query represents a DOI.
func IsDOIQuery(value string) bool { return parse.IsDOIQuery(value) }

// NormalizeDOI removes a DOI URL or label prefix.
func NormalizeDOI(value string) string { return parse.NormalizeDOI(value) }

// ValidDOI reports whether a normalized value is a DOI.
func ValidDOI(value string) bool { return parse.ValidDOI(value) }

// NormalizeAccountCookie validates a raw session value or Cookie header.
func NormalizeAccountCookie(value string) (string, error) {
	raw := strings.Trim(strings.TrimSpace(value), `"'`)
	if raw == "" {
		return "", nil
	}
	if hasInvalidCookieChars(raw) {
		return "", NewError(CodeConfig, "AccountCookie contains invalid cookie characters")
	}

	// A semicolon unambiguously means that the value is a Cookie header. In
	// that form, require the session cookie rather than accidentally selecting
	// an unrelated cookie from the header.
	if strings.ContainsRune(raw, ';') {
		value, ok := cookieValue(raw, accountCookieName)
		if !ok {
			return "", NewError(CodeConfig, "AccountCookie must be a raw aa_account_id2 value or a Cookie header containing aa_account_id2")
		}
		return accountCookieName + "=" + value, nil
	}

	// A single name=value pair is supported when it names the session cookie
	// directly. Values may themselves contain base64 padding (=).
	if strings.HasPrefix(raw, accountCookieName+"=") {
		value := strings.TrimPrefix(raw, accountCookieName+"=")
		if !validCookieValue(value) {
			return "", NewError(CodeConfig, "AccountCookie contains an empty or invalid aa_account_id2 value")
		}
		return accountCookieName + "=" + value, nil
	}

	// A raw value is allowed to use base64 padding. If an equals sign appears
	// anywhere other than the end, the input looks like an unrelated
	// name=value pair and is rejected instead of being sent as a cookie value.
	if strings.ContainsRune(raw, '=') && !isPaddedRawCookieValue(raw) {
		return "", NewError(CodeConfig, "AccountCookie must be a raw aa_account_id2 value or a Cookie header containing aa_account_id2")
	}
	if !validCookieValue(raw) {
		return "", NewError(CodeConfig, "AccountCookie contains an empty or invalid aa_account_id2 value")
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
