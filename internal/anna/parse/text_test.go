package parse

import "testing"

func TestCleanText(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"Smith &amp; Jones", "Smith & Jones"},
		{"&#x201C;Quoted&#x201D; &#8212; text", "“Quoted” — text"},
		{"AT&T and R&D", "AT&T and R&D"},
		{"Cats &para no semicolon", "Cats &para no semicolon"},
		{"Protein prediction by <i>AlphaFold</i>2", "Protein prediction by AlphaFold2"},
		{"<jats:p>Abstract <jats:italic>text</jats:italic></jats:p>", "Abstract text"},
		{"<p>One<p>Two<br/>Three</p>", "One Two Three"},
		{"Vector<T> in C++", "Vector<T> in C++"},
		{"a < b and c > d", "a < b and c > d"},
		{"<scp>CompTIA</scp> A+ Core 1 Exam", "CompTIA A+ Core 1 Exam"},
		{"Photonic Quantum Computing - <title>Hamiltonians for quantum computing</title>", "Photonic Quantum Computing - Hamiltonians for quantum computing"},
		{"  spaced\n\tout  ", "spaced out"},
		{"\u200fהאיחוד האירופי :\u200f \u200fהיסטוריה", "האיחוד האירופי : היסטוריה"},
		{"\u202bembedded\u202c \u2067isolated\u2069 \ufeffmarked", "embedded isolated marked"},
		// Joiners shape Bengali letters and stay.
		{"খুল্\u200cনার", "খুল্\u200cনার"},
	} {
		if got := CleanText(tc.in); got != tc.want {
			t.Errorf("cleanText(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
