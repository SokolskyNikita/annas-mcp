package parse

// The parts of Anna's markup the parsers rely on. Where Anna's own scripts
// use a js- class, the parsers use it too: those change when Anna's scripts
// do, while styling classes change with any redesign. Icons and link
// targets are the next best thing. The few parts with no such hook are
// marked; CheckSearchPage notices when they stop matching.
const (
	// Search pages: each list of results, the partial matches' section and
	// its "Show 41 partial matches" button.
	resultsListSelector    = ".js-aarecord-list-outer"
	partialMatchesSelector = ".js-partial-matches-show"
	partialButtonSelector  = ".js-partial-matches-remove button"
	// A card's title, linking to its record, and the link that follows its
	// description.
	titleSelector           = "a.js-vim-focus[href]"
	descriptionLinkSelector = "a.js-aarecord-list-read-more"
	// The author and "publisher, edition" lines are links with these icons,
	// on cards and on record pages.
	authorSelector    = "a[href^='/search'] span.icon-\\[mdi--user-edit\\]"
	publisherSelector = "a[href^='/search'] span.icon-\\[mdi--company\\]"
	// No hook: the metadata line is the one div of a card or a record
	// page's header with this class.
	metadataLineSelector = "div.text-gray-800"
	// No hook: Anna greys out the cards it warns about. CheckSearchPage
	// compares it with the issues line.
	possibleIssuesClass = "opacity-40"

	// Record pages: the cover drawn when the record has no image, which
	// carries the full title and then the authors, and the identifiers
	// panel.
	fallbackCoverSelector = ".js-aarecord-list-fallback-cover [data-content]"
	codesSelector         = ".js-md5-codes-container"

	// SciDB pages embed the PDF in this viewer.
	viewerPath = "/pdfjs/web/viewer.html"

	// The account page's links.
	secretKeySelector = "a[href='/account/secret_key']"
	downloadsSelector = "a[href='/account/downloaded']"
	tierSelector      = "a[href^='/donate?tier=']"
	// No hook: an expired membership is struck through.
	expiredClass = "line-through"
)

// hooks are the strings in Anna's templates that the selectors above
// depend on. anna/scripts/upstream-watch.json must watch each of them; a test
// checks that it does.
var hooks = []string{
	"js-aarecord-list-outer", "js-partial-matches-show", "js-partial-matches-remove", "js-vim-focus",
	"js-aarecord-list-read-more", "mdi--user-edit", "mdi--company", "text-gray-800", "opacity-40",
	"js-aarecord-list-fallback-cover", "data-content", "js-md5-codes-container", "/pdfjs/web/viewer.html",
	"/account/secret_key", "/account/downloaded", "/donate?tier=", "line-through",
}
