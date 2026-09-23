package anna

import (
	"context"
	"testing"
)

func TestAutomaticMirrorRejectsFraudulentCustomResolverCandidate(t *testing.T) {
	t.Parallel()

	client := NewClient(Config{
		BaseURL:     "annas-archive.gl",
		AutoBaseURL: true,
		Resolver: func(context.Context) (string, error) {
			return "https://annas-archive.su/", nil
		},
	})

	base, err := client.baseURLFor(context.Background())
	if err != nil {
		t.Fatalf("baseURLFor returned error: %v", err)
	}
	if base != "https://annas-archive.gl" {
		t.Fatalf("untrusted resolver candidate selected %q, want configured fallback", base)
	}
}

func TestExplicitBaseURLCanOverrideAutomaticMirrorAllowlist(t *testing.T) {
	t.Parallel()

	client := NewClient(Config{BaseURL: "annas-archive.su"})
	base, err := client.baseURLFor(context.Background())
	if err != nil {
		t.Fatalf("baseURLFor returned error: %v", err)
	}
	if base != "https://annas-archive.su" {
		t.Fatalf("explicit base URL became %q, want configured origin", base)
	}
}
