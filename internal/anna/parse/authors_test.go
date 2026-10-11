package parse

import (
	"reflect"
	"testing"
)

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

func TestCleanAuthorsDropsParenthesizedAffiliations(t *testing.T) {
	t.Parallel()

	for input, want := range map[string]string{
		"Yang Zhang;Lin Ning(Innovative Institute of Chinese Medicine, Chengdu University;Department of Oncology (Quzhou))":                               "Yang Zhang; Lin Ning",
		"Li Wei（Institute of Physics, Chinese Academy of Sciences, Beijing, China）":                                                                       "Li Wei",
		"Ada Lovelace (ed.); Charles Babbage (Translator)":                                                                                                "Ada Lovelace (ed.); Charles Babbage (Translator)",
		"Smith (a long note that mentions nothing about where the author works)":                                                                          "Smith (a long note that mentions nothing about where the author works)",
		"Jones(University of Somewhere, Department of Things, which never closes":                                                                         "Jones",
		"A. One;B. Two( A. One ( Department of Radiology, X University) Hope Center, X University) ; B. Two ( Division of Biostatistics, X University) )": "A. One; B. Two",
		"Le, Minh-Quy( quy@example.edu)（Department of Mechanics, Hanoi University）":                                                                       "Le, Minh-Quy",
		"Birch, Eugenie L（University of Pennsylvania）":                                                                                                    "Birch, Eugenie L",
		"Pandha, H (Pandha, Hardev); Pawelec, G (Pawelec, Graham)(Section of Oncology, School of Biosciences, University of Surrey)":                      "Pandha, H (Pandha, Hardev); Pawelec, G (Pawelec, Graham)",
		"Carolee Caffrey; Tiffany J Weston Affiliation: Zoology Department, Oklahoma State University":                                                    "Carolee Caffrey; Tiffany J Weston",
		"Hamel, R.. Alzheimer Centre Limburg, Maastricht University, r.hamel@example.nl;Leuzy, Antoine. Department NVS, Karolinska Institutet, Sweden":    "Hamel, R.; Leuzy, Antoine",
		"Kate L. Turabian, Wayne C. Booth, University of Chicago Press Editorial Staff":                                                                   "Kate L. Turabian, Wayne C. Booth, University of Chicago Press Editorial Staff",
		"Ramaswamy Govindan; Washington University (Saint Louis, Mo). School of Medicine":                                                                 "Ramaswamy Govindan; Washington University (Saint Louis, Mo). School of Medicine",
	} {
		if got := CleanAuthors(input); got != want {
			t.Errorf("CleanAuthors(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestSplitAuthors(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		values []string
		want   []string
	}{
		{[]string{"Ashish Vaswani, Noam Shazeer"}, []string{"Ashish Vaswani", "Noam Shazeer"}},
		{[]string{"Kingma, Diederik P. (author); Ba, Jimmy Lei (author)"}, []string{"Diederik P. Kingma", "Jimmy Lei Ba"}},
		{[]string{"One, Two, Three; Four"}, []string{"One, Two, Three", "Four"}},
		{[]string{" ; "}, nil},
	} {
		if got := splitAuthors(tc.values); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("splitAuthors(%q) = %q, want %q", tc.values, got, tc.want)
		}
	}
}
