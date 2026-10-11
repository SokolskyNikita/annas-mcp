// Package parse reads what Anna's Archive and DOI.org send: search result
// pages, record pages, SciDB pages, the account page, the archive's JSON
// answers and download headers. It makes no requests and keeps no state; the
// client in package anna decides what an answer means for a tool call and
// which error to report.
package parse
