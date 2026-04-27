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

// wikiPattern matches unambiguous Jira wiki-markup tokens per-line. Anchors
// use Go's default regexp mode where ^ is start-of-input — start-of-line
// holds because each line is matched in isolation.
//
// Alternatives (in order):
//   - {code}, {code:sql}, {noformat}, {panel}, {panel:title=X}, {quote}
//     block macros, including any `:params` body.
//   - {{inline}} variable/mono syntax, same line only.
//   - h1.–h6. headings at line start (trailing space required so tokens like
//     "h12" or prose starting with "h1." don't match).
//   - [Label|https://url] bracketed links; pipe + http(s) scheme is required
//     so we never clash with Markdown [text](url) syntax.
var wikiPattern = regexp.MustCompile(
	`\{code(?::[^}]*)?\}` +
		`|\{noformat(?::[^}]*)?\}` +
		`|\{panel(?::[^}]*)?\}` +
		`|\{quote(?::[^}]*)?\}` +
		`|\{\{[^}\n]+\}\}` +
		`|^h[1-6]\. ` +
		`|\[[^\]|\n]+\|https?://[^\]\n]+\]`,
)

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
		for _, m := range wikiPattern.FindAllStringIndex(line, -1) {
			hits = append(hits, WikiMarkupHit{
				Token:      strings.TrimSpace(line[m[0]:m[1]]),
				Line:       line,
				LineNumber: lineNum,
			})
		}
	}
	return hits
}
