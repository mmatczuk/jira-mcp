package mdconv

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- node-builder helpers (test-local) ---

func doc(content ...any) map[string]any {
	return map[string]any{"version": 1, "type": "doc", "content": content}
}

func para(content ...any) map[string]any {
	return map[string]any{"type": "paragraph", "content": content}
}

func head(level int, content ...any) map[string]any {
	return map[string]any{
		"type":    "heading",
		"attrs":   map[string]any{"level": level},
		"content": content,
	}
}

func txt(s string, marks ...any) map[string]any {
	n := map[string]any{"type": "text", "text": s}
	if len(marks) > 0 {
		n["marks"] = marks
	}
	return n
}

func mark(t string) map[string]any                { return map[string]any{"type": t} }
func linkMark(href string) map[string]any {
	return map[string]any{"type": "link", "attrs": map[string]any{"href": href}}
}

func bulletList(items ...any) map[string]any {
	return map[string]any{"type": "bulletList", "content": items}
}
func orderedList(items ...any) map[string]any {
	return map[string]any{"type": "orderedList", "content": items}
}
func li(content ...any) map[string]any {
	return map[string]any{"type": "listItem", "content": content}
}

func codeBlock(text, lang string) map[string]any {
	n := map[string]any{
		"type":    "codeBlock",
		"content": []any{map[string]any{"type": "text", "text": text}},
	}
	if lang != "" {
		n["attrs"] = map[string]any{"language": lang}
	}
	return n
}

func hardBreak() map[string]any { return map[string]any{"type": "hardBreak"} }

// --- core node coverage ---

func TestFromADF_BlockNodes(t *testing.T) {
	cases := []struct {
		name string
		in   map[string]any
		want string
	}{
		{
			name: "paragraph",
			in:   doc(para(txt("hello"))),
			want: "hello\n",
		},
		{
			name: "heading level 1",
			in:   doc(head(1, txt("Title"))),
			want: "# Title\n",
		},
		{
			name: "heading levels 2-6",
			in: doc(
				head(2, txt("h2")),
				head(3, txt("h3")),
				head(4, txt("h4")),
				head(5, txt("h5")),
				head(6, txt("h6")),
			),
			want: "## h2\n\n### h3\n\n#### h4\n\n##### h5\n\n###### h6\n",
		},
		{
			name: "bullet list",
			in: doc(bulletList(
				li(para(txt("one"))),
				li(para(txt("two"))),
			)),
			want: "- one\n- two\n",
		},
		{
			name: "ordered list",
			in: doc(orderedList(
				li(para(txt("first"))),
				li(para(txt("second"))),
			)),
			want: "1. first\n2. second\n",
		},
		{
			name: "nested bullet list",
			in: doc(bulletList(
				li(
					para(txt("outer")),
					bulletList(li(para(txt("inner")))),
				),
			)),
			want: "- outer\n  - inner\n",
		},
		{
			name: "fenced code block with language",
			in:   doc(codeBlock("fmt.Println(\"hi\")\n", "go")),
			want: "```go\nfmt.Println(\"hi\")\n```\n",
		},
		{
			name: "fenced code block without language",
			in:   doc(codeBlock("plain\n", "")),
			want: "```\nplain\n```\n",
		},
		{
			name: "hard break joins two text runs with a newline",
			in:   doc(para(txt("line1"), hardBreak(), txt("line2"))),
			want: "line1\nline2\n",
		},
		{
			name: "two paragraphs separated by blank line",
			in:   doc(para(txt("first")), para(txt("second"))),
			want: "first\n\nsecond\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := FromADF(tc.in)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

// --- inline marks ---

func TestFromADF_InlineMarks(t *testing.T) {
	cases := []struct {
		name string
		in   map[string]any
		want string
	}{
		{
			name: "strong",
			in:   doc(para(txt("bold", mark("strong")))),
			want: "**bold**\n",
		},
		{
			name: "em",
			in:   doc(para(txt("italic", mark("em")))),
			want: "*italic*\n",
		},
		{
			name: "inline code",
			in:   doc(para(txt("code", mark("code")))),
			want: "`code`\n",
		},
		{
			name: "link",
			in:   doc(para(txt("GitHub", linkMark("https://github.com")))),
			want: "[GitHub](https://github.com)\n",
		},
		{
			name: "strong and em stack",
			in:   doc(para(txt("both", mark("strong"), mark("em")))),
			want: "***both***\n",
		},
		{
			name: "link wraps strong text",
			in:   doc(para(txt("click", mark("strong"), linkMark("https://x")))),
			want: "[**click**](https://x)\n",
		},
		{
			name: "mixed inline",
			in:   doc(para(txt("a "), txt("b", mark("strong")), txt(" c"))),
			want: "a **b** c\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := FromADF(tc.in)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

// --- unknown nodes ---

func TestFromADF_UnknownNode_FallbackToFencedJSON(t *testing.T) {
	panel := map[string]any{
		"type":  "panel",
		"attrs": map[string]any{"panelType": "info"},
		"content": []any{
			map[string]any{"type": "paragraph", "content": []any{txt("note")}},
		},
	}
	got, err := FromADF(doc(panel))
	require.NoError(t, err)
	assert.Contains(t, got, "```adf-unsupported")
	assert.Contains(t, got, `"type":"panel"`)
	assert.Contains(t, got, `"panelType":"info"`)
}

// --- empty inputs ---

func TestFromADF_EmptyInputs(t *testing.T) {
	cases := []struct {
		name string
		in   map[string]any
	}{
		{name: "nil doc", in: nil},
		{name: "doc with no content", in: doc()},
		{name: "doc with empty paragraph", in: doc(para())},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := FromADF(tc.in)
			require.NoError(t, err)
			assert.Equal(t, "", got)
		})
	}
}

// --- round-trip on a representative Markdown corpus ---

func TestRoundTrip_MarkdownCorpus(t *testing.T) {
	cases := []struct {
		name     string
		markdown string
	}{
		{name: "single paragraph", markdown: "hello world"},
		{name: "heading and paragraph", markdown: "# Title\n\nParagraph body"},
		{name: "heading hierarchy", markdown: "# H1\n\n## H2\n\n### H3"},
		{name: "bullet list", markdown: "- one\n- two\n- three"},
		{name: "ordered list", markdown: "1. first\n2. second\n3. third"},
		{name: "inline marks", markdown: "**bold** and *italic* and `inline code`"},
		{name: "link", markdown: "see [docs](https://example.com)"},
		{name: "fenced code with lang", markdown: "```go\nfmt.Println(\"hi\")\n```"},
		{name: "soft-break in paragraph", markdown: "line one\nline two"},
		{
			name: "mixed paragraphs and bullet list",
			markdown: "**Status:** open\n\n" +
				"**Notes**\n\n" +
				"- first item\n" +
				"- second item\n" +
				"- third item",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			adf := ToADF(tc.markdown)
			got, err := FromADF(adf)
			require.NoError(t, err)
			assert.Equal(t, tc.markdown, strings.TrimRight(got, "\n"))
		})
	}
}
