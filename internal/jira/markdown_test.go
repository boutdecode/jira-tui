package jira

import (
	"encoding/json"
	"math/rand"
	"strings"
	"testing"
)

func decode(t *testing.T, s string) any {
	t.Helper()
	var d any
	if err := json.Unmarshal([]byte(s), &d); err != nil {
		t.Fatal(err)
	}
	return d
}

// asJSON gives the document the shape it has when Jira sends it (maps and []any only).
func asJSON(t *testing.T, doc any) any {
	t.Helper()
	b, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	var d any
	_ = json.Unmarshal(b, &d)
	return d
}

// Markdown that is already in the canonical form must survive md -> ADF -> md unchanged.
func TestMarkdownIsStable(t *testing.T) {
	for _, md := range []string{
		"Un paragraphe simple.",
		"Deux paragraphes\n\nséparés par une ligne vide.",
		"Une ligne\nun saut de ligne dans le même paragraphe",
		"# Titre 1\n\n## Titre 2\n\n###### Titre 6",
		"Du **gras**, de l'*italique*, du ~~barré~~ et du `code`.",
		"**gras et _italique_** puis *italique seul*",
		"Un [lien](https://example.com/a?b=1&c=2) dans une phrase.",
		"Un [lien **en gras**](https://example.com) et du `code`.",
		"- un\n- deux\n- trois",
		"1. premier\n2. deuxième\n3. troisième",
		"- parent\n  - enfant\n  - autre enfant\n    - petit-enfant\n- suite",
		"1. a\n  - sous-liste\n2. b",
		"- item sur\n  deux lignes\n- suivant",
		"Avant\n\n- liste\n- liste\n\nAprès",
		"```go\nfunc main() {\n\tfmt.Println(\"hi\")\n}\n```",
		"```\nsans langage\n```",
		"> une citation\n> sur deux lignes\n>\n> et un second paragraphe",
		"Avant\n\n---\n\nAprès",
		"Texte avec 2 * 3 = 6 et snake_case_name et [WIP] et a_b",
		`Des \*étoiles\* échappées et un \[crochet\](pas un lien)`,
		"5. commence à cinq\n6. suite",
		"Accents é, emoji 🎉, guillemets « x », tiret — long",
	} {
		doc := MarkdownToADF(md)
		got, ok := ADFToMarkdown(asJSON(t, doc))
		if !ok {
			t.Errorf("not editable but written by the parser:\n%s\n%s", md, debugJSON(doc))
			continue
		}
		if got != md {
			t.Errorf("not stable:\n--- in\n%s\n--- out\n%s", md, got)
		}
	}
}

// collapse ignores the number of blank lines: empty paragraphs are spacing, not content.
func collapse(s string) string {
	for strings.Contains(s, "\n\n\n") {
		s = strings.ReplaceAll(s, "\n\n\n", "\n\n")
	}
	return strings.TrimSpace(s)
}

func debugJSON(v any) string { b, _ := json.MarshalIndent(v, "", " "); return string(b) }

func TestMarkdownBlocksAndMarks(t *testing.T) {
	doc := asJSON(t, MarkdownToADF("# T\n\nA **b** *c* ~~d~~ `e` [f](http://g)\n\n- x\n  - y\n\n```py\nz\n```"))
	d := doc.(map[string]any)
	blocks := d["content"].([]any)
	var types []string
	for _, b := range blocks {
		types = append(types, b.(map[string]any)["type"].(string))
	}
	if strings.Join(types, ",") != "heading,paragraph,bulletList,codeBlock" {
		t.Fatalf("blocks = %v", types)
	}
	if lvl := blocks[0].(map[string]any)["attrs"].(map[string]any)["level"]; lvl != float64(1) {
		t.Errorf("level = %v", lvl)
	}
	inl := blocks[1].(map[string]any)["content"].([]any)
	got := map[string]string{}
	for _, n := range inl {
		nn := n.(map[string]any)
		ms, _ := nn["marks"].([]any)
		for _, m := range ms {
			mm := m.(map[string]any)
			got[mm["type"].(string)] = nn["text"].(string)
			if mm["type"] == "link" && mm["attrs"].(map[string]any)["href"] != "http://g" {
				t.Errorf("link attrs %v", mm["attrs"])
			}
		}
	}
	want := map[string]string{"strong": "b", "em": "c", "strike": "d", "code": "e", "link": "f"}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("mark %s on %q, want %q (all: %v)", k, got[k], v, got)
		}
	}
	code := blocks[3].(map[string]any)
	if code["attrs"].(map[string]any)["language"] != "py" || code["content"].([]any)[0].(map[string]any)["text"] != "z" {
		t.Errorf("code block %v", code)
	}
	// plain text still produces what TextToADF produced (comments written before keep working)
	for _, in := range []string{"un", "deux\nlignes", "p1\n\np2", "p1\nsuite\n\np2\n\np3 avec « accents » 🎉"} {
		if a, b := ADFToText(MarkdownToADF(in)), ADFToText(TextToADF(in)); a != b {
			t.Errorf("%q: markdown %q, plain %q", in, a, b)
		}
	}
}

func TestMarkdownLeavesUnclosedMarkupLiteral(t *testing.T) {
	for _, in := range []string{"2 * 3 * 4", "a **b", "un *seul", "snake_case_word", "[pas un lien]", "[x](", "`ouvert", "~~tilde", "_a b", "a__b__c"} {
		doc := asJSON(t, MarkdownToADF(in))
		if got := ADFToText(doc); got != in {
			t.Errorf("%q became %q", in, got)
		}
		for _, n := range doc.(map[string]any)["content"].([]any)[0].(map[string]any)["content"].([]any) {
			if _, marked := n.(map[string]any)["marks"]; marked {
				t.Errorf("%q: unexpected formatting %v", in, n)
			}
		}
	}
}

// Documents as Jira sends them.
func TestADFToMarkdownFromJira(t *testing.T) {
	cases := []struct{ name, doc, want string }{
		{"paragraphs and breaks",
			`{"type":"doc","version":1,"content":[{"type":"paragraph","content":[{"type":"text","text":"a"},{"type":"hardBreak"},{"type":"text","text":"b"}]},{"type":"paragraph","content":[{"type":"text","text":"c"}]}]}`,
			"a\nb\n\nc"},
		{"trailing and spacing empty paragraphs (the Jira editor adds them)",
			`{"type":"doc","version":1,"content":[{"type":"paragraph","content":[{"type":"text","text":"a"}]},{"type":"paragraph"},{"type":"paragraph","content":[{"type":"text","text":"b"}]},{"type":"paragraph"}]}`,
			"a\n\nb"},
		{"bold with a trailing space, then plain text",
			`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"gras ","marks":[{"type":"strong"}]},{"type":"text","text":"suite"}]}]}`,
			"**gras** suite"},
		{"link",
			`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"voir "},{"type":"text","text":"la page","marks":[{"type":"link","attrs":{"href":"https://x.io/a_b"}}]}]}]}`,
			"voir [la page](https://x.io/a_b)"},
		{"bullet list with localId attributes (ignored)",
			`{"type":"doc","content":[{"type":"bulletList","content":[{"type":"listItem","attrs":{"localId":"1"},"content":[{"type":"paragraph","content":[{"type":"text","text":"a"}]}]},{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"b"}]},{"type":"bulletList","content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"c"}]}]}]}]}]}]}`,
			"- a\n- b\n  - c"},
		{"ordered list with order 1",
			`{"type":"doc","content":[{"type":"orderedList","attrs":{"order":1},"content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"a"}]}]},{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"b"}]}]}]}]}`,
			"1. a\n2. b"},
		{"heading and code block",
			`{"type":"doc","content":[{"type":"heading","attrs":{"level":3},"content":[{"type":"text","text":"Titre"}]},{"type":"codeBlock","attrs":{"language":"json"},"content":[{"type":"text","text":"{\"a\": 1}"}]}]}`,
			"### Titre\n\n```json\n{\"a\": 1}\n```"},
		{"bold and italic together, code, strike",
			`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"x","marks":[{"type":"strong"},{"type":"em"}]},{"type":"text","text":" "},{"type":"text","text":"c","marks":[{"type":"code"}]},{"type":"text","text":" "},{"type":"text","text":"s","marks":[{"type":"strike"}]}]}]}`,
			"**_x_** `c` ~~s~~"},
		{"markup characters in plain text are escaped",
			`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"2*3 et *étoile* et a_b et _x_ et [lien](u) et 1. non"}]}]}`,
			`2\*3 et \*étoile\* et a_b et \_x\_ et \[lien\](u) et 1. non`},
		{"a paragraph that looks like a list or a heading",
			`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"- pas une liste"},{"type":"hardBreak"},{"type":"text","text":"# pas un titre"},{"type":"hardBreak"},{"type":"text","text":"1. pas ordonné"},{"type":"hardBreak"},{"type":"text","text":"> pas une citation"}]}]}`,
			"\\- pas une liste\n\\# pas un titre\n1\\. pas ordonné\n\\> pas une citation"},
		{"empty document", `{"type":"doc","version":1,"content":[]}`, ""},
	}
	for _, c := range cases {
		got, ok := ADFToMarkdown(decode(t, c.doc))
		if !ok {
			t.Errorf("%s: should be editable", c.name)
			continue
		}
		if got != c.want {
			t.Errorf("%s:\n got %q\nwant %q", c.name, got, c.want)
		}
		// and editing without touching the text keeps the document
		if back := MarkdownToADF(got); canon(back) != canon(decode(t, c.doc)) {
			t.Errorf("%s: round trip changed the document\n%s\n%s", c.name, canon(decode(t, c.doc)), canon(back))
		}
	}
	if md, ok := ADFToMarkdown(nil); !ok || md != "" {
		t.Errorf("a missing document is editable and empty, got %q %v", md, ok)
	}
}

// Everything the dialect cannot express must be refused, never flattened.
func TestADFToMarkdownRefusesWhatItCannotKeep(t *testing.T) {
	p := func(inner string) string {
		return `{"type":"doc","content":[{"type":"paragraph","content":[` + inner + `]}]}`
	}
	cases := map[string]string{
		"mention":                          p(`{"type":"mention","attrs":{"id":"1","text":"@Kevin"}}`),
		"emoji":                            p(`{"type":"emoji","attrs":{"shortName":":smile:"}}`),
		"date":                             p(`{"type":"date","attrs":{"timestamp":"1"}}`),
		"status":                           p(`{"type":"status","attrs":{"text":"OK","color":"green"}}`),
		"underline":                        p(`{"type":"text","text":"x","marks":[{"type":"underline"}]}`),
		"text color":                       p(`{"type":"text","text":"x","marks":[{"type":"textColor","attrs":{"color":"#f00"}}]}`),
		"sub/sup":                          p(`{"type":"text","text":"x","marks":[{"type":"subsup","attrs":{"type":"sub"}}]}`),
		"link title":                       p(`{"type":"text","text":"x","marks":[{"type":"link","attrs":{"href":"https://x","title":"t"}}]}`),
		"link w/space":                     p(`{"type":"text","text":"x","marks":[{"type":"link","attrs":{"href":"https://x y"}}]}`),
		"code in text":                     p(`{"type":"text","text":"a` + "`" + `b","marks":[{"type":"code"}]}`),
		"media":                            `{"type":"doc","content":[{"type":"mediaSingle","content":[{"type":"media","attrs":{"id":"1","type":"file"}}]}]}`,
		"table":                            `{"type":"doc","content":[{"type":"table","content":[{"type":"tableRow","content":[{"type":"tableCell","content":[{"type":"paragraph","content":[{"type":"text","text":"a"}]}]}]}]}]}`,
		"panel":                            `{"type":"doc","content":[{"type":"panel","attrs":{"panelType":"info"},"content":[{"type":"paragraph","content":[{"type":"text","text":"a"}]}]}]}`,
		"task list":                        `{"type":"doc","content":[{"type":"taskList","content":[{"type":"taskItem","attrs":{"state":"TODO"},"content":[{"type":"text","text":"a"}]}]}]}`,
		"expand":                           `{"type":"doc","content":[{"type":"expand","attrs":{"title":"t"},"content":[{"type":"paragraph","content":[{"type":"text","text":"a"}]}]}]}`,
		"alignment":                        `{"type":"doc","content":[{"type":"paragraph","marks":[{"type":"alignment","attrs":{"align":"center"}}],"content":[{"type":"text","text":"a"}]}]}`,
		"two paragraphs in a list item":    `{"type":"doc","content":[{"type":"bulletList","content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"a"}]},{"type":"paragraph","content":[{"type":"text","text":"b"}]}]}]}]}`,
		"code block in a list item":        `{"type":"doc","content":[{"type":"bulletList","content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"a"}]},{"type":"codeBlock","content":[{"type":"text","text":"x"}]}]}]}]}`,
		"two adjacent lists (would merge)": `{"type":"doc","content":[{"type":"bulletList","content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"a"}]}]}]},{"type":"bulletList","content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"b"}]}]}]}]}`,
		"a plain string (API v2)":          `"hello"`,
		"not a doc":                        `{"type":"paragraph"}`,
	}
	for name, doc := range cases {
		if md, ok := ADFToMarkdown(decode(t, doc)); ok {
			t.Errorf("%s must NOT be editable as Markdown (editing it would destroy content), got %q", name, md)
		}
	}
}

// For any plain text, either the Markdown form is refused or editing it keeps the text exactly.
func TestPlainTextNeverChangesThroughMarkdown(t *testing.T) {
	alphabet := []string{"*", "**", "_", "[", "]", "(", ")", "`", "~", "~~", `\`, "#", "-", "+", ">", "1", ".", "2)", " ", " ", "\n", "\n\n", "a", "b", "é", "http://x", "---", "```", "<", "|"}
	rng := rand.New(rand.NewSource(42))
	editable, total := 0, 3000
	for n := 0; n < total; n++ {
		var sb strings.Builder
		for k := rng.Intn(14) + 1; k > 0; k-- {
			sb.WriteString(alphabet[rng.Intn(len(alphabet))])
		}
		in := sb.String()
		doc := asJSON(t, TextToADF(in))
		md, ok := ADFToMarkdown(doc)
		if !ok {
			continue
		}
		editable++
		back := asJSON(t, MarkdownToADF(md))
		if a, b := collapse(ADFToText(doc)), collapse(ADFToText(back)); a != b {
			t.Fatalf("text changed through Markdown\n in   %q\n md   %q\n text %q -> %q", in, md, a, b)
		}
		// no formatting may appear by accident
		if strings.Contains(debugJSON(back), `"marks"`) && !strings.Contains(debugJSON(doc), `"marks"`) {
			t.Fatalf("formatting appeared from plain text\n in %q\n md %q", in, md)
		}
		// and the Markdown is stable: parsing and writing it again changes nothing
		if md2, ok2 := ADFToMarkdown(back); !ok2 || md2 != md {
			t.Fatalf("not stable\n md  %q\n md2 %q (ok=%v)", md, md2, ok2)
		}
	}
	if editable < total*7/10 {
		t.Errorf("only %d of %d plain texts are editable: the escaping is too timid", editable, total)
	}
}

func TestMarkdownIsWhatClientSends(t *testing.T) {
	c, rec := serve(t, 204, "")
	desc := "# Titre\n\n- a\n- **b**\n\n[x](https://y)"
	if err := c.UpdateIssue("PAY-1", FieldUpdate{Description: docp(desc)}); err != nil {
		t.Fatal(err)
	}
	fields, _ := rec.body["fields"].(map[string]any)
	got, ok := ADFToMarkdown(fields["description"])
	if !ok || got != desc {
		t.Errorf("sent description = %q (ok=%v), want %q", got, ok, desc)
	}
	c2, rec2 := serve(t, 200, `{}`)
	if err := c2.EditComment("PAY-1", Comment{ID: "1"}, MarkdownToADF("du **gras** et `code`")); err != nil {
		t.Fatal(err)
	}
	if md, ok := ADFToMarkdown(rec2.body["body"]); !ok || md != "du **gras** et `code`" {
		t.Errorf("comment body = %q (ok=%v)", md, ok)
	}
	if err := c2.AddComment("PAY-1", MarkdownToADF("*nouveau*"), true); err != nil {
		t.Fatal(err)
	}
	if md, ok := ADFToMarkdown(rec2.body["body"]); !ok || md != "*nouveau*" {
		t.Errorf("new comment body = %q (ok=%v)", md, ok)
	}
}

// Whatever the user types, once the result is editable, writing it back and parsing it
// again must give the same document: no drift from one edit to the next.
func TestTypedMarkdownReachesAFixedPoint(t *testing.T) {
	alphabet := []string{"*", "**", "_", "[", "](", "x)", "`", "~~", `\`, "# ", "## ", "- ", "  - ", "1. ", "2. ", "> ", "---", "```", "```go", "\n", "\n\n", "  ", "a", "bc", "é", "http://u", " ", " ", "\t"}
	for seed := int64(1); seed <= 2; seed++ {
		rng := rand.New(rand.NewSource(seed))
		kept := 0
		for n := 0; n < 3000; n++ {
			var sb strings.Builder
			for k := rng.Intn(16) + 1; k > 0; k-- {
				sb.WriteString(alphabet[rng.Intn(len(alphabet))])
			}
			typed := sb.String()
			adf := asJSON(t, MarkdownToADF(typed))
			md1, ok := ADFToMarkdown(adf)
			if !ok {
				continue // refused: nothing can be lost
			}
			kept++
			again := asJSON(t, MarkdownToADF(md1))
			if canon(adf) != canon(again) {
				t.Fatalf("seed %d: the document changed when written back\n typed %q\n md1   %q\n%s\n%s", seed, typed, md1, canon(adf), canon(again))
			}
			if md2, ok2 := ADFToMarkdown(again); !ok2 || md2 != md1 {
				t.Fatalf("seed %d: not a fixed point\n typed %q\n md1 %q\n md2 %q (ok=%v)", seed, typed, md1, md2, ok2)
			}
		}
		if kept < 1500 {
			t.Errorf("seed %d: only %d of 3000 typed texts are editable", seed, kept)
		}
	}
}
