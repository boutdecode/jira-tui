package jira

// Markdown <-> ADF, used to edit descriptions and comments in the app.
//
// The Markdown dialect is deliberately small and strict:
//
//	paragraphs        separated by a blank line; a single newline is a line break
//	headings          # … ######
//	lists             "- item", "1. item", nested by indenting; an item is one paragraph (+ nested lists)
//	code blocks       ``` fences with an optional language
//	quotes            > paragraph
//	rule              ---
//	inline            **bold**  *italic* or _italic_  ~~strike~~  `code`  [text](url)
//	escapes           a backslash before any ASCII punctuation makes it literal
//
// Lossless editing is guaranteed by verification, not by trust: ADFToMarkdown only
// returns a text when converting it back gives exactly the same document (up to
// insignificant details such as empty paragraphs). Anything else — mentions, images,
// tables, panels, colors, underline… — is reported as not editable, so an edit can
// never silently destroy content.

import (
	"encoding/json"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// --------------------------------------------------------------------------------------
// shared helpers

// mark is an inline mark. Unsupported ADF marks are kept as typ "?<name>" so that they
// make the round-trip check fail instead of vanishing.
type mark struct{ typ, href, title string }

// seg is a piece of inline content: a run of text with marks, or an atom: a line break,
// a mention (mid = account id, mname = display name), a smart link (card = url) or an
// unsupported node (x holds its type).
type seg struct {
	text  string
	marks []mark
	brk   bool
	x     string
	mid   string
	mname string
	card  string
}

func (s seg) atom() bool { return s.brk || s.x != "" || s.mid != "" || s.card != "" }

func (m mark) key() string {
	if m.typ == "link" {
		return "link|" + m.href + "|" + m.title
	}
	return m.typ
}

func marksKey(ms []mark) string {
	ks := make([]string, len(ms))
	for i, m := range ms {
		ks[i] = m.key()
	}
	sort.Strings(ks)
	return strings.Join(ks, ",")
}

func (m mark) adf() map[string]any {
	if m.typ == "link" {
		attrs := map[string]any{"href": m.href}
		if m.title != "" {
			attrs["title"] = m.title
		}
		return map[string]any{"type": "link", "attrs": attrs}
	}
	return map[string]any{"type": m.typ}
}

func children(n map[string]any) []any {
	c, _ := n["content"].([]any)
	return c
}

func attrsOf(n map[string]any) map[string]any {
	a, _ := n["attrs"].(map[string]any)
	return a
}

func num(v any) (int, bool) {
	switch x := v.(type) {
	case int:
		return x, true
	case int64:
		return int(x), true
	case float64:
		return int(x), true
	case json.Number:
		n, err := x.Int64()
		return int(n), err == nil
	}
	return 0, false
}

func typeOf(n map[string]any) string {
	t, _ := n["type"].(string)
	return t
}

// readInline turns ADF inline content into segments.
func readInline(content []any) []seg {
	var out []seg
	for _, c := range content {
		n, ok := c.(map[string]any)
		if !ok {
			out = append(out, seg{x: "?"})
			continue
		}
		switch t := typeOf(n); t {
		case "text":
			text, _ := n["text"].(string)
			var ms []mark
			rawMarks, _ := n["marks"].([]any)
			for _, rm := range rawMarks {
				mm, _ := rm.(map[string]any)
				switch mt := typeOf(mm); mt {
				case "strong", "em", "strike", "code":
					ms = append(ms, mark{typ: mt})
				case "link":
					a := attrsOf(mm)
					href, _ := a["href"].(string)
					title, _ := a["title"].(string)
					ms = append(ms, mark{typ: "link", href: href, title: title})
				default:
					ms = append(ms, mark{typ: "?" + mt})
				}
			}
			out = append(out, seg{text: text, marks: ms})
		case "hardBreak":
			out = append(out, seg{brk: true})
		case "mention":
			a := attrsOf(n)
			id, _ := a["id"].(string)
			text, _ := a["text"].(string)
			name := strings.TrimSpace(strings.TrimPrefix(text, "@"))
			if id == "" || name == "" || strings.Contains(name, "\n") {
				out = append(out, seg{x: "mention"})
			} else {
				out = append(out, seg{mid: id, mname: name})
			}
		case "inlineCard":
			url, _ := attrsOf(n)["url"].(string)
			if url == "" {
				out = append(out, seg{x: "inlineCard"})
			} else {
				out = append(out, seg{card: url})
			}
		default:
			out = append(out, seg{x: t})
		}
	}
	return out
}

// unit is one character, or one atom, with its marks.
type unit struct {
	r     rune
	marks []mark
	atom  *seg
}

func hasMark(u unit, key string) bool {
	if u.atom != nil {
		return false
	}
	return hasKey(u.marks, key)
}

func hasKey(ms []mark, key string) bool {
	for _, m := range ms {
		if m.key() == key {
			return true
		}
	}
	return false
}

func without(ms []mark, key string) []mark {
	return slices.DeleteFunc(slices.Clone(ms), func(m mark) bool { return m.key() == key })
}

// normalize makes two equivalent inline contents identical: for each mark, the spaces at
// the edges of a run carrying it leave the mark (Markdown cannot express them), empty runs
// disappear and neighbouring runs with the same marks merge.
func normalize(segs []seg) []seg {
	var us []unit
	for _, s := range segs {
		if s.atom() {
			a := s
			us = append(us, unit{atom: &a})
			continue
		}
		for _, r := range s.text {
			us = append(us, unit{r: r, marks: s.marks})
		}
	}
	keys := map[string]bool{}
	for _, u := range us {
		for _, m := range u.marks {
			if m.typ != "code" { // spaces are significant in a code span
				keys[m.key()] = true
			}
		}
	}
	isSpace := func(r rune) bool { return r == ' ' || r == '\t' }
	for k := range keys {
		for i := 0; i < len(us); {
			if !hasMark(us[i], k) {
				i++
				continue
			}
			j := i
			for j < len(us) && hasMark(us[j], k) {
				j++
			}
			lo, hi := i, j-1
			for lo <= hi && isSpace(us[lo].r) {
				us[lo].marks = without(us[lo].marks, k)
				lo++
			}
			for hi >= lo && isSpace(us[hi].r) {
				us[hi].marks = without(us[hi].marks, k)
				hi--
			}
			i = j
		}
	}
	var out []seg
	for _, u := range us {
		if u.atom != nil {
			out = append(out, *u.atom)
			continue
		}
		if n := len(out); n > 0 && !out[n-1].atom() && marksKey(out[n-1].marks) == marksKey(u.marks) {
			out[n-1].text += string(u.r)
			continue
		}
		out = append(out, seg{text: string(u.r), marks: u.marks})
	}
	return out
}

// --------------------------------------------------------------------------------------
// canonical form (the round-trip check compares these)

func canon(doc any) string {
	d, _ := doc.(map[string]any)
	b, _ := json.Marshal(canonBlocks(children(d)))
	return string(b)
}

func canonBlocks(content []any) []any {
	out := []any{}
	for _, c := range content {
		n, ok := c.(map[string]any)
		if !ok {
			out = append(out, "?")
			continue
		}
		if typeOf(n) == "paragraph" && len(children(n)) == 0 {
			continue // spacing only
		}
		out = append(out, canonNode(n))
	}
	return out
}

func codeText(n map[string]any) string {
	var sb strings.Builder
	for _, c := range children(n) {
		if cn, ok := c.(map[string]any); ok {
			t, _ := cn["text"].(string)
			sb.WriteString(t)
		}
	}
	return sb.String()
}

func canonNode(n map[string]any) map[string]any {
	t := typeOf(n)
	out := map[string]any{"t": t}
	a := attrsOf(n)
	switch t {
	case "paragraph":
		out["in"] = canonInline(children(n))
	case "heading":
		lvl, _ := num(a["level"])
		out["level"] = lvl
		out["in"] = canonInline(children(n))
	case "codeBlock":
		lang, _ := a["language"].(string)
		out["lang"] = lang
		out["text"] = codeText(n)
	case "orderedList":
		order, ok := num(a["order"])
		if !ok {
			order = 1
		}
		out["order"] = order
		out["c"] = canonBlocks(children(n))
	default:
		out["c"] = canonBlocks(children(n))
	}
	if ms, _ := n["marks"].([]any); len(ms) > 0 { // alignment, indentation…
		out["nodemarks"] = len(ms)
	}
	return out
}

func canonInline(content []any) []any {
	out := []any{}
	for _, s := range normalize(readInline(content)) {
		switch {
		case s.brk:
			out = append(out, map[string]any{"br": true})
		case s.x != "":
			out = append(out, map[string]any{"x": s.x})
		case s.mid != "":
			out = append(out, map[string]any{"mention": s.mid, "name": s.mname})
		case s.card != "":
			out = append(out, map[string]any{"card": s.card})
		default:
			out = append(out, map[string]any{"s": s.text, "m": marksKey(s.marks)})
		}
	}
	return out
}

// --------------------------------------------------------------------------------------
// ADF -> Markdown

// Converter converts between ADF and Markdown. The zero value knows neither people nor
// tickets; the two fields give it what it needs to read and write mentions and smart links.
type Converter struct {
	// BrowseURL returns the web address of an issue; BrowseURL("") is the common prefix.
	// "#PAY-142" in the Markdown is a smart link to BrowseURL("PAY-142").
	BrowseURL func(key string) string
	// Mentions maps a display name to an account id. "@Sofia M." in the Markdown is a mention
	// of that account when "Sofia M." is a key of this table.
	Mentions map[string]string
}

type emitter struct {
	ok    bool
	conv  Converter
	names []string // every known mention name (they must not be taken for mentions when literal)
}

// ADFToMarkdown is ToMarkdown without people or tickets; a document with mentions is refused.
func ADFToMarkdown(doc any) (md string, ok bool) {
	md, mentions, ok := Converter{}.ToMarkdown(doc)
	if len(mentions) > 0 {
		return "", false
	}
	return md, ok
}

// collectMentions lists the mentions of a document; conflict is true when one name stands
// for two accounts (the Markdown could not tell them apart).
func collectMentions(n any, into map[string]string) (conflict bool) {
	switch v := n.(type) {
	case map[string]any:
		if typeOf(v) == "mention" {
			a := attrsOf(v)
			id, _ := a["id"].(string)
			text, _ := a["text"].(string)
			name := strings.TrimSpace(strings.TrimPrefix(text, "@"))
			if id != "" && name != "" {
				if old, ok := into[name]; ok && old != id {
					return true
				}
				into[name] = id
			}
		}
		for _, c := range children(v) {
			if collectMentions(c, into) {
				return true
			}
		}
	}
	return false
}

// ToMarkdown returns the Markdown form of an ADF document and the table of the mentions it
// contains (merged with c.Mentions). ok is true only when editing that Markdown loses
// nothing: the text converts back to the same document.
func (c Converter) ToMarkdown(doc any) (md string, mentions map[string]string, ok bool) {
	mentions = map[string]string{}
	if doc == nil {
		return "", mentions, true
	}
	d, isMap := doc.(map[string]any)
	if !isMap || typeOf(d) != "doc" {
		return "", nil, false
	}
	for k, v := range c.Mentions {
		mentions[k] = v
	}
	if collectMentions(d, mentions) {
		return "", nil, false
	}
	conv := Converter{BrowseURL: c.BrowseURL, Mentions: mentions}
	e := &emitter{ok: true, conv: conv}
	for n := range mentions {
		e.names = append(e.names, n)
	}
	md = e.blocks(children(d))
	if !e.ok {
		return "", nil, false
	}
	if canon(doc) != canon(conv.ToADF(md)) {
		return "", nil, false
	}
	return md, mentions, true
}

func (e *emitter) blocks(content []any) string {
	var parts []string
	for _, c := range content {
		n, isMap := c.(map[string]any)
		if !isMap {
			e.ok = false
			return ""
		}
		if typeOf(n) == "paragraph" && len(children(n)) == 0 {
			continue
		}
		if s := e.block(n); s != "" || !e.ok {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, "\n\n")
}

func (e *emitter) block(n map[string]any) string {
	switch t := typeOf(n); t {
	case "paragraph":
		return e.paragraph(n)
	case "heading":
		lvl, ok := num(attrsOf(n)["level"])
		if !ok || lvl < 1 || lvl > 6 {
			e.ok = false
			return ""
		}
		text := e.inline(children(n))
		if strings.Contains(text, "\n") {
			e.ok = false
		}
		return strings.Repeat("#", lvl) + " " + text
	case "bulletList", "orderedList":
		return e.list(n, 0)
	case "codeBlock":
		text := codeText(n)
		lang, _ := attrsOf(n)["language"].(string)
		for _, l := range strings.Split(text, "\n") {
			if strings.HasPrefix(l, "```") {
				e.ok = false
			}
		}
		if strings.ContainsAny(lang, "\n`") {
			e.ok = false
		}
		return "```" + lang + "\n" + text + "\n```"
	case "blockquote":
		var parts []string
		for _, c := range children(n) {
			p, isMap := c.(map[string]any)
			if !isMap || typeOf(p) != "paragraph" {
				e.ok = false
				return ""
			}
			var lines []string
			for _, l := range strings.Split(e.paragraph(p), "\n") {
				lines = append(lines, "> "+l)
			}
			parts = append(parts, strings.Join(lines, "\n"))
		}
		return strings.Join(parts, "\n>\n")
	case "rule":
		return "---"
	default:
		e.ok = false
		return ""
	}
}

func (e *emitter) paragraph(n map[string]any) string {
	lines := strings.Split(e.inline(children(n)), "\n")
	for i, l := range lines {
		lines[i] = escapeLineStart(l)
	}
	return strings.Join(lines, "\n")
}

func (e *emitter) list(n map[string]any, indent int) string {
	ordered := typeOf(n) == "orderedList"
	start := 1
	if o, ok := num(attrsOf(n)["order"]); ok {
		start = o
	}
	pad := strings.Repeat(" ", indent)
	var lines []string
	for i, c := range children(n) {
		item, isMap := c.(map[string]any)
		if !isMap || typeOf(item) != "listItem" {
			e.ok = false
			return ""
		}
		marker := "- "
		if ordered {
			marker = strconv.Itoa(start+i) + ". "
		}
		kids := children(item)
		first := ""
		if len(kids) > 0 {
			p, isMap := kids[0].(map[string]any)
			if !isMap || typeOf(p) != "paragraph" {
				e.ok = false
				return ""
			}
			first = e.paragraph(p)
			kids = kids[1:]
		}
		for j, l := range strings.Split(first, "\n") {
			if j == 0 {
				lines = append(lines, pad+marker+l)
			} else {
				lines = append(lines, pad+"  "+l)
			}
		}
		for _, k := range kids {
			sub, isMap := k.(map[string]any)
			if !isMap || (typeOf(sub) != "bulletList" && typeOf(sub) != "orderedList") {
				e.ok = false
				return ""
			}
			lines = append(lines, e.list(sub, indent+2))
		}
	}
	return strings.Join(lines, "\n")
}

// markOrder is the nesting order when several marks cover the same text, outermost first.
var markOrder = []string{"link", "strong", "strike", "em", "code"}

func (e *emitter) inline(content []any) string {
	return e.run(normalize(readInline(content)), map[string]bool{})
}

// run writes segments, grouping neighbours that share a mark under one pair of delimiters
// so that "**bold _and italic_**" stays nested instead of being cut in pieces.
func (e *emitter) run(segs []seg, ctx map[string]bool) string {
	var sb strings.Builder
	for i := 0; i < len(segs); {
		s := segs[i]
		if s.atom() {
			sb.WriteString(e.atom(s))
			i++
			continue
		}
		var pick *mark
		for _, typ := range markOrder {
			for k := range s.marks {
				if s.marks[k].typ == typ {
					pick = &s.marks[k]
					break
				}
			}
			if pick != nil {
				break
			}
		}
		for _, m := range s.marks {
			if strings.HasPrefix(m.typ, "?") {
				e.ok = false
			}
		}
		switch {
		case pick == nil:
			sb.WriteString(escapeText(s.text, ctx["link"], e.names))
			i++
		case pick.typ == "code":
			if strings.Contains(s.text, "`") {
				e.ok = false
			}
			sb.WriteString("`" + s.text + "`")
			i++
		default:
			key := pick.key()
			j := i
			var group []seg
			for j < len(segs) && !segs[j].atom() && hasKey(segs[j].marks, key) {
				g := segs[j]
				g.marks = without(g.marks, key)
				group = append(group, g)
				j++
			}
			inner := ctx2(ctx, pick.typ)
			sb.WriteString(e.wrap(*pick, e.run(group, inner), ctx))
			i = j
		}
	}
	return sb.String()
}

// atom writes a line break, a mention or a smart link.
func (e *emitter) atom(s seg) string {
	switch {
	case s.brk:
		return "\n"
	case s.mid != "":
		if id, ok := e.conv.Mentions[s.mname]; !ok || id != s.mid {
			e.ok = false
		}
		return "@" + s.mname
	case s.card != "":
		if e.conv.BrowseURL != nil {
			prefix := e.conv.BrowseURL("")
			if strings.HasPrefix(s.card, prefix) && isTicketKey(s.card[len(prefix):]) {
				return "#" + s.card[len(prefix):]
			}
		}
		if !strings.HasPrefix(s.card, "http://") && !strings.HasPrefix(s.card, "https://") || strings.ContainsAny(s.card, " \t\n<>") {
			e.ok = false
		}
		return "<" + s.card + ">"
	}
	e.ok = false
	return ""
}

// scanTicketKey returns the issue key at the start of s ("PAY-142"), or "" when there is none.
func scanTicketKey(s string) string {
	i := 0
	if i >= len(s) || s[i] < 'A' || s[i] > 'Z' {
		return ""
	}
	for i < len(s) && (s[i] >= 'A' && s[i] <= 'Z' || s[i] >= '0' && s[i] <= '9' || s[i] == '_') {
		i++
	}
	if i >= len(s) || s[i] != '-' {
		return ""
	}
	i++
	j := i
	for j < len(s) && s[j] >= '0' && s[j] <= '9' {
		j++
	}
	if j == i {
		return ""
	}
	return s[:j]
}

func isTicketKey(s string) bool { return s != "" && scanTicketKey(s) == s }

// ticketKeyAt is scanTicketKey for a "#" reference: the key must end the word.
func ticketKeyAt(s string) string {
	k := scanTicketKey(s)
	if k == "" {
		return ""
	}
	if r := runeAt(s, len(k)); isWord(r) || r == '-' {
		return ""
	}
	return k
}

// autolinkEnd returns the length of the URL of an "<https://…>" at the start of s (which
// follows the "<"), or 0.
func autolinkEnd(s string) int {
	if !strings.HasPrefix(s, "http://") && !strings.HasPrefix(s, "https://") {
		return 0
	}
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '>':
			if i > len("https://") {
				return i
			}
			return 0
		case ' ', '\t', '\n', '<':
			return 0
		}
	}
	return 0
}

func ctx2(ctx map[string]bool, typ string) map[string]bool {
	out := map[string]bool{typ: true}
	for k, v := range ctx {
		out[k] = v
	}
	return out
}

func (e *emitter) wrap(m mark, inner string, ctx map[string]bool) string {
	switch m.typ {
	case "strong":
		return "**" + inner + "**"
	case "strike":
		return "~~" + inner + "~~"
	case "em":
		d := "*"
		if ctx["strong"] {
			d = "_" // "***x***" would be ambiguous
		}
		return d + inner + d
	case "link":
		if m.title != "" || m.href == "" || strings.ContainsAny(m.href, " \t\n") {
			e.ok = false
		}
		return "[" + inner + "](" + escapeURL(m.href) + ")"
	}
	e.ok = false
	return inner
}

const asciiPunct = "!\"#$%&'()*+,-./:;<=>?@[\\]^_`{|}~"

func isASCIIPunct(r rune) bool { return r < 128 && r != 0 && strings.ContainsRune(asciiPunct, r) }

func isWord(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }

// escapeText escapes what the Markdown parser would take for markup; text that cannot
// be mistaken (a snake_case word, "2 * 3", a plain [WIP]) is left readable.
func escapeText(s string, inLink bool, names []string) string {
	rs := []rune(s)
	var b strings.Builder
	for i, r := range rs {
		var prev, next rune
		if i > 0 {
			prev = rs[i-1]
		}
		if i+1 < len(rs) {
			next = rs[i+1]
		}
		esc := false
		switch r {
		case '\\':
			esc = next == 0 || isASCIIPunct(next)
		case '`':
			esc = true
		case '*':
			esc = !(prev == ' ' && next == ' ')
		case '_':
			esc = !((isWord(prev) && isWord(next)) || (prev == ' ' && next == ' '))
		case '~':
			esc = prev == '~' || next == '~'
		case '@':
			if !isWord(prev) {
				rest := string(rs[i+1:])
				for _, n := range names {
					if strings.HasPrefix(rest, n) && !isWord(runeAt(rest, len(n))) {
						esc = true
					}
				}
			}
		case '#':
			esc = !isWord(prev) && ticketKeyAt(string(rs[i+1:])) != ""
		case '<':
			esc = autolinkEnd(string(rs[i+1:])) > 0
		case '[':
			esc = inLink || strings.Contains(string(rs[i+1:]), "](")
		case ']':
			esc = inLink || next == '('
		}
		if esc {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

func escapeURL(u string) string {
	var b strings.Builder
	for _, r := range u {
		if r == '\\' || r == ')' || r == '(' {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// escapeLineStart protects a line that would otherwise start a block.
func escapeLineStart(l string) string {
	switch {
	case strings.HasPrefix(l, ">"), strings.HasPrefix(l, "```"):
		return `\` + l
	case strings.HasPrefix(l, "- "), strings.HasPrefix(l, "+ "), l == "-", l == "+", strings.TrimRight(l, " \t") == "---":
		return `\` + l
	case strings.HasPrefix(l, "#"):
		h := strings.TrimLeft(l, "#")
		if len(l)-len(h) <= 6 && (h == "" || h[0] == ' ') {
			return `\` + l
		}
	}
	i := 0
	for i < len(l) && l[i] >= '0' && l[i] <= '9' {
		i++
	}
	if i > 0 && i < len(l) && (l[i] == '.' || l[i] == ')') && (i+1 == len(l) || l[i+1] == ' ') {
		return l[:i] + `\` + l[i:]
	}
	return l
}

// --------------------------------------------------------------------------------------
// Markdown -> ADF

// MarkdownToADF parses the dialect described at the top of the file, without people or tickets.
func MarkdownToADF(md string) Doc { return Converter{}.ToADF(md) }

// inlineCtx is what the inline parser knows about people and tickets.
type inlineCtx struct {
	names    []string // mention names, longest first
	mentions map[string]string
	browse   func(key string) string
}

// ToADF parses the dialect described at the top of the file. It never fails: anything it
// does not recognise stays literal text.
func (c Converter) ToADF(md string) Doc {
	md = strings.ReplaceAll(md, "\r\n", "\n")
	md = strings.Trim(md, "\n")
	var lines []string
	if strings.TrimSpace(md) != "" {
		lines = strings.Split(md, "\n")
		inFence := false
		for i, l := range lines {
			switch {
			case strings.HasPrefix(l, "```"):
				inFence = !inFence || strings.TrimRight(l, " ") != "```"
			case !inFence: // tabs are only meaningful as indentation, never inside code
				trimmed := strings.TrimLeft(l, "\t")
				lines[i] = strings.Repeat("    ", len(l)-len(trimmed)) + trimmed
			}
		}
	}
	ic := &inlineCtx{mentions: c.Mentions, browse: c.BrowseURL}
	for n := range c.Mentions {
		ic.names = append(ic.names, n)
	}
	sort.Slice(ic.names, func(a, b int) bool {
		if len(ic.names[a]) != len(ic.names[b]) {
			return len(ic.names[a]) > len(ic.names[b])
		}
		return ic.names[a] < ic.names[b]
	})
	p := &mdParser{lines: lines, ic: ic}
	return Doc{"type": "doc", "version": 1, "content": p.blocks()}
}

type mdParser struct {
	lines []string
	i     int
	ic    *inlineCtx
}

type listLine struct {
	indent  int
	ordered bool
	num     int
	text    string
}

func parseListLine(l string) (listLine, bool) {
	indent := len(l) - len(strings.TrimLeft(l, " "))
	rest := l[indent:]
	if rest == "" {
		return listLine{}, false
	}
	var ll listLine
	ll.indent = indent
	switch rest[0] {
	case '-', '*', '+':
		rest = rest[1:]
	default:
		j := 0
		for j < len(rest) && rest[j] >= '0' && rest[j] <= '9' {
			j++
		}
		if j == 0 || j > 9 || j >= len(rest) || (rest[j] != '.' && rest[j] != ')') {
			return listLine{}, false
		}
		ll.ordered = true
		ll.num, _ = strconv.Atoi(rest[:j])
		rest = rest[j+1:]
	}
	if rest == "" || rest[0] != ' ' {
		return listLine{}, false
	}
	ll.text = strings.TrimPrefix(rest, " ")
	return ll, true
}

func headingLine(l string) (int, string, bool) {
	h := strings.TrimLeft(l, "#")
	n := len(l) - len(h)
	if n < 1 || n > 6 || h == "" || h[0] != ' ' {
		return 0, "", false
	}
	return n, strings.TrimSpace(h), true
}

func isBlank(l string) bool { return strings.TrimSpace(l) == "" }

func (p *mdParser) startsBlock(l string) bool {
	if strings.HasPrefix(l, "```") || strings.HasPrefix(l, ">") || strings.TrimRight(l, " ") == "---" {
		return true
	}
	if _, _, ok := headingLine(l); ok {
		return true
	}
	_, ok := parseListLine(l)
	return ok
}

func (p *mdParser) blocks() []any {
	content := []any{}
	for p.i < len(p.lines) {
		l := p.lines[p.i]
		switch {
		case isBlank(l):
			p.i++
		case strings.HasPrefix(l, "```"):
			content = append(content, p.fence())
		case strings.TrimRight(l, " ") == "---":
			content = append(content, map[string]any{"type": "rule"})
			p.i++
		case strings.HasPrefix(l, ">"):
			content = append(content, p.quote())
		default:
			if lvl, text, ok := headingLine(l); ok {
				h := map[string]any{"type": "heading", "attrs": map[string]any{"level": lvl}}
				if c := p.inlineNodes(text); len(c) > 0 {
					h["content"] = c
				}
				content = append(content, h)
				p.i++
				continue
			}
			if _, ok := parseListLine(l); ok {
				content = append(content, p.list())
				continue
			}
			content = append(content, p.paragraph())
		}
	}
	return content
}

func (p *mdParser) fence() map[string]any {
	lang := strings.TrimSpace(strings.TrimPrefix(p.lines[p.i], "```"))
	p.i++
	var body []string
	for p.i < len(p.lines) {
		if strings.TrimRight(p.lines[p.i], " ") == "```" {
			p.i++
			break
		}
		body = append(body, p.lines[p.i])
		p.i++
	}
	n := map[string]any{"type": "codeBlock"}
	if lang != "" {
		n["attrs"] = map[string]any{"language": lang}
	}
	if text := strings.Join(body, "\n"); text != "" {
		n["content"] = []any{map[string]any{"type": "text", "text": text}}
	}
	return n
}

func (p *mdParser) quote() map[string]any {
	var paras [][]string
	cur := []string{}
	for p.i < len(p.lines) && strings.HasPrefix(p.lines[p.i], ">") {
		l := strings.TrimPrefix(strings.TrimPrefix(p.lines[p.i], ">"), " ")
		p.i++
		if strings.TrimSpace(l) == "" {
			if len(cur) > 0 {
				paras = append(paras, cur)
				cur = []string{}
			}
			continue
		}
		cur = append(cur, l)
	}
	if len(cur) > 0 {
		paras = append(paras, cur)
	}
	content := []any{}
	for _, ls := range paras {
		content = append(content, p.paragraphNode(strings.Join(ls, "\n")))
	}
	return map[string]any{"type": "blockquote", "content": content}
}

func (p *mdParser) paragraphNode(text string) map[string]any {
	n := map[string]any{"type": "paragraph"}
	if c := p.inlineNodes(text); len(c) > 0 {
		n["content"] = c
	}
	return n
}

func (p *mdParser) paragraph() map[string]any {
	var ls []string
	for p.i < len(p.lines) {
		l := p.lines[p.i]
		if isBlank(l) || (len(ls) > 0 && p.startsBlock(l)) {
			break
		}
		ls = append(ls, l)
		p.i++
	}
	return p.paragraphNode(strings.Join(ls, "\n"))
}

func (p *mdParser) list() map[string]any {
	first, _ := parseListLine(p.lines[p.i])
	base, ordered := first.indent, first.ordered
	var items []any
	for {
		// the next list line of the same list may follow blank lines
		j := p.i
		for j < len(p.lines) && isBlank(p.lines[j]) {
			j++
		}
		if j >= len(p.lines) {
			break
		}
		ll, ok := parseListLine(p.lines[j])
		if !ok || ll.indent != base || ll.ordered != ordered {
			break
		}
		p.i = j + 1
		text := []string{ll.text}
		for p.i < len(p.lines) {
			l := p.lines[p.i]
			if isBlank(l) {
				break
			}
			if _, isItem := parseListLine(l); isItem {
				break
			}
			if len(l)-len(strings.TrimLeft(l, " ")) < base+2 {
				break
			}
			text = append(text, l[base+2:])
			p.i++
		}
		item := []any{p.paragraphNode(strings.Join(text, "\n"))}
		for p.i < len(p.lines) {
			sub, ok := parseListLine(p.lines[p.i])
			if !ok || sub.indent <= base {
				break
			}
			item = append(item, p.list())
		}
		items = append(items, map[string]any{"type": "listItem", "content": item})
	}
	if ordered {
		n := map[string]any{"type": "orderedList", "content": items}
		if first.num != 1 {
			n["attrs"] = map[string]any{"order": first.num}
		}
		return n
	}
	return map[string]any{"type": "bulletList", "content": items}
}

// --- inline ---

func (p *mdParser) inlineNodes(s string) []any {
	var segs []seg
	parseInline(p.ic, s, nil, &segs)
	var out []any
	for _, sg := range normalize(segs) {
		switch {
		case sg.brk:
			out = append(out, map[string]any{"type": "hardBreak"})
			continue
		case sg.mid != "":
			out = append(out, map[string]any{"type": "mention", "attrs": map[string]any{"id": sg.mid, "text": "@" + sg.mname}})
			continue
		case sg.card != "":
			out = append(out, map[string]any{"type": "inlineCard", "attrs": map[string]any{"url": sg.card}})
			continue
		}
		n := map[string]any{"type": "text", "text": sg.text}
		if len(sg.marks) > 0 {
			ms := make([]any, len(sg.marks))
			for i, m := range sg.marks {
				ms[i] = m.adf()
			}
			n["marks"] = ms
		}
		out = append(out, n)
	}
	return out
}

func spaceOrEnd(s string, i int) bool { return i < 0 || i >= len(s) || s[i] == ' ' || s[i] == '\n' }

func runeBefore(s string, i int) rune {
	r, _ := utf8.DecodeLastRuneInString(s[:i])
	if r == utf8.RuneError {
		return 0
	}
	return r
}

func runeAt(s string, i int) rune {
	if i >= len(s) {
		return 0
	}
	r, _ := utf8.DecodeRuneInString(s[i:])
	if r == utf8.RuneError {
		return 0
	}
	return r
}

// findClose returns the index of the next delim at or after from that ok accepts,
// skipping escapes and code spans; -1 when there is none.
func findClose(s string, from int, delim string, ok func(j int) bool) int {
	for i := from; i < len(s); {
		switch {
		case s[i] == '\\' && i+1 < len(s):
			i += 2
		case s[i] == '`':
			if j := strings.IndexByte(s[i+1:], '`'); j >= 0 {
				i += j + 2
			} else {
				i++
			}
		case strings.HasPrefix(s[i:], delim):
			if delim == "*" && strings.HasPrefix(s[i:], "**") { // a bold run inside an italic one
				i += 2
				continue
			}
			if ok(i) {
				return i
			}
			i += len(delim)
		default:
			i++
		}
	}
	return -1
}

func unescape(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) && isASCIIPunct(rune(s[i+1])) {
			i++
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func parseInline(ic *inlineCtx, s string, marks []mark, out *[]seg) {
	var buf strings.Builder
	flush := func() {
		if buf.Len() > 0 {
			*out = append(*out, seg{text: buf.String(), marks: slices.Clone(marks)})
			buf.Reset()
		}
	}
	with := func(m mark) []mark { return append(slices.Clone(marks), m) }
	always := func(int) bool { return true }
	emphasis := func(i int, delim string, m mark, closeOK func(int) bool) (int, bool) {
		j := findClose(s, i+len(delim), delim, closeOK)
		if j < 0 || j == i+len(delim) {
			return 0, false
		}
		flush()
		parseInline(ic, s[i+len(delim):j], with(m), out)
		return j + len(delim), true
	}

scan:
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == '\\' && i+1 < len(s) && isASCIIPunct(rune(s[i+1])):
			buf.WriteByte(s[i+1])
			i += 2
			continue
		case c == '\n':
			flush()
			*out = append(*out, seg{brk: true})
			i++
			continue
		case c == '`':
			if j := strings.IndexByte(s[i+1:], '`'); j > 0 {
				flush()
				*out = append(*out, seg{text: s[i+1 : i+1+j], marks: with(mark{typ: "code"})})
				i += j + 2
				continue
			}
		case strings.HasPrefix(s[i:], "**") && !spaceOrEnd(s, i+2):
			if next, ok := emphasis(i, "**", mark{typ: "strong"}, func(j int) bool { return !spaceOrEnd(s, j-1) }); ok {
				i = next
				continue
			}
		case strings.HasPrefix(s[i:], "~~") && !spaceOrEnd(s, i+2):
			if next, ok := emphasis(i, "~~", mark{typ: "strike"}, func(j int) bool { return !spaceOrEnd(s, j-1) }); ok {
				i = next
				continue
			}
		case c == '*' && !strings.HasPrefix(s[i:], "**") && !spaceOrEnd(s, i+1):
			if next, ok := emphasis(i, "*", mark{typ: "em"}, func(j int) bool { return !spaceOrEnd(s, j-1) }); ok {
				i = next
				continue
			}
		case c == '_' && !isWord(runeBefore(s, i)) && runeBefore(s, i) != '_' && !spaceOrEnd(s, i+1):
			if next, ok := emphasis(i, "_", mark{typ: "em"}, func(j int) bool { return !spaceOrEnd(s, j-1) && !isWord(runeAt(s, j+1)) && runeAt(s, j+1) != '_' }); ok {
				i = next
				continue
			}
		case c == '@' && !isWord(runeBefore(s, i)):
			for _, name := range ic.names {
				if strings.HasPrefix(s[i+1:], name) && !isWord(runeAt(s, i+1+len(name))) {
					flush()
					*out = append(*out, seg{mid: ic.mentions[name], mname: name})
					i += 1 + len(name)
					continue scan
				}
			}
		case c == '#' && ic.browse != nil && !isWord(runeBefore(s, i)):
			if key := ticketKeyAt(s[i+1:]); key != "" {
				flush()
				*out = append(*out, seg{card: ic.browse(key)})
				i += 1 + len(key)
				continue
			}
		case c == '<':
			if n := autolinkEnd(s[i+1:]); n > 0 {
				flush()
				*out = append(*out, seg{card: s[i+1 : i+1+n]})
				i += n + 2
				continue
			}
		case c == '[':
			if j := findClose(s, i+1, "]", always); j > i+1 && j+1 < len(s) && s[j+1] == '(' {
				k := j + 2
				for k < len(s) && s[k] != ')' {
					if s[k] == '\\' && k+1 < len(s) {
						k++
					}
					k++
				}
				if k < len(s) {
					url := unescape(s[j+2 : k])
					if url != "" && !strings.ContainsAny(url, " \t\n") {
						flush()
						parseInline(ic, s[i+1:j], with(mark{typ: "link", href: url}), out)
						i = k + 1
						continue
					}
				}
			}
		}
		buf.WriteByte(c)
		i++
	}
	flush()
}
