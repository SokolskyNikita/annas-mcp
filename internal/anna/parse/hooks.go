package parse

// Selectors for parts of Anna's pages that several parsers read.
var (
	authorSelector      = "a[href^='/search'] span.icon-\\[mdi--user-edit\\]"
	publisherSelector   = "a[href^='/search'] span.icon-\\[mdi--company\\]"
	descriptionSelector = "div.text-gray-600"
)
