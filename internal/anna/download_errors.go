package anna

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/SokolskyNikita/annas-mcp/internal/apperr"
)

var diagnosticURLPattern = regexp.MustCompile(`https?://[^\s<>"']+`)

// Keep every attempt available to errors.Is/As without repeating nested
// "download failed" prefixes in the message exposed by the CLI and MCP.
type downloadAttemptsError struct {
	attempts []error
}

func (e *downloadAttemptsError) Error() string {
	lines := make([]string, 0, len(e.attempts))
	for _, err := range e.attempts {
		lines = append(lines, err.Error())
	}
	return "download failed:\n- " + strings.Join(lines, "\n- ")
}

func (e *downloadAttemptsError) Unwrap() []error { return e.attempts }

// Earlier failures remain inspectable, but must not override the final
// outcome's public code (for example, a 403 followed by a file-transfer 404).
func (e *downloadAttemptsError) ErrorCode() string {
	return apperr.CodeOf(e.attempts[len(e.attempts)-1])
}

func failedDownload(attempts []error, stop error) error {
	if stop != nil {
		if len(attempts) == 0 {
			return stop
		}
		if !errors.Is(attempts[len(attempts)-1], stop) {
			attempts = append(attempts, fmt.Errorf("operation stopped: %w", stop))
		}
	}
	if len(attempts) == 0 {
		return errors.New("download failed: no download servers were attempted")
	}
	return &downloadAttemptsError{attempts: attempts}
}

// Download URLs can contain account tokens in both their path and query.
// Diagnostics identify the host only, including after a redirect.
func requestHost(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Hostname() == "" {
		return "upstream server"
	}
	return parsed.Hostname()
}

func responseHost(response *http.Response, rawURL string) string {
	if response.Request != nil && response.Request.URL != nil {
		rawURL = response.Request.URL.String()
	}
	return requestHost(rawURL)
}

func httpResponseError(response *http.Response, rawURL string) error {
	message := strings.TrimSpace(fmt.Sprintf("HTTP status %d %s", response.StatusCode, http.StatusText(response.StatusCode)))
	message += " from " + responseHost(response, rawURL)
	switch response.StatusCode {
	case http.StatusNotFound, http.StatusGone:
		message += ": requested resource is unavailable on this server"
	case http.StatusUnauthorized, http.StatusForbidden:
		message += ": access denied"
	case http.StatusTooManyRequests:
		message += ": rate limited; try again later"
	default:
		if response.StatusCode >= 500 {
			message += ": server error; try again later"
		}
	}
	// Do not include response bodies: they are often HTML error pages and
	// can reflect credentials or signed URLs back into the tool result.
	return errors.New(message)
}

func requestFailure(err error, rawURL string) error {
	message := err.Error()
	var requestErr *url.Error
	if errors.As(err, &requestErr) {
		message = requestErr.Err.Error()
		if requestErr.URL != "" {
			rawURL = requestErr.URL
		}
	}
	message = diagnosticURLPattern.ReplaceAllStringFunc(message, requestHost)
	return &redactedError{message: "request to " + requestHost(rawURL) + " failed: " + message, cause: err}
}

func apiErrorMessage(message string) string {
	if strings.Contains(message, "<") && strings.Contains(message, ">") {
		return "API returned an HTML error message"
	}
	message = strings.Join(strings.Fields(message), " ")
	message = diagnosticURLPattern.ReplaceAllStringFunc(message, requestHost)
	const maxRunes = 240
	if runes := []rune(message); len(runes) > maxRunes {
		message = string(runes[:maxRunes]) + "…"
	}
	return message
}
