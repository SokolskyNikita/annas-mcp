package anna

import "testing"

func TestCookieValueDoesNotConfuseSimilarNames(t *testing.T) {
	if value, ok := cookieValue("not_aa_account_id2=wrong", accountCookieName); ok || value != "" {
		t.Fatalf("matched a similar cookie name: %q, %v", value, ok)
	}
	if _, ok := cookieValue("aa_account_id2=", accountCookieName); ok {
		t.Fatal("accepted empty account cookie")
	}
}
