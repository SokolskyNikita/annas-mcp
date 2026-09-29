package env

import "testing"

// setEnv sets every ANNAS_* variable Load reads, so a test never inherits
// values from the developer's shell.
func setEnv(t *testing.T, secret, downloads, baseURL, auto, cookie string) {
	t.Helper()
	for name, value := range map[string]string{
		"ANNAS_SECRET_KEY":     secret,
		"ANNAS_DOWNLOAD_PATH":  downloads,
		"ANNAS_BASE_URL":       baseURL,
		"ANNAS_AUTO_BASE_URL":  auto,
		"ANNAS_ACCOUNT_COOKIE": cookie,
	} {
		t.Setenv(name, value)
	}
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
			// Search needs no download settings, so Load must not require them.
			name: "search-only overrides", base: "https://configured.example/", auto: "false",
			cookie: "fundraiser_banner_hidden=6; aa_account_id2=from-jar; __ddg1_=x",
			want:   Env{AnnasBaseURL: "configured.example", AccountCookie: "aa_account_id2=from-jar"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setEnv(t, tc.secret, tc.downloads, tc.base, tc.auto, tc.cookie)
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
