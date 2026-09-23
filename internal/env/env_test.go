package env

import (
	"testing"
)

func TestLoadIsPureAndDoesNotRequireDownloadConfiguration(t *testing.T) {
	t.Setenv("ANNAS_SECRET_KEY", "")
	t.Setenv("ANNAS_DOWNLOAD_PATH", "")
	t.Setenv("ANNAS_BASE_URL", "https://configured.example/")
	t.Setenv("ANNAS_AUTO_BASE_URL", "false")
	t.Setenv("ANNAS_ACCOUNT_COOKIE", "fundraiser_banner_hidden=6; aa_account_id2=from-jar; __ddg1_=x")

	config, err := Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if config.SecretKey != "" || config.DownloadPath != "" {
		t.Fatalf("expected missing download settings to remain optional: %+v", config)
	}
	if config.AnnasBaseURL != "configured.example" || config.AutoBaseURL {
		t.Fatalf("unexpected normalized config: %+v", config)
	}
	if config.AccountCookie != "aa_account_id2=from-jar" {
		t.Fatalf("unexpected account cookie: %q", config.AccountCookie)
	}
}

func TestLoadDefaultsToConfiguredFallbackAndAutomaticSelection(t *testing.T) {
	t.Setenv("ANNAS_SECRET_KEY", "secret")
	t.Setenv("ANNAS_DOWNLOAD_PATH", "/tmp/downloads")
	t.Setenv("ANNAS_BASE_URL", "")
	t.Setenv("ANNAS_AUTO_BASE_URL", "")
	t.Setenv("ANNAS_ACCOUNT_COOKIE", "token")

	config, err := Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if config.AnnasBaseURL != DefaultAnnasBaseURL || !config.AutoBaseURL {
		t.Fatalf("unexpected defaults: %+v", config)
	}
	if config.AccountCookie != "aa_account_id2=token" {
		t.Fatalf("unexpected account cookie: %q", config.AccountCookie)
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
