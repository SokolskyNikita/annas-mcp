package parse

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Limits on what a Report keeps, so a page Anna changed cannot flood a log
// with markup or long text.
const (
	maxReportSamples     = 3
	maxReportSampleRunes = 80
	maxReportBytes       = 600
)

// Report lists what a parser met on a page and could not read, by kind,
// with a few short examples of each. Examples are text the parser had
// already extracted, or record paths; never markup.
type Report struct {
	Problems []Problem
}

// Problem is one kind of thing a page had that a parser could not read.
type Problem struct {
	Kind    string
	Count   int
	Samples []string
}

// Empty reports whether nothing went unread.
func (r Report) Empty() bool {
	return len(r.Problems) == 0
}

// Add counts one occurrence of kind, keeping sample when it is new and
// there is room for it.
func (r *Report) Add(kind, sample string) {
	sample = reportSample(sample)
	for i := range r.Problems {
		problem := &r.Problems[i]
		if problem.Kind != kind {
			continue
		}
		problem.Count++
		if len(problem.Samples) < maxReportSamples && !containsString(problem.Samples, sample) {
			problem.Samples = append(problem.Samples, sample)
		}
		return
	}
	r.Problems = append(r.Problems, Problem{Kind: kind, Count: 1, Samples: []string{sample}})
}

// Filter keeps the samples keep accepts, and the problems left with any.
func (r Report) Filter(keep func(kind, sample string) bool) Report {
	var kept Report
	for _, problem := range r.Problems {
		var samples []string
		for _, sample := range problem.Samples {
			if keep(problem.Kind, sample) {
				samples = append(samples, sample)
			}
		}
		if len(samples) > 0 {
			kept.Problems = append(kept.Problems, Problem{Kind: problem.Kind, Count: problem.Count, Samples: samples})
		}
	}
	return kept
}

// String renders the report as one line of at most 600 bytes, such as
// `unrecognized parts of metadata lines: 7 (e.g. "KFX", "Ms. Word")`.
func (r Report) String() string {
	parts := make([]string, 0, len(r.Problems))
	for _, problem := range r.Problems {
		quoted := make([]string, len(problem.Samples))
		for i, sample := range problem.Samples {
			quoted[i] = fmt.Sprintf("%q", sample)
		}
		parts = append(parts, fmt.Sprintf("%s: %d (e.g. %s)", problem.Kind, problem.Count, strings.Join(quoted, ", ")))
	}
	return truncateBytes(strings.Join(parts, "; "), maxReportBytes)
}

// reportSample collapses whitespace, drops control characters and cuts the
// sample to 80 characters.
func reportSample(sample string) string {
	sample = strings.Join(strings.FieldsFunc(sample, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }), " ")
	if utf8.RuneCountInString(sample) <= maxReportSampleRunes {
		return sample
	}
	runes := []rune(sample)
	return string(runes[:maxReportSampleRunes-1]) + "…"
}

func truncateBytes(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	cut := text[:limit-len("…")]
	for !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	return cut + "…"
}

func containsString(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}
