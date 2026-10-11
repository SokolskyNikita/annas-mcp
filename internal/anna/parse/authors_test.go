package parse

import "testing"

func TestCleanAuthorsDropsNumberedAffiliations(t *testing.T) {
	t.Parallel()

	for input, want := range map[string]string{
		"Yann Lecun , Yoshua Bengio , Geoffrey Hinton ;1 FAIR - Facebook AI Research [New York] ( 545378 ) - New York 2 NYU": "Yann Lecun, Yoshua Bengio, Geoffrey Hinton",
		"  Austen, Jane  ":      "Austen, Jane",
		"Ayr, Bray; Lady, a":    "Ayr, Bray; Lady, a",
		"Cinnamon Worth & JAff": "Cinnamon Worth & JAff",
		"":                      "",
	} {
		if got := CleanAuthors(input); got != want {
			t.Errorf("cleanAuthors(%q) = %q, want %q", input, got, want)
		}
	}
}
