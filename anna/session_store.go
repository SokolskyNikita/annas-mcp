package anna

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// The session minted from the secret key is saved so later runs, including
// every npx launch, reuse it instead of signing in again. The file also
// remembers the configured cookie that session replaced, so an expired
// AccountCookie is skipped until the user sets a different one.
type savedSession struct {
	// KeySHA256 ties the session to one account: a different secret key
	// never picks up another account's session.
	KeySHA256 string `json:"key_sha256"`
	Cookie    string `json:"cookie"`
	// ReplacedSHA256 identifies the configured cookie the archive rejected.
	ReplacedSHA256 string `json:"replaced_cookie_sha256,omitempty"`
}

const maxSessionFileBytes = 64 << 10

func fingerprint(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

// restoreSession starts from the configured cookie unless the saved session
// already replaced that same cookie, and from the saved session otherwise.
func (c *Client) restoreSession() {
	saved, ok := c.loadSession()
	if !ok {
		return
	}
	switch {
	case c.configuredCookie == "":
		c.cookie = saved.Cookie
	case saved.ReplacedSHA256 == fingerprint(c.configuredCookie):
		c.cookie = saved.Cookie
		c.log.Warn("AccountCookie was rejected by Anna's Archive earlier, so the session signed in with SecretKey is used instead; AccountCookie can be removed")
	}
}

func (c *Client) loadSession() (savedSession, bool) {
	key := strings.TrimSpace(c.config.SecretKey)
	if c.config.SessionFile == "" || key == "" {
		return savedSession{}, false
	}
	file, err := os.Open(c.config.SessionFile)
	if err != nil {
		return savedSession{}, false
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxSessionFileBytes))
	var saved savedSession
	if err != nil || json.Unmarshal(data, &saved) != nil {
		return savedSession{}, false
	}
	if saved.KeySHA256 != fingerprint(key) || !strings.HasPrefix(saved.Cookie, accountCookieName+"=") || normalizeCookieHeader(saved.Cookie) != saved.Cookie {
		return savedSession{}, false
	}
	return saved, true
}

// saveSession is best effort: without a writable cache directory the next
// run simply signs in again.
func (c *Client) saveSession(cookie string) {
	if c.config.SessionFile == "" {
		return
	}
	saved := savedSession{KeySHA256: fingerprint(strings.TrimSpace(c.config.SecretKey)), Cookie: cookie}
	// A sign-in only happens once the configured cookie is out of use: the
	// archive rejected it, or a saved session already replaced it.
	if c.configuredCookie != "" {
		saved.ReplacedSHA256 = fingerprint(c.configuredCookie)
	}
	if err := writeSessionFile(c.config.SessionFile, saved); err != nil {
		c.log.Warn("Could not save the Anna's Archive session; the next run will sign in again", "error", err)
	}
}

func writeSessionFile(path string, saved savedSession) error {
	data, err := json.Marshal(saved)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	// CreateTemp makes the file readable only by its owner, and the rename
	// means servers running side by side never read half a file.
	temp, err := os.CreateTemp(dir, ".session-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(temp.Name(), path)
}
