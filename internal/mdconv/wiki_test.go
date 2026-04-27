package mdconv

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectWikiMarkup(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		wantTokens    []string // substrings expected in the returned Token fields
		wantLineNums  []int    // zero-based line numbers (same length as wantTokens)
		wantNoMatches bool
	}{
		// --- positive cases ---
		{
			name:         "code block",
			input:        "prose\n{code:sql}\nselect 1\n{code}",
			wantTokens:   []string{"{code:sql}", "{code}"},
			wantLineNums: []int{1, 3},
		},
		{
			name:         "noformat block",
			input:        "{noformat}\nfoo\n{noformat}",
			wantTokens:   []string{"{noformat}", "{noformat}"},
			wantLineNums: []int{0, 2},
		},
		{
			name:         "panel block",
			input:        "{panel:title=Hi}\nbody\n{panel}",
			wantTokens:   []string{"{panel:title=Hi}", "{panel}"},
			wantLineNums: []int{0, 2},
		},
		{
			name:         "quote block",
			input:        "{quote}\nsaid\n{quote}",
			wantTokens:   []string{"{quote}", "{quote}"},
			wantLineNums: []int{0, 2},
		},
		{
			name:         "double-brace inline",
			input:        "use {{variable}} here",
			wantTokens:   []string{"{{variable}}"},
			wantLineNums: []int{0},
		},
		{
			name:         "h1 heading at line start",
			input:        "h1. Top\ntext",
			wantTokens:   []string{"h1."},
			wantLineNums: []int{0},
		},
		{
			name:         "h2 heading at line start",
			input:        "prose\nh2. Sub",
			wantTokens:   []string{"h2."},
			wantLineNums: []int{1},
		},
		{
			name:         "h6 heading at line start",
			input:        "h6. Leaf",
			wantTokens:   []string{"h6."},
			wantLineNums: []int{0},
		},
		{
			name:         "bracketed link with pipe and url",
			input:        "see [docs|https://example.com/x] for more",
			wantTokens:   []string{"[docs|https://example.com/x]"},
			wantLineNums: []int{0},
		},

		// --- negative cases ---
		{
			name:          "markdown bold",
			input:         "Hello **bold** world",
			wantNoMatches: true,
		},
		{
			name:          "markdown italic",
			input:         "Hello *italic* world",
			wantNoMatches: true,
		},
		{
			name:          "markdown inline code",
			input:         "use `fmt.Println` here",
			wantNoMatches: true,
		},
		{
			name:          "markdown fenced code block",
			input:         "```sql\nselect 1\n```",
			wantNoMatches: true,
		},
		{
			name:          "markdown heading with hash and space",
			input:         "# Heading\n\nparagraph",
			wantNoMatches: true,
		},
		{
			name:          "markdown numbered list",
			input:         "1. first\n2. second",
			wantNoMatches: true,
		},
		{
			name:          "prose with curly braces mid-line",
			input:         "C++ {std::vector} is common",
			wantNoMatches: true,
		},
		{
			name:          "prose with markdown link",
			input:         "see [here](https://example.com)",
			wantNoMatches: true,
		},
		{
			name:          "h1 not at line start",
			input:         "some text h1. not a heading",
			wantNoMatches: true,
		},
		{
			name:          "h7 is not a wiki heading",
			input:         "h7. not really",
			wantNoMatches: true,
		},
		{
			name:          "double-brace without closing on same line",
			input:         "{{start\nno close}}",
			wantNoMatches: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			hits := DetectWikiMarkup(tc.input)

			if tc.wantNoMatches {
				assert.Empty(t, hits, "unexpected hits: %+v", hits)
				return
			}

			require.Len(t, hits, len(tc.wantTokens))
			for i, want := range tc.wantTokens {
				assert.Contains(t, hits[i].Token, want, "hit %d token mismatch", i)
				assert.Equal(t, tc.wantLineNums[i], hits[i].LineNumber, "hit %d line number", i)
			}
		})
	}
}

func TestDetectWikiMarkup_HitIncludesLine(t *testing.T) {
	hits := DetectWikiMarkup("before\n{code:sql}select 1{code}\nafter")
	require.NotEmpty(t, hits)
	assert.Equal(t, "{code:sql}select 1{code}", hits[0].Line)
	assert.Equal(t, 1, hits[0].LineNumber)
}
