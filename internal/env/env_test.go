package env

import (
	"path/filepath"
	"runtime"
	"testing"
)

// setEnv sets every ANNAS_* variable Load reads, so a test never inherits
// values from the developer's shell. It returns the session file path.
func setEnv(t *testing.T, secret, downloads, baseURL, auto, cookie string) string {
	t.Helper()
	cacheDir := t.TempDir()
	for name, value := range map[string]string{
		"ANNAS_SECRET_KEY":     secret,
		"ANNAS_DOWNLOAD_PATH":  downloads,
		"ANNAS_BASE_URL":       baseURL,
		"ANNAS_AUTO_BASE_URL":  auto,
		"ANNAS_ACCOUNT_COOKIE": cookie,
		"ANNAS_MCP_CACHE_DIR":  cacheDir,
	} {
		t.Setenv(name, value)
	}
	return filepath.Join(cacheDir, "session.json")
}

func TestLoadNormalizesSettings(t *testing.T) {
	cases := []struct {
		name                          string
		secret, downloads, base, auto string
		cookie                        string
		want                          Env
	}{
		{
			name: "defaults", secret: "key", downloads: "/data/books", cookie: "token",
			want: Env{AnnasBaseURL: DefaultAnnasBaseURL, AutoBaseURL: true, SecretKey: "key", DownloadPath: "/data/books", AccountCookie: "aa_account_id2=token"},
		},
		{
			// Search needs no download settings, and downloads default to a
			// folder in the home folder.
			name: "search-only overrides", base: "https://configured.example/", auto: "false",
			cookie: "fundraiser_banner_hidden=6; aa_account_id2=from-jar; __ddg1_=x",
			want:   Env{AnnasBaseURL: "configured.example", AccountCookie: "aa_account_id2=from-jar"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.want.SessionFile = setEnv(t, tc.secret, tc.downloads, tc.base, tc.auto, tc.cookie)
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)
			if tc.downloads == "" {
				tc.want.DownloadPath = filepath.Join(home, DefaultDownloadFolder)
			}
			got, err := Load()
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if *got != tc.want {
				t.Fatalf("Load = %+v, want %+v", *got, tc.want)
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
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "raw", raw: "token", want: "aa_account_id2=token"},
		{name: "raw base64 padding", raw: "abc=", want: "aa_account_id2=abc="},
		{name: "name value", raw: "aa_account_id2=abc=", want: "aa_account_id2=abc="},
		{name: "full header", raw: "fundraiser_banner_hidden=6; aa_account_id2=from-jar; __ddg1_=x", want: "aa_account_id2=from-jar"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("ANNAS_ACCOUNT_COOKIE", tt.raw)
			config, err := Load()
			if err != nil {
				t.Fatalf("Load returned error: %v", err)
			}
			if config.AccountCookie != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, config.AccountCookie)
			}
		})
	}

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
