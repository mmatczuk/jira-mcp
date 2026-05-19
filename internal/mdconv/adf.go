package mdconv

import (
	"encoding/json"
	"fmt"
	"strings"
)

const adfUnsupportedFence = "adf-unsupported"

// FromADF converts an ADF document map to Markdown. It is the inverse of
// ToADF for the subset of nodes ToADF emits (paragraph, heading, bulletList,
// orderedList, listItem, text + marks, hardBreak, codeBlock). Unknown block
// nodes are rendered as fenced ```adf-unsupported``` blocks carrying the raw
// node JSON; unknown inline nodes are dropped.
func FromADF(d map[string]any) (string, error) {
	blocks := contentOf(d)
	if len(blocks) == 0 {
		return "", nil
	}
	body, err := renderBlocks(blocks)
	if err != nil {
		return "", err
	}
	if body == "" {
		return "", nil
	}
	return body + "\n", nil
}

func renderBlocks(blocks []any) (string, error) {
	var rendered []string
	for _, b := range blocks {
		s, err := renderBlock(b)
		if err != nil {
			return "", err
		}
		if s == "" {
			continue
		}
		rendered = append(rendered, s)
	}
	return strings.Join(rendered, "\n\n"), nil
}

func renderBlock(n any) (string, error) {
	nm, ok := n.(map[string]any)
	if !ok {
		return "", fmt.Errorf("FromADF: expected node map, got %T", n)
	}
	switch t, _ := nm["type"].(string); t {
	case "paragraph":
		return renderInlines(contentOf(nm)), nil
	case "heading":
		return strings.Repeat("#", headingLevel(nm)) + " " + renderInlines(contentOf(nm)), nil
	case "bulletList":
		return renderList(nm, false), nil
	case "orderedList":
		return renderList(nm, true), nil
	case "codeBlock":
		return renderCodeBlock(nm), nil
	default:
		return fenceUnsupported(nm)
	}
}

func renderList(n map[string]any, ordered bool) string {
	items := contentOf(n)
	var lines []string
	for i, item := range items {
		im, ok := item.(map[string]any)
		if !ok {
			continue
		}
		marker := "- "
		if ordered {
			marker = fmt.Sprintf("%d. ", i+1)
		}
		lines = append(lines, renderListItem(im, marker))
	}
	return strings.Join(lines, "\n")
}

func renderListItem(item map[string]any, marker string) string {
	indent := strings.Repeat(" ", len(marker))
	var parts []string
	for j, b := range contentOf(item) {
		rendered, _ := renderBlock(b)
		if rendered == "" {
			continue
		}
		if j == 0 {
			parts = append(parts, prependPrefixIndentRest(rendered, marker, indent))
		} else {
			parts = append(parts, indentAll(rendered, indent))
		}
	}
	return strings.Join(parts, "\n")
}

func renderCodeBlock(n map[string]any) string {
	var body strings.Builder
	for _, c := range contentOf(n) {
		cm, ok := c.(map[string]any)
		if !ok {
			continue
		}
		if t, _ := cm["type"].(string); t == "text" {
			text, _ := cm["text"].(string)
			body.WriteString(text)
		}
	}
	lang := ""
	if attrs, ok := n["attrs"].(map[string]any); ok {
		lang, _ = attrs["language"].(string)
	}
	return "```" + lang + "\n" + strings.TrimRight(body.String(), "\n") + "\n```"
}

func renderInlines(content []any) string {
	var b strings.Builder
	for _, c := range content {
		cm, ok := c.(map[string]any)
		if !ok {
			continue
		}
		switch t, _ := cm["type"].(string); t {
		case "text":
			b.WriteString(renderText(cm))
		case "hardBreak":
			b.WriteString("\n")
		}
	}
	return b.String()
}

func renderText(n map[string]any) string {
	text, _ := n["text"].(string)
	marks, _ := n["marks"].([]any)

	s := text
	switch {
	case hasMarkType(marks, "code"):
		// ADF disallows combining the code mark with strong/em, so render code
		// before considering them.
		s = "`" + s + "`"
	default:
		if hasMarkType(marks, "strong") {
			s = "**" + s + "**"
		}
		if hasMarkType(marks, "em") {
			s = "*" + s + "*"
		}
	}
	if href := linkHref(marks); href != "" {
		s = "[" + s + "](" + href + ")"
	}
	return s
}

func findMark(marks []any, t string) (map[string]any, bool) {
	for _, m := range marks {
		mm, ok := m.(map[string]any)
		if !ok {
			continue
		}
		if mt, _ := mm["type"].(string); mt == t {
			return mm, true
		}
	}
	return nil, false
}

func hasMarkType(marks []any, t string) bool {
	_, ok := findMark(marks, t)
	return ok
}

func linkHref(marks []any) string {
	mark, ok := findMark(marks, "link")
	if !ok {
		return ""
	}
	attrs, _ := mark["attrs"].(map[string]any)
	href, _ := attrs["href"].(string)
	return href
}

func fenceUnsupported(n map[string]any) (string, error) {
	j, err := json.Marshal(n)
	if err != nil {
		return "", err
	}
	return "```" + adfUnsupportedFence + "\n" + string(j) + "\n```", nil
}

func contentOf(n map[string]any) []any {
	if n == nil {
		return nil
	}
	c, _ := n["content"].([]any)
	return c
}

func headingLevel(n map[string]any) int {
	attrs, _ := n["attrs"].(map[string]any)
	if l, ok := attrs["level"].(int); ok {
		return l
	}
	if l, ok := attrs["level"].(float64); ok {
		return int(l)
	}
	return 1
}

func prependPrefixIndentRest(s, prefix, indent string) string {
	lines := strings.Split(s, "\n")
	if len(lines) == 0 {
		return prefix
	}
	lines[0] = prefix + lines[0]
	for i := 1; i < len(lines); i++ {
		if lines[i] != "" {
			lines[i] = indent + lines[i]
		}
	}
	return strings.Join(lines, "\n")
}

func indentAll(s, indent string) string {
	lines := strings.Split(s, "\n")
	for i := range lines {
		if lines[i] != "" {
			lines[i] = indent + lines[i]
		}
	}
	return strings.Join(lines, "\n")
}
