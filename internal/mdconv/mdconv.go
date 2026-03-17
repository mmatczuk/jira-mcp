// Package mdconv converts Markdown text to Atlassian Document Format (ADF).
package mdconv

import (
	"bytes"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

type node = map[string]any

// md is a package-level goldmark instance. The default parser is stateless
// and safe for concurrent use.
var md = goldmark.New()

// ToADF converts a Markdown string to an ADF document map.
// Returns nil if the input is empty.
func ToADF(markdown string) node {
	if markdown == "" {
		return nil
	}

	reader := text.NewReader([]byte(markdown))
	doc := md.Parser().Parse(reader)

	content := walkChildren(doc, []byte(markdown))
	return node{
		"version": 1,
		"type":    "doc",
		"content": content,
	}
}

func walkChildren(n ast.Node, source []byte) []any {
	var result []any
	for child := n.FirstChild(); child != nil; child = child.NextSibling() {
		if nd := convertNode(child, source); nd != nil {
			result = append(result, nd)
		}
	}
	return result
}

func convertNode(n ast.Node, source []byte) node {
	switch n := n.(type) {
	case *ast.Paragraph:
		content := convertInlineChildren(n, source)
		if len(content) == 0 {
			return nil
		}
		return node{
			"type":    "paragraph",
			"content": content,
		}

	case *ast.Heading:
		return node{
			"type":    "heading",
			"attrs":   node{"level": n.Level},
			"content": convertInlineChildren(n, source),
		}

	case *ast.List:
		listType := "bulletList"
		if n.IsOrdered() {
			listType = "orderedList"
		}
		var items []any
		for child := n.FirstChild(); child != nil; child = child.NextSibling() {
			if item := convertListItem(child, source); item != nil {
				items = append(items, item)
			}
		}
		return node{
			"type":    listType,
			"content": items,
		}

	case *ast.FencedCodeBlock:
		var buf bytes.Buffer
		lines := n.Lines()
		for i := 0; i < lines.Len(); i++ {
			line := lines.At(i)
			buf.Write(line.Value(source))
		}
		lang := string(n.Language(source))
		nd := node{
			"type":    "codeBlock",
			"content": []any{node{"type": "text", "text": buf.String()}},
		}
		if lang != "" {
			nd["attrs"] = node{"language": lang}
		}
		return nd

	case *ast.CodeBlock:
		var buf bytes.Buffer
		lines := n.Lines()
		for i := 0; i < lines.Len(); i++ {
			line := lines.At(i)
			buf.Write(line.Value(source))
		}
		return node{
			"type":    "codeBlock",
			"content": []any{node{"type": "text", "text": buf.String()}},
		}

	case *ast.Blockquote:
		return node{
			"type":    "blockquote",
			"content": walkChildren(n, source),
		}

	case *ast.ThematicBreak:
		return node{"type": "rule"}

	default:
		content := convertInlineChildren(n, source)
		if len(content) > 0 {
			return node{
				"type":    "paragraph",
				"content": content,
			}
		}
		return nil
	}
}

func convertListItem(n ast.Node, source []byte) node {
	var content []any
	for child := n.FirstChild(); child != nil; child = child.NextSibling() {
		if nd := convertNode(child, source); nd != nil {
			content = append(content, nd)
		}
	}
	if len(content) == 0 {
		return nil
	}
	return node{
		"type":    "listItem",
		"content": content,
	}
}

func convertInlineChildren(n ast.Node, source []byte) []any {
	var result []any
	for child := n.FirstChild(); child != nil; child = child.NextSibling() {
		result = append(result, convertInline(child, source)...)
	}
	return result
}

func convertInline(n ast.Node, source []byte) []any {
	switch n := n.(type) {
	case *ast.Text:
		t := string(n.Segment.Value(source))
		if t == "" && !n.HardLineBreak() {
			return nil
		}
		var result []any
		if t != "" {
			result = append(result, node{"type": "text", "text": t})
		}
		if n.HardLineBreak() {
			result = append(result, node{"type": "hardBreak"})
		}
		return result

	case *ast.String:
		t := string(n.Value)
		if t == "" {
			return nil
		}
		return []any{node{"type": "text", "text": t}}

	case *ast.CodeSpan:
		var buf bytes.Buffer
		for child := n.FirstChild(); child != nil; child = child.NextSibling() {
			if t, ok := child.(*ast.Text); ok {
				buf.Write(t.Segment.Value(source))
			}
		}
		return []any{node{
			"type":  "text",
			"text":  buf.String(),
			"marks": []any{node{"type": "code"}},
		}}

	case *ast.Emphasis:
		markType := "em"
		if n.Level == 2 {
			markType = "strong"
		}
		children := convertInlineChildren(n, source)
		for _, child := range children {
			if m, ok := child.(node); ok {
				marks, _ := m["marks"].([]any)
				marks = append(marks, node{"type": markType})
				m["marks"] = marks
			}
		}
		return children

	case *ast.Link:
		children := convertInlineChildren(n, source)
		for _, child := range children {
			if m, ok := child.(node); ok {
				marks, _ := m["marks"].([]any)
				marks = append(marks, node{
					"type":  "link",
					"attrs": node{"href": string(n.Destination)},
				})
				m["marks"] = marks
			}
		}
		return children

	case *ast.Image:
		// Images are block-level in ADF (mediaSingle), but goldmark treats them
		// as inline within paragraphs. Emit as a linked text node instead,
		// since mediaSingle cannot appear inside a paragraph.
		alt := extractText(n, source)
		if alt == "" {
			alt = string(n.Destination)
		}
		return []any{node{
			"type": "text",
			"text": alt,
			"marks": []any{node{
				"type":  "link",
				"attrs": node{"href": string(n.Destination)},
			}},
		}}

	case *ast.AutoLink:
		url := string(n.URL(source))
		return []any{node{
			"type": "text",
			"text": url,
			"marks": []any{node{
				"type":  "link",
				"attrs": node{"href": url},
			}},
		}}

	default:
		return convertInlineChildren(n, source)
	}
}

// extractText concatenates all text segments from child Text nodes.
func extractText(n ast.Node, source []byte) string {
	var buf bytes.Buffer
	for child := n.FirstChild(); child != nil; child = child.NextSibling() {
		if t, ok := child.(*ast.Text); ok {
			buf.Write(t.Segment.Value(source))
		}
	}
	return buf.String()
}
