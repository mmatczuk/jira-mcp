package mdconv

import (
	"regexp"
	"strings"
)

// WikiMarkupHit reports a detected Jira wiki-markup token.
// LineNumber is zero-based; Line is the full source line containing the token.
type WikiMarkupHit struct {
	Token      string
	Line       string
	LineNumber int
}

// wikiPatterns are unambiguous Jira wiki-markup tokens matched per-line. The
// heading regex is anchored with ^ — Go's default regexp mode treats ^ as
// start-of-input, which is start-of-line here because we match each line in
// isolation.
var wikiPatterns = []*regexp.Regexp{
	// Block macros: {code}, {code:sql}, {noformat}, {panel}, {panel:title=X},
	// {quote}. We match the full tag including any `:params` body.
	regexp.MustCompile(`\{code(?::[^}]*)?\}`),
	regexp.MustCompile(`\{noformat(?::[^}]*)?\}`),
	regexp.MustCompile(`\{panel(?::[^}]*)?\}`),
	regexp.MustCompile(`\{quote(?::[^}]*)?\}`),
	// {{inline}} — variable/mono syntax. Same line only.
	regexp.MustCompile(`\{\{[^}\n]+\}\}`),
	// Headings h1.–h6. at line start (requires trailing space to avoid
	// matching tokens like "h12" or prose starting with "h1.").
	regexp.MustCompile(`^h[1-6]\. `),
	// [Label|https://url] bracketed link. Requires pipe + http(s) scheme so
	// we never clash with Markdown [text](url) syntax.
	regexp.MustCompile(`\[[^\]|\n]+\|https?://[^\]\n]+\]`),
}

// DetectWikiMarkup scans Markdown input for unambiguous Jira wiki-markup
// tokens and returns a hit per occurrence in source order.
//
// Coverage is deliberately conservative: only patterns with a near-zero
// false-positive rate against plain Markdown are included. Ambiguous tokens
// like *bold*, _italic_, +ins+, ~sub~, ^sup^ are out of scope here — callers
// wanting literal wiki-markup should opt in via the format parameter.
func DetectWikiMarkup(s string) []WikiMarkupHit {
	if s == "" {
		return nil
	}

	var hits []WikiMarkupHit
	for lineNum, line := range strings.Split(s, "\n") {
		for _, re := range wikiPatterns {
			for _, m := range re.FindAllStringIndex(line, -1) {
				hits = append(hits, WikiMarkupHit{
					Token:      strings.TrimSpace(line[m[0]:m[1]]),
					Line:       line,
					LineNumber: lineNum,
				})
			}
		}
	}
	return hits
}
