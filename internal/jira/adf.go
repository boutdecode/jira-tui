package jira

import "strings"

// TextToADF wraps plain text (paragraphs separated by blank lines) in an ADF document.
func TextToADF(text string) map[string]any {
	var content []any
	for _, p := range strings.Split(strings.TrimSpace(text), "\n\n") {
		var inline []any
		for i, line := range strings.Split(p, "\n") {
			if i > 0 {
				inline = append(inline, map[string]any{"type": "hardBreak"})
			}
			if line != "" {
				inline = append(inline, map[string]any{"type": "text", "text": line})
			}
		}
		content = append(content, map[string]any{"type": "paragraph", "content": inline})
	}
	return map[string]any{"type": "doc", "version": 1, "content": content}
}

// ADFToText renders an ADF document (decoded JSON) as readable plain text.
// Unknown nodes are traversed so their text is never lost.
func ADFToText(doc any) string {
	var sb strings.Builder
	renderNode(&sb, doc, 0)
	return strings.TrimSpace(sb.String())
}

func renderNode(sb *strings.Builder, n any, depth int) {
	m, ok := n.(map[string]any)
	if !ok {
		return
	}
	typ, _ := m["type"].(string)
	children, _ := m["content"].([]any)
	each := func() {
		for _, c := range children {
			renderNode(sb, c, depth)
		}
	}
	switch typ {
	case "text":
		t, _ := m["text"].(string)
		sb.WriteString(t)
	case "hardBreak":
		sb.WriteString("\n")
	case "mention":
		if a, ok := m["attrs"].(map[string]any); ok {
			t, _ := a["text"].(string)
			sb.WriteString(t)
		}
	case "emoji", "inlineCard":
		if a, ok := m["attrs"].(map[string]any); ok {
			for _, k := range []string{"text", "shortName", "url"} {
				if t, _ := a[k].(string); t != "" {
					sb.WriteString(t)
					break
				}
			}
		}
	case "paragraph", "heading", "blockquote", "codeBlock":
		if typ == "heading" {
			sb.WriteString("# ")
		}
		if typ == "blockquote" {
			sb.WriteString("│ ")
		}
		each()
		sb.WriteString("\n\n")
	case "bulletList", "orderedList":
		for i, c := range children {
			sb.WriteString(strings.Repeat("  ", depth))
			if typ == "bulletList" {
				sb.WriteString("• ")
			} else {
				sb.WriteString(itoa(i+1) + ". ")
			}
			var item strings.Builder
			renderNode(&item, c, depth+1)
			sb.WriteString(strings.TrimSpace(item.String()) + "\n")
		}
		sb.WriteString("\n")
	default:
		each()
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for ; i > 0; i /= 10 {
		b = append([]byte{byte('0' + i%10)}, b...)
	}
	return string(b)
}
