package mdconv

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToADF_EmptyString(t *testing.T) {
	assert.Nil(t, ToADF(""))
}

func TestToADF_Paragraph(t *testing.T) {
	result := ToADF("Hello world")
	require.NotNil(t, result)
	assert.Equal(t, 1, result["version"])
	assert.Equal(t, "doc", result["type"])

	content := result["content"].([]any)
	require.Len(t, content, 1)

	para := content[0].(node)
	assert.Equal(t, "paragraph", para["type"])

	inlines := para["content"].([]any)
	require.NotEmpty(t, inlines)
	text := inlines[0].(node)
	assert.Equal(t, "text", text["type"])
	assert.Equal(t, "Hello world", text["text"])
}

func TestToADF_Heading(t *testing.T) {
	result := ToADF("# Title")
	require.NotNil(t, result)

	content := result["content"].([]any)
	require.NotEmpty(t, content)

	heading := content[0].(node)
	assert.Equal(t, "heading", heading["type"])
	attrs := heading["attrs"].(node)
	assert.Equal(t, 1, attrs["level"])
}

func TestToADF_BulletList(t *testing.T) {
	result := ToADF("- one\n- two\n- three")
	require.NotNil(t, result)

	content := result["content"].([]any)
	require.NotEmpty(t, content)

	list := content[0].(node)
	assert.Equal(t, "bulletList", list["type"])

	items := list["content"].([]any)
	assert.Len(t, items, 3)
}

func TestToADF_OrderedList(t *testing.T) {
	result := ToADF("1. first\n2. second")
	require.NotNil(t, result)

	content := result["content"].([]any)
	list := content[0].(node)
	assert.Equal(t, "orderedList", list["type"])
}

func TestToADF_FencedCodeBlock(t *testing.T) {
	result := ToADF("```go\nfmt.Println(\"hi\")\n```")
	require.NotNil(t, result)

	content := result["content"].([]any)
	require.NotEmpty(t, content)

	cb := content[0].(node)
	assert.Equal(t, "codeBlock", cb["type"])
	attrs := cb["attrs"].(node)
	assert.Equal(t, "go", attrs["language"])
}

func TestToADF_Bold(t *testing.T) {
	result := ToADF("Hello **bold** world")
	require.NotNil(t, result)

	content := result["content"].([]any)
	para := content[0].(node)
	inlines := para["content"].([]any)

	// Find the bold text node
	found := false
	for _, inline := range inlines {
		n := inline.(node)
		if marks, ok := n["marks"]; ok {
			for _, mark := range marks.([]any) {
				if m, ok := mark.(node); ok && m["type"] == "strong" {
					found = true
					assert.Equal(t, "bold", n["text"])
				}
			}
		}
	}
	assert.True(t, found, "expected a text node with strong mark")
}

func TestToADF_Blockquote(t *testing.T) {
	result := ToADF("> quoted text")
	require.NotNil(t, result)

	content := result["content"].([]any)
	bq := content[0].(node)
	assert.Equal(t, "blockquote", bq["type"])
}

func TestToADF_InlineCode(t *testing.T) {
	result := ToADF("Use `fmt.Println` here")
	require.NotNil(t, result)

	content := result["content"].([]any)
	para := content[0].(node)
	inlines := para["content"].([]any)

	found := false
	for _, inline := range inlines {
		n := inline.(node)
		if marks, ok := n["marks"]; ok {
			for _, mark := range marks.([]any) {
				if m, ok := mark.(node); ok && m["type"] == "code" {
					found = true
					assert.Equal(t, "fmt.Println", n["text"])
				}
			}
		}
	}
	assert.True(t, found, "expected a text node with code mark")
}

func TestToADF_Italic(t *testing.T) {
	result := ToADF("Hello *italic* world")
	require.NotNil(t, result)

	content := result["content"].([]any)
	para := content[0].(node)
	inlines := para["content"].([]any)

	found := false
	for _, inline := range inlines {
		n := inline.(node)
		if marks, ok := n["marks"]; ok {
			for _, mark := range marks.([]any) {
				if m, ok := mark.(node); ok && m["type"] == "em" {
					found = true
					assert.Equal(t, "italic", n["text"])
				}
			}
		}
	}
	assert.True(t, found, "expected a text node with em mark")
}

func TestToADF_Link(t *testing.T) {
	result := ToADF("Click [here](https://example.com)")
	require.NotNil(t, result)

	content := result["content"].([]any)
	para := content[0].(node)
	inlines := para["content"].([]any)

	found := false
	for _, inline := range inlines {
		n := inline.(node)
		if marks, ok := n["marks"]; ok {
			for _, mark := range marks.([]any) {
				if m, ok := mark.(node); ok && m["type"] == "link" {
					found = true
					attrs := m["attrs"].(node)
					assert.Equal(t, "https://example.com", attrs["href"])
					assert.Equal(t, "here", n["text"])
				}
			}
		}
	}
	assert.True(t, found, "expected a text node with link mark")
}

func TestToADF_Image(t *testing.T) {
	result := ToADF("![alt text](https://example.com/img.png)")
	require.NotNil(t, result)

	content := result["content"].([]any)
	require.NotEmpty(t, content)

	// Images are rendered as linked text nodes
	para := content[0].(node)
	inlines := para["content"].([]any)

	found := false
	for _, inline := range inlines {
		n := inline.(node)
		if marks, ok := n["marks"]; ok {
			for _, mark := range marks.([]any) {
				if m, ok := mark.(node); ok && m["type"] == "link" {
					attrs := m["attrs"].(node)
					if attrs["href"] == "https://example.com/img.png" {
						found = true
						assert.Equal(t, "alt text", n["text"])
					}
				}
			}
		}
	}
	assert.True(t, found, "expected image converted to linked text")
}

func TestToADF_HardLineBreak(t *testing.T) {
	// Two trailing spaces before newline = hard line break in Markdown.
	result := ToADF("line one  \nline two")
	require.NotNil(t, result)

	content := result["content"].([]any)
	para := content[0].(node)
	inlines := para["content"].([]any)

	found := false
	for _, inline := range inlines {
		if n, ok := inline.(node); ok && n["type"] == "hardBreak" {
			found = true
		}
	}
	assert.True(t, found, "expected a hardBreak node")
}

func TestToADF_SoftLineBreak(t *testing.T) {
	// Single newline inside a paragraph = soft line break. ADF has no soft
	// break primitive; the converter emits hardBreak so authored newlines
	// survive round-trip through Jira instead of silently collapsing.
	result := ToADF("line one\nline two")
	require.NotNil(t, result)

	content := result["content"].([]any)
	para := content[0].(node)
	inlines := para["content"].([]any)
	require.Len(t, inlines, 3)

	first := inlines[0].(node)
	assert.Equal(t, "text", first["type"])
	assert.Equal(t, "line one", first["text"])

	br := inlines[1].(node)
	assert.Equal(t, "hardBreak", br["type"])

	last := inlines[2].(node)
	assert.Equal(t, "text", last["type"])
	assert.Equal(t, "line two", last["text"])
}

func TestToADF_BoldFollowedBySoftBreak(t *testing.T) {
	// Pins the original bug: "**Header**\ntext" rendered as "Headertext"
	// because the soft break between the closing emphasis and the trailing
	// text was dropped. The strong mark must stay on "Header" only and must
	// not be applied to the hardBreak.
	result := ToADF("**Context**\nThe CHECK MOTOR feature flag.")
	require.NotNil(t, result)

	content := result["content"].([]any)
	para := content[0].(node)
	inlines := para["content"].([]any)
	require.GreaterOrEqual(t, len(inlines), 3)

	bold := inlines[0].(node)
	assert.Equal(t, "text", bold["type"])
	assert.Equal(t, "Context", bold["text"])
	boldMarks := bold["marks"].([]any)
	require.Len(t, boldMarks, 1)
	assert.Equal(t, "strong", boldMarks[0].(node)["type"])

	br := inlines[1].(node)
	assert.Equal(t, "hardBreak", br["type"])
	_, hasMarks := br["marks"]
	assert.False(t, hasMarks, "hardBreak must not carry marks")

	var trailing string
	for _, inline := range inlines[2:] {
		n := inline.(node)
		assert.Equal(t, "text", n["type"])
		if marks, ok := n["marks"]; ok {
			for _, mark := range marks.([]any) {
				assert.NotEqual(t, "strong", mark.(node)["type"],
					"text after soft break must not inherit strong mark")
			}
		}
		if s, ok := n["text"].(string); ok {
			trailing += s
		}
	}
	assert.Equal(t, "The CHECK MOTOR feature flag.", trailing)
}

func TestToADF_SoftBreakBetweenEmphasis(t *testing.T) {
	// Soft break between two bold runs: each run keeps its strong mark, the
	// hardBreak between them carries no marks.
	result := ToADF("**a**\n**b**")
	require.NotNil(t, result)

	content := result["content"].([]any)
	para := content[0].(node)
	inlines := para["content"].([]any)
	require.Len(t, inlines, 3)

	a := inlines[0].(node)
	assert.Equal(t, "text", a["type"])
	assert.Equal(t, "a", a["text"])
	aMarks := a["marks"].([]any)
	require.Len(t, aMarks, 1)
	assert.Equal(t, "strong", aMarks[0].(node)["type"])

	br := inlines[1].(node)
	assert.Equal(t, "hardBreak", br["type"])
	_, hasMarks := br["marks"]
	assert.False(t, hasMarks)

	b := inlines[2].(node)
	assert.Equal(t, "text", b["type"])
	assert.Equal(t, "b", b["text"])
	bMarks := b["marks"].([]any)
	require.Len(t, bMarks, 1)
	assert.Equal(t, "strong", bMarks[0].(node)["type"])
}

func TestToADF_EmphasisSpanningSoftBreak(t *testing.T) {
	// Italic that spans a soft break. Goldmark places the soft-break flag on
	// the first inner Text, so a hardBreak is emitted inside the Emphasis
	// subtree. The em mark must apply to the text nodes only, never to the
	// hardBreak — ADF rejects marks on hardBreak nodes.
	result := ToADF("*line one\nline two*")
	require.NotNil(t, result)

	content := result["content"].([]any)
	para := content[0].(node)
	inlines := para["content"].([]any)
	require.Len(t, inlines, 3)

	first := inlines[0].(node)
	assert.Equal(t, "text", first["type"])
	assert.Equal(t, "line one", first["text"])
	firstMarks := first["marks"].([]any)
	require.Len(t, firstMarks, 1)
	assert.Equal(t, "em", firstMarks[0].(node)["type"])

	br := inlines[1].(node)
	assert.Equal(t, "hardBreak", br["type"])
	_, hasMarks := br["marks"]
	assert.False(t, hasMarks, "hardBreak inside emphasis must not carry em mark")

	last := inlines[2].(node)
	assert.Equal(t, "text", last["type"])
	assert.Equal(t, "line two", last["text"])
	lastMarks := last["marks"].([]any)
	require.Len(t, lastMarks, 1)
	assert.Equal(t, "em", lastMarks[0].(node)["type"])
}

func TestToADF_StrongSpanningSoftBreak(t *testing.T) {
	// Same as the em test but with double-asterisk strong.
	result := ToADF("**line one\nline two**")
	require.NotNil(t, result)

	content := result["content"].([]any)
	para := content[0].(node)
	inlines := para["content"].([]any)
	require.Len(t, inlines, 3)

	first := inlines[0].(node)
	assert.Equal(t, "text", first["type"])
	firstMarks := first["marks"].([]any)
	require.Len(t, firstMarks, 1)
	assert.Equal(t, "strong", firstMarks[0].(node)["type"])

	br := inlines[1].(node)
	assert.Equal(t, "hardBreak", br["type"])
	_, hasMarks := br["marks"]
	assert.False(t, hasMarks, "hardBreak inside strong must not carry strong mark")

	last := inlines[2].(node)
	assert.Equal(t, "text", last["type"])
	lastMarks := last["marks"].([]any)
	require.Len(t, lastMarks, 1)
	assert.Equal(t, "strong", lastMarks[0].(node)["type"])
}

func TestToADF_LinkSpanningSoftBreak(t *testing.T) {
	// Link whose label wraps across a soft break — the hardBreak ends up
	// inside the Link subtree. The link mark must apply to text nodes only.
	result := ToADF("[line one\nline two](https://example.com)")
	require.NotNil(t, result)

	content := result["content"].([]any)
	para := content[0].(node)
	inlines := para["content"].([]any)
	require.Len(t, inlines, 3)

	first := inlines[0].(node)
	assert.Equal(t, "text", first["type"])
	firstMarks := first["marks"].([]any)
	require.Len(t, firstMarks, 1)
	firstMark := firstMarks[0].(node)
	assert.Equal(t, "link", firstMark["type"])
	assert.Equal(t, "https://example.com", firstMark["attrs"].(node)["href"])

	br := inlines[1].(node)
	assert.Equal(t, "hardBreak", br["type"])
	_, hasMarks := br["marks"]
	assert.False(t, hasMarks, "hardBreak inside link must not carry link mark")

	last := inlines[2].(node)
	assert.Equal(t, "text", last["type"])
	lastMarks := last["marks"].([]any)
	require.Len(t, lastMarks, 1)
	assert.Equal(t, "link", lastMarks[0].(node)["type"])
}

func TestToADF_SoftBreakInsideListItem(t *testing.T) {
	// List item with a lazy continuation line. The soft break must produce
	// a hardBreak inside the list item's paragraph.
	result := ToADF("- line one\n  line two\n- item two")
	require.NotNil(t, result)

	content := result["content"].([]any)
	list := content[0].(node)
	assert.Equal(t, "bulletList", list["type"])

	items := list["content"].([]any)
	require.Len(t, items, 2)

	first := items[0].(node)
	para := first["content"].([]any)[0].(node)
	assert.Equal(t, "paragraph", para["type"])
	inlines := para["content"].([]any)
	require.Len(t, inlines, 3)
	assert.Equal(t, "text", inlines[0].(node)["type"])
	assert.Equal(t, "line one", inlines[0].(node)["text"])
	assert.Equal(t, "hardBreak", inlines[1].(node)["type"])
	assert.Equal(t, "text", inlines[2].(node)["type"])
	assert.Equal(t, "line two", inlines[2].(node)["text"])
}

func TestToADF_SoftBreakInsideBlockquote(t *testing.T) {
	// Blockquote whose body wraps across two lines.
	result := ToADF("> line one\n> line two")
	require.NotNil(t, result)

	content := result["content"].([]any)
	bq := content[0].(node)
	assert.Equal(t, "blockquote", bq["type"])

	para := bq["content"].([]any)[0].(node)
	assert.Equal(t, "paragraph", para["type"])
	inlines := para["content"].([]any)
	require.Len(t, inlines, 3)
	assert.Equal(t, "text", inlines[0].(node)["type"])
	assert.Equal(t, "line one", inlines[0].(node)["text"])
	assert.Equal(t, "hardBreak", inlines[1].(node)["type"])
	assert.Equal(t, "text", inlines[2].(node)["type"])
	assert.Equal(t, "line two", inlines[2].(node)["text"])
}

func TestToADF_AutoLink(t *testing.T) {
	result := ToADF("<https://example.com>")
	require.NotNil(t, result)

	content := result["content"].([]any)
	para := content[0].(node)
	inlines := para["content"].([]any)

	found := false
	for _, inline := range inlines {
		n := inline.(node)
		if marks, ok := n["marks"]; ok {
			for _, mark := range marks.([]any) {
				if m, ok := mark.(node); ok && m["type"] == "link" {
					attrs := m["attrs"].(node)
					if attrs["href"] == "https://example.com" {
						found = true
					}
				}
			}
		}
	}
	assert.True(t, found, "expected autolink converted to link-marked text node")
}

func TestToADF_NestedList(t *testing.T) {
	md := "- item one\n  - nested a\n  - nested b\n- item two"
	result := ToADF(md)
	require.NotNil(t, result)

	content := result["content"].([]any)
	outerList := content[0].(node)
	assert.Equal(t, "bulletList", outerList["type"])

	items := outerList["content"].([]any)
	require.GreaterOrEqual(t, len(items), 2)

	// First list item should contain a nested bulletList.
	firstItem := items[0].(node)
	firstItemContent := firstItem["content"].([]any)
	found := false
	for _, c := range firstItemContent {
		if n, ok := c.(node); ok && n["type"] == "bulletList" {
			found = true
			nestedItems := n["content"].([]any)
			assert.Len(t, nestedItems, 2)
		}
	}
	assert.True(t, found, "expected nested bulletList inside first list item")
}

func TestToADF_CodeInsideBold_DropsBoldMark(t *testing.T) {
	result := ToADF("**`foo`**")
	require.NotNil(t, result)

	content := result["content"].([]any)
	para := content[0].(node)
	inlines := para["content"].([]any)
	require.Len(t, inlines, 1)

	text := inlines[0].(node)
	assert.Equal(t, "foo", text["text"])

	marks := text["marks"].([]any)
	require.Len(t, marks, 1)
	assert.Equal(t, "code", marks[0].(node)["type"])

	for _, mark := range marks {
		assert.NotEqual(t, "strong", mark.(node)["type"], "strong mark must not be combined with code")
	}
}

func TestToADF_CodeInsideEm_DropsEmMark(t *testing.T) {
	result := ToADF("*`foo`*")
	require.NotNil(t, result)

	content := result["content"].([]any)
	para := content[0].(node)
	inlines := para["content"].([]any)
	require.Len(t, inlines, 1)

	text := inlines[0].(node)
	assert.Equal(t, "foo", text["text"])

	marks := text["marks"].([]any)
	require.Len(t, marks, 1)
	assert.Equal(t, "code", marks[0].(node)["type"])

	for _, mark := range marks {
		assert.NotEqual(t, "em", mark.(node)["type"], "em mark must not be combined with code")
	}
}

func TestToADF_CodeInsideLink_KeepsBothMarks(t *testing.T) {
	result := ToADF("[`click`](https://example.com)")
	require.NotNil(t, result)

	content := result["content"].([]any)
	para := content[0].(node)
	inlines := para["content"].([]any)
	require.Len(t, inlines, 1)

	text := inlines[0].(node)
	assert.Equal(t, "click", text["text"])

	marks := text["marks"].([]any)
	require.Len(t, marks, 2)

	types := map[string]bool{}
	for _, mark := range marks {
		types[mark.(node)["type"].(string)] = true
	}
	assert.True(t, types["code"], "expected code mark")
	assert.True(t, types["link"], "expected link mark")
}

func TestToADF_MixedEmphasisChildren(t *testing.T) {
	result := ToADF("**plain and `code`**")
	require.NotNil(t, result)

	content := result["content"].([]any)
	para := content[0].(node)
	inlines := para["content"].([]any)
	require.Len(t, inlines, 2)

	plain := inlines[0].(node)
	assert.Equal(t, "plain and ", plain["text"])
	plainMarks := plain["marks"].([]any)
	require.Len(t, plainMarks, 1)
	assert.Equal(t, "strong", plainMarks[0].(node)["type"])

	code := inlines[1].(node)
	assert.Equal(t, "code", code["text"])
	codeMarks := code["marks"].([]any)
	require.Len(t, codeMarks, 1)
	assert.Equal(t, "code", codeMarks[0].(node)["type"])
}

func TestToADF_NestedBoldItalicCode(t *testing.T) {
	result := ToADF("***`foo`***")
	require.NotNil(t, result)

	content := result["content"].([]any)
	para := content[0].(node)
	inlines := para["content"].([]any)
	require.Len(t, inlines, 1)

	text := inlines[0].(node)
	assert.Equal(t, "foo", text["text"])

	marks := text["marks"].([]any)
	require.Len(t, marks, 1)
	assert.Equal(t, "code", marks[0].(node)["type"])
}

// TestToADF_WikiMarkupCurrentlyPassesThroughAsText pins the converter's
// behaviour today: wiki-markup input is parsed as plain-text Markdown and the
// original tokens survive verbatim in the ADF output. Detection of wiki-markup
// happens at the handler boundary (see internal/jiramcp), not inside ToADF.
// This test is a sanity check that the converter itself is not modified to
// silently strip or rewrite wiki-markup.
func TestToADF_WikiMarkupCurrentlyPassesThroughAsText(t *testing.T) {
	result := ToADF("{code:sql}select 1{code}")
	require.NotNil(t, result)

	rendered := collectText(result)
	assert.Contains(t, rendered, "{code:sql}")
}

// collectText walks the ADF doc and concatenates all text node values.
func collectText(n any) string {
	switch v := n.(type) {
	case node:
		if t, ok := v["type"].(string); ok && t == "text" {
			if s, ok := v["text"].(string); ok {
				return s
			}
		}
		if c, ok := v["content"].([]any); ok {
			var out string
			for _, child := range c {
				out += collectText(child)
			}
			return out
		}
	case []any:
		var out string
		for _, child := range v {
			out += collectText(child)
		}
		return out
	}
	return ""
}

// firstTextOfCell asserts the cell shape (type + single paragraph) and returns
// the cell's first inline text node so callers can check text and marks.
func firstTextOfCell(t *testing.T, cell any, wantCellType string) node {
	t.Helper()
	c := cell.(node)
	assert.Equal(t, wantCellType, c["type"])
	para := c["content"].([]any)[0].(node)
	assert.Equal(t, "paragraph", para["type"])
	return para["content"].([]any)[0].(node)
}

// rowCells asserts the row is a tableRow and returns its cells.
func rowCells(t *testing.T, row any) []any {
	t.Helper()
	r := row.(node)
	assert.Equal(t, "tableRow", r["type"])
	return r["content"].([]any)
}

func TestToADF_PipeTable(t *testing.T) {
	md := "| a | b |\n|---|---|\n| 1 | 2 |\n| 3 | 4 |\n"
	result := ToADF(md)
	require.NotNil(t, result)

	content := result["content"].([]any)
	require.Len(t, content, 1)

	table := content[0].(node)
	assert.Equal(t, "table", table["type"])

	rows := table["content"].([]any)
	require.Len(t, rows, 3, "header row plus two body rows; alignment row is consumed")

	expected := []struct {
		cellType string
		texts    []string
	}{
		{"tableHeader", []string{"a", "b"}},
		{"tableCell", []string{"1", "2"}},
		{"tableCell", []string{"3", "4"}},
	}
	for i, want := range expected {
		cells := rowCells(t, rows[i])
		require.Len(t, cells, len(want.texts))
		for j, wantText := range want.texts {
			text := firstTextOfCell(t, cells[j], want.cellType)
			assert.Equal(t, wantText, text["text"])
		}
	}
}

func TestToADF_PipeTable_InlineFormattingInCells(t *testing.T) {
	md := "| name | code |\n|---|---|\n| **bold** | `fmt.Println` |\n| [link](https://example.com) | plain |\n"
	result := ToADF(md)
	require.NotNil(t, result)

	table := result["content"].([]any)[0].(node)
	rows := table["content"].([]any)
	require.Len(t, rows, 3)

	row1Cells := rowCells(t, rows[1])
	bold := firstTextOfCell(t, row1Cells[0], "tableCell")
	assert.Equal(t, "bold", bold["text"])
	boldMarks := bold["marks"].([]any)
	require.Len(t, boldMarks, 1)
	assert.Equal(t, "strong", boldMarks[0].(node)["type"])

	code := firstTextOfCell(t, row1Cells[1], "tableCell")
	assert.Equal(t, "fmt.Println", code["text"])
	codeMarks := code["marks"].([]any)
	require.Len(t, codeMarks, 1)
	assert.Equal(t, "code", codeMarks[0].(node)["type"])

	row2Cells := rowCells(t, rows[2])
	link := firstTextOfCell(t, row2Cells[0], "tableCell")
	assert.Equal(t, "link", link["text"])
	linkMarks := link["marks"].([]any)
	require.Len(t, linkMarks, 1)
	linkMark := linkMarks[0].(node)
	assert.Equal(t, "link", linkMark["type"])
	assert.Equal(t, "https://example.com", linkMark["attrs"].(node)["href"])
}

func TestToADF_PipeTable_AlignmentRowVariants(t *testing.T) {
	cases := []struct {
		name string
		md   string
	}{
		{"plain", "| a | b |\n|---|---|\n| 1 | 2 |\n"},
		{"left_right", "| a | b |\n|:--|--:|\n| 1 | 2 |\n"},
		{"center", "| a | b |\n|:-:|:-:|\n| 1 | 2 |\n"},
		{"padded", "| a | b |\n| --- | --- |\n| 1 | 2 |\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := ToADF(tc.md)
			require.NotNil(t, result)
			content := result["content"].([]any)
			require.Len(t, content, 1)
			table := content[0].(node)
			assert.Equal(t, "table", table["type"], "alignment row variant must produce a table node")
			rows := table["content"].([]any)
			require.Len(t, rows, 2, "alignment row must be consumed, not emitted")
		})
	}
}

func TestToADF_ThematicBreak(t *testing.T) {
	result := ToADF("above\n\n---\n\nbelow")
	require.NotNil(t, result)

	content := result["content"].([]any)
	found := false
	for _, c := range content {
		if n, ok := c.(node); ok && n["type"] == "rule" {
			found = true
		}
	}
	assert.True(t, found, "expected a rule node for thematic break")
}
