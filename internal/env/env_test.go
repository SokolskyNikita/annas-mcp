package env

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// loadCases decodes testdata/cases/<name>.json into into. Unknown fields fail
// the test, so a typo in a case file cannot pass silently.
func loadCases(t testing.TB, name string, into any) {
	t.Helper()
	file := filepath.Join("testdata", "cases", name+".json")
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(into); err != nil {
		t.Fatalf("%s: %v", file, err)
	}
}

// setEnv sets every ANNAS_* variable Load reads, so a test never inherits
// values from the developer's shell. It returns the session file path.
func setEnv(t *testing.T, secret, downloads, baseURL, auto, cookie string) string {
	t.Helper()
	cacheDir := t.TempDir()
	t.Setenv("ANNAS_SECRET_KEY", secret)
	t.Setenv("ANNAS_DOWNLOAD_PATH", downloads)
	t.Setenv("ANNAS_BASE_URL", baseURL)
	t.Setenv("ANNAS_AUTO_BASE_URL", auto)
	t.Setenv("ANNAS_ACCOUNT_COOKIE", cookie)
	t.Setenv("ANNAS_MCP_CACHE_DIR", cacheDir)
	return filepath.Join(cacheDir, "session.json")
}

func TestLoadNormalizesSettings(t *testing.T) {
	var file struct {
		Comment string
		Cases   []struct {
			Name                          string
			Secret, Downloads, Base, Auto string
			Cookie                        string
			Want                          struct {
				BaseURL       string `json:"base_url"`
				AutoBaseURL   bool   `json:"auto_base_url"`
				SecretKey     string `json:"secret_key"`
				DownloadPath  string `json:"download_path"`
				AccountCookie string `json:"account_cookie"`
			}
		}
	}
	loadCases(t, "settings", &file)
	for _, tc := range file.Cases {
		t.Run(tc.Name, func(t *testing.T) {
			want := Env{
				AnnasBaseURL: tc.Want.BaseURL, AutoBaseURL: tc.Want.AutoBaseURL, SecretKey: tc.Want.SecretKey,
				DownloadPath: tc.Want.DownloadPath, AccountCookie: tc.Want.AccountCookie,
			}
			want.SessionFile = setEnv(t, tc.Secret, tc.Downloads, tc.Base, tc.Auto, tc.Cookie)
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)
			// Unset settings take their defaults. Search needs no download
			// settings, and downloads default to a folder in the home folder.
			if tc.Base == "" {
				want.AnnasBaseURL = DefaultAnnasBaseURL
			}
			if tc.Downloads == "" {
				want.DownloadPath = filepath.Join(home, DefaultDownloadFolder)
			}
			got, err := Load()
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if *got != want {
				t.Fatalf("Load = %+v, want %+v", *got, want)
			}
		})
	}
}

func TestLoadDownloadPathDefaultsToHomeAndExpandsTilde(t *testing.T) {
	setEnv(t, "", "", "", "", "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	absolute := filepath.Join(t.TempDir(), "books")
	// literal: the expected paths are built from this run's temporary home and download folders.
	cases := map[string]string{
		"":                   filepath.Join(home, DefaultDownloadFolder),
		"~":                  home,
		"~/Books":            filepath.Join(home, "Books"),
		" " + absolute + " ": absolute,
		"~other/Books":       "~other/Books",
	}
	for raw, want := range cases {
		t.Setenv("ANNAS_DOWNLOAD_PATH", raw)
		got, err := Load()
		if err != nil {
			t.Fatalf("Load with %q: %v", raw, err)
		}
		if got.DownloadPath != want {
			t.Errorf("ANNAS_DOWNLOAD_PATH=%q gave %q, want %q", raw, got.DownloadPath, want)
		}
	}
}

func TestLoadRejectsUnsafeBaseURLAndAutoFlag(t *testing.T) {
	t.Setenv("ANNAS_BASE_URL", "http://fallback.example")
	if _, err := Load(); err == nil {
		t.Fatal("expected HTTP base URL to fail")
	}

	t.Setenv("ANNAS_BASE_URL", "fallback.example")
	t.Setenv("ANNAS_AUTO_BASE_URL", "maybe")
	if _, err := Load(); err == nil {
		t.Fatal("expected malformed auto flag to fail")
	}

	t.Setenv("ANNAS_AUTO_BASE_URL", "true")
	t.Setenv("ANNAS_ACCOUNT_COOKIE", "other_cookie=value")
	if _, err := Load(); err == nil {
		t.Fatal("expected a Cookie header without aa_account_id2 to fail")
	}
}

func TestLoadAccountCookieForms(t *testing.T) {
	var file struct {
		Comment string
		Cases   []struct{ Name, Raw, Want string }
	}
	loadCases(t, "account_cookies", &file)
	for _, tt := range file.Cases {
		t.Run(tt.Name, func(t *testing.T) {
			t.Setenv("ANNAS_ACCOUNT_COOKIE", tt.Raw)
			config, err := Load()
			if err != nil {
				t.Fatalf("Load returned error: %v", err)
			}
			if config.AccountCookie != tt.Want {
				t.Fatalf("expected %q, got %q", tt.Want, config.AccountCookie)
			}
		})
	}

	// literal: the CR LF that would start a second header is what the check must reject.
	t.Setenv("ANNAS_ACCOUNT_COOKIE", "bad\r\nCookie: injected")
	if _, err := Load(); err == nil {
		t.Fatal("expected unsafe cookie to be rejected")
	}

	t.Setenv("ANNAS_ACCOUNT_COOKIE", "aa_account_id2=")
	if _, err := Load(); err == nil {
		t.Fatal("expected empty account cookie to be rejected")
	}

	for _, raw := range []string{"token with spaces", "token,with,commas", "token\\with\\slashes", "токен"} {
		t.Setenv("ANNAS_ACCOUNT_COOKIE", raw)
		if _, err := Load(); err == nil {
			t.Fatalf("expected invalid cookie value %q to be rejected", raw)
		}
	}
}

func TestCookieValueDoesNotConfuseSimilarNames(t *testing.T) {
	if value, ok := cookieValue("not_aa_account_id2=wrong", accountCookieName); ok || value != "" {
		t.Fatalf("matched a similar cookie name: %q, %v", value, ok)
	}
	if _, ok := cookieValue("aa_account_id2=", accountCookieName); ok {
		t.Fatal("accepted empty account cookie")
	}
}

func TestSessionFileFollowsTheLauncherCacheDirectory(t *testing.T) {
	setEnv(t, "", "", "", "", "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("ANNAS_MCP_CACHE_DIR", "")
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("LOCALAPPDATA", "")
	want := filepath.Join(home, ".cache", "annas-mcp", "session.json")
	if runtime.GOOS == "windows" {
		want = filepath.Join(home, "AppData", "Local", "annas-mcp", "session.json")
	}
	if got := sessionFile(); got != want {
		t.Fatalf("default session file = %q, want %q", got, want)
	}

	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)
	t.Setenv("LOCALAPPDATA", cache)
	if got, want := sessionFile(), filepath.Join(cache, "annas-mcp", "session.json"); got != want {
		t.Fatalf("platform cache session file = %q, want %q", got, want)
	}
}
