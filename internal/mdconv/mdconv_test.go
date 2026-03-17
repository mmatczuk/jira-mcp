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
