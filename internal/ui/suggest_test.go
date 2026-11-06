package ui

import (
	"encoding/json"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/kevin/jira-tui/internal/config"
	"github.com/kevin/jira-tui/internal/jira"
	"github.com/kevin/jira-tui/internal/mock"
)

// pause ends the typing pause: the delayed search of the picker runs.
func pause(t *testing.T, m Model) Model {
	t.Helper()
	next, cmd := m.Update(suggestQueryMsg{m.sg.seq})
	return settle(t, next.(Model), cmd)
}

// nodesIn returns the inline nodes of a document, flattened.
func nodesIn(t *testing.T, doc any) []map[string]any {
	t.Helper()
	raw, _ := json.Marshal(doc)
	var d map[string]any
	_ = json.Unmarshal(raw, &d)
	var out []map[string]any
	for _, b := range d["content"].([]any) {
		bb, _ := b.(map[string]any)
		inl, _ := bb["content"].([]any)
		for _, n := range inl {
			out = append(out, n.(map[string]any))
		}
	}
	return out
}

func lastComment(m Model) jira.Comment {
	cs := m.detail.Fields.Comment.Comments
	return cs[len(cs)-1]
}

func TestMentionPickerInNewComment(t *testing.T) {
	m, _ := editModel(t, "PAY-142")
	m = press(t, m, "c")
	m = typeText(t, m, "Salut ")
	if m = press(t, m, "@"); !m.sg.active || m.sg.kind != '@' {
		t.Fatalf("@ after a space must open the picker (active=%v)", m.sg.active)
	}
	out := view(m)
	for _, want := range []string{"Utilisateurs", "Kevin", "Camille R.", "Sofia M.", "Léo B."} {
		if !strings.Contains(out, want) {
			t.Errorf("picker misses %q\n%s", want, out)
		}
	}
	if strings.Contains(out, "erreur 502") {
		t.Error("the picker should take the place of the description above the comment box")
	}
	if h := hintsOf(m); !strings.Contains(h, "insérer") {
		t.Errorf("hints = %s", h)
	}
	// typing filters after the pause
	m = typeText(t, m, "so")
	if !m.sg.busy || m.sg.query != "so" {
		t.Fatalf("busy=%v query=%q", m.sg.busy, m.sg.query)
	}
	m = pause(t, m)
	if len(m.sg.users) != 1 || m.sg.users[0].DisplayName != "Sofia M." {
		t.Errorf("filtered users = %+v", m.sg.users)
	}
	// enter replaces "@so" by the name and registers the mention
	m = press(t, m, "enter")
	if m.sg.active || m.comment.Value() != "Salut @Sofia M. " {
		t.Fatalf("active=%v value=%q", m.sg.active, m.comment.Value())
	}
	if m.cmtMentions["Sofia M."] != "Sofia M." { // the demo uses the name as account id
		t.Errorf("mentions table = %v", m.cmtMentions)
	}
	m = typeText(t, m, "merci")
	m = press(t, m, "ctrl+s")
	if m.mode != modeNormal || len(m.cmtMentions) != 0 {
		t.Fatalf("mode=%v table=%v", m.mode, m.cmtMentions)
	}
	var kinds []string
	for _, n := range nodesIn(t, lastComment(m).Body) {
		kinds = append(kinds, n["type"].(string))
		if n["type"] == "mention" {
			a := n["attrs"].(map[string]any)
			if a["id"] != "Sofia M." || a["text"] != "@Sofia M." {
				t.Errorf("mention attrs = %v", a)
			}
		}
	}
	if strings.Join(kinds, ",") != "text,mention,text" {
		t.Errorf("the comment was sent with nodes %v", kinds)
	}
	if got := jira.ADFToText(lastComment(m).Body); got != "Salut @Sofia M. merci" {
		t.Errorf("comment text = %q", got)
	}
}

func TestTicketPickerInNewComment(t *testing.T) {
	m, _ := editModel(t, "PAY-142")
	m = press(t, m, "c")
	m = press(t, m, "#")
	if !m.sg.active || m.sg.kind != '#' {
		t.Fatal("# at the start must open the ticket picker")
	}
	out := view(m)
	for _, want := range []string{"Tickets", "PAY-142", "PAY-141", "Payment fails", "Le paiement échoue"} {
		if strings.Contains(want, "Payment") {
			continue
		}
		if !strings.Contains(out, want) {
			t.Errorf("picker misses %q\n%s", want, out)
		}
	}
	m = typeText(t, m, "137")
	m = pause(t, m)
	if out := view(m); !strings.Contains(out, "PAY-137") || strings.Contains(out, "PAY-141") {
		t.Errorf("filtered list:\n%s", out)
	}
	m = press(t, m, "enter")
	if m.comment.Value() != "#PAY-137 " {
		t.Fatalf("value = %q", m.comment.Value())
	}
	m = typeText(t, m, "est lié")
	m = press(t, m, "ctrl+s")
	var card map[string]any
	for _, n := range nodesIn(t, lastComment(m).Body) {
		if n["type"] == "inlineCard" {
			card = n
		}
	}
	if card == nil || card["attrs"].(map[string]any)["url"] != "https://example.atlassian.net/browse/PAY-137" {
		t.Errorf("no smart link in %v", nodesIn(t, lastComment(m).Body))
	}
	if got := jira.ADFToText(lastComment(m).Body); got != "PAY-137 est lié" {
		t.Errorf("comment text = %q", got)
	}
}

func TestPickerTriggerRules(t *testing.T) {
	m, _ := editModel(t, "PAY-142")
	m = press(t, m, "c")
	// glued to a word: an e-mail address, "C#": no picker
	for _, s := range []string{"mail@", "C#", "x1@", "a\\@"} {
		m.comment.Reset()
		m = typeText(t, m, s)
		if m.sg.active {
			t.Errorf("%q opened the picker", s)
		}
		m.closeSuggest()
	}
	// after a space, a new line, an opening parenthesis: picker
	for _, s := range []string{"a @", "a\n@", "(@", "a #"} {
		m.comment.Reset()
		m = typeText(t, m, strings.ReplaceAll(s, "\n", ""))
		if strings.Contains(s, "\n") {
			m = press(t, m, "enter") // a real new line
			m = press(t, m, s[len(s)-1:])
		}
		if !m.sg.active {
			t.Errorf("%q should open the picker", s)
		}
		m.closeSuggest()
	}
	// Markdown headings: "# Title" and "## Title" never keep the picker open
	for _, s := range []string{"# ", "## ", "#- "} {
		m.comment.Reset()
		m = typeText(t, m, s)
		if m.sg.active {
			t.Errorf("%q: the picker must close for a heading", s)
		}
		if m.comment.Value() != s {
			t.Errorf("%q: the typed text was altered: %q", s, m.comment.Value())
		}
		m.closeSuggest()
	}
	// "@ " is only an at sign
	m.comment.Reset()
	m = typeText(t, m, "@ ")
	if m.sg.active || m.comment.Value() != "@ " {
		t.Errorf("active=%v value=%q", m.sg.active, m.comment.Value())
	}
}

func TestPickerKeys(t *testing.T) {
	m, _ := editModel(t, "PAY-142")
	m = press(t, m, "c")
	m = typeText(t, m, "a @")
	// down then enter picks the second person
	m = press(t, press(t, m, "down"), "enter")
	if m.comment.Value() != "a @Camille R. " {
		t.Errorf("value = %q", m.comment.Value())
	}
	// esc closes the picker only; a second esc leaves the editor but keeps the draft
	m = press(t, m, "@")
	if !m.sg.active {
		t.Fatal("picker should be open")
	}
	if m = press(t, m, "esc"); m.sg.active || m.mode != modeComment || m.comment.Value() != "a @Camille R. @" {
		t.Fatalf("first esc: active=%v mode=%v value=%q", m.sg.active, m.mode, m.comment.Value())
	}
	if m = press(t, m, "esc"); m.mode != modeNormal {
		t.Errorf("second esc: mode=%v", m.mode)
	}
	// backspace on the trigger closes the picker and deletes the character
	m = press(t, m, "c")
	m.comment.Reset()
	m = press(t, m, "@")
	if m = press(t, m, "backspace"); m.sg.active || m.comment.Value() != "" {
		t.Errorf("backspace: active=%v value=%q", m.sg.active, m.comment.Value())
	}
	// backspace inside the query keeps the picker and searches again
	m = press(t, m, "@")
	m = typeText(t, m, "sx")
	m = pause(t, m)
	if out := view(m); !strings.Contains(out, "aucun résultat") {
		t.Errorf("no match expected:\n%s", out)
	}
	m = press(t, m, "backspace")
	m = pause(t, m)
	if !m.sg.active || m.sg.query != "s" || !strings.Contains(view(m), "Sofia M.") {
		t.Errorf("after backspace: active=%v query=%q", m.sg.active, m.sg.query)
	}
	// enter with nothing to pick is a plain new line
	m = typeText(t, m, "zz")
	m = pause(t, m)
	if m = press(t, m, "enter"); m.sg.active || !strings.HasSuffix(m.comment.Value(), "\n") {
		t.Errorf("enter with no result: active=%v value=%q", m.sg.active, m.comment.Value())
	}
	// an arrow moves the cursor: the picker closes
	m.comment.Reset()
	m = press(t, m, "@")
	m = press(t, m, "left")
	if m.sg.active {
		t.Error("left should close the picker")
	}
	// tab toggles the visibility when there is nothing to pick
	m.comment.Reset()
	m = press(t, m, "@")
	m = typeText(t, m, "zzz")
	m = pause(t, m)
	was := m.commentInternal
	if m = press(t, m, "tab"); m.commentInternal == was {
		t.Error("tab with no result must still toggle customer/internal")
	}
}

func TestPickerIgnoresStaleAnswers(t *testing.T) {
	m, _ := editModel(t, "PAY-142")
	m = press(t, m, "c")
	m = press(t, m, "@")
	old := m.sg.seq
	m = typeText(t, m, "s")
	if m.sg.seq == old {
		t.Fatal("each change must start a new search")
	}
	next, _ := m.Update(suggestMsg{seq: old, users: []jira.User{{AccountID: "x", DisplayName: "Fantôme"}}})
	m = next.(Model)
	if strings.Contains(view(m), "Fantôme") || !m.sg.busy {
		t.Error("a stale answer was used")
	}
	if cmd := m.onSuggestQuery(suggestQueryMsg{old}); cmd != nil {
		t.Error("a stale tick started a search")
	}
	// an answer arriving after the picker was closed is dropped too
	cur := m.sg.seq
	m = press(t, m, "esc")
	next, _ = m.Update(suggestMsg{seq: cur, users: []jira.User{{AccountID: "y", DisplayName: "Revenant"}}})
	m = next.(Model)
	if m.sg.active || len(m.sg.users) != 0 {
		t.Error("an answer reopened a closed picker")
	}
	// errors are shown
	m.comment.Reset()
	m = press(t, m, "@")
	next, _ = m.Update(suggestMsg{seq: m.sg.seq, err: jiraErr("accès interdit (403)")})
	m = next.(Model)
	if !strings.Contains(view(m), "accès interdit (403)") {
		t.Errorf("search error not displayed:\n%s", view(m))
	}
}

func TestPickersInTheEditors(t *testing.T) {
	m, sp := editModel(t, "PAY-142")
	m = openEntry(t, m, itDesc)
	m.ed.area.SetValue("Voir avec ")
	m = press(t, m, "@")
	m = typeText(t, m, "ca")
	m = pause(t, m)
	out := view(m)
	if !strings.Contains(out, "Utilisateurs") || !strings.Contains(out, "Camille R.") || strings.Contains(out, "Sofia M.") {
		t.Errorf("picker in the description editor:\n%s", out)
	}
	m = press(t, m, "enter")
	m = typeText(t, m, "pour #")
	m = typeText(t, m, "PAY-13")
	m = pause(t, m)
	if out := view(m); !strings.Contains(out, "PAY-139") || !strings.Contains(out, "PAY-137") || strings.Contains(out, "PAY-141") {
		t.Errorf("tickets:\n%s", out)
	}
	m = press(t, press(t, m, "down"), "enter") // PAY-137
	if v := m.ed.area.Value(); !strings.HasPrefix(v, "Voir avec @Camille R. pour #PAY-137 ") {
		t.Fatalf("editor value = %q", v)
	}
	// esc closes the list first; the editor stays
	m = press(t, m, "#")
	if m = press(t, m, "esc"); m.mode != modeEditText || m.sg.active {
		t.Fatalf("mode=%v active=%v", m.mode, m.sg.active)
	}
	m.ed.area.SetValue("Voir avec @Camille R. pour #PAY-137 ")
	m = press(t, m, "ctrl+s")
	if len(sp.calls) != 1 {
		t.Fatalf("calls = %v", sp.calls)
	}
	var types []string
	for _, n := range nodesIn(t, m.detail.Fields.Description) {
		types = append(types, n["type"].(string))
	}
	if strings.Join(types, ",") != "text,mention,text,inlineCard" { // the trailing space is trimmed
		t.Errorf("description nodes = %v", types)
	}
	// and editing it again shows the same readable text, with the table rebuilt from the document
	m = openEntry(t, m, itDesc)
	if v := m.ed.area.Value(); v != "Voir avec @Camille R. pour #PAY-137" {
		t.Errorf("second edit shows %q", v)
	}
	if m.ed.mentions["Camille R."] != "Camille R." {
		t.Errorf("table = %v", m.ed.mentions)
	}
	if m = press(t, m, "ctrl+s"); len(sp.calls) != 1 || m.mode != modeNormal {
		t.Errorf("saving an unchanged text must not call Jira (calls=%v mode=%v)", sp.calls, m.mode)
	}

	// the same pickers work when editing a comment
	m = openEntry(t, m, itEditComment)
	m = press(t, m, "enter")
	m.ed.area.SetValue("Merci ")
	m = press(t, m, "@")
	m = typeText(t, m, "l")
	m = pause(t, m)
	m = press(t, m, "enter")
	m = press(t, m, "ctrl+s")
	var cm []string
	for _, n := range nodesIn(t, lastComment(m).Body) {
		cm = append(cm, n["type"].(string))
	}
	if strings.Join(cm, ",") != "text,mention" { // the trailing space is trimmed
		t.Errorf("comment nodes after the edit = %v", cm)
	}
}

func TestExistingMentionsAreEditable(t *testing.T) {
	m, sp := editModel(t, "PAY-142")
	m.detail.Fields.Description = map[string]any{"type": "doc", "version": 1, "content": []any{map[string]any{"type": "paragraph", "content": []any{
		map[string]any{"type": "text", "text": "Demander à "},
		map[string]any{"type": "mention", "attrs": map[string]any{"id": "Sofia M.", "text": "@Sofia M.", "accessLevel": ""}},
		map[string]any{"type": "text", "text": " et voir "},
		map[string]any{"type": "inlineCard", "attrs": map[string]any{"url": "https://example.atlassian.net/browse/PAY-139"}},
	}}}}
	m = openEntry(t, m, itDesc)
	if m.mode != modeEditText || m.ed.area.Value() != "Demander à @Sofia M. et voir #PAY-139" {
		t.Fatalf("mode=%v value=%q (status %q)", m.mode, m.ed.area.Value(), m.status)
	}
	m.ed.area.SetValue("Demander à @Sofia M. et voir #PAY-139 demain")
	m = press(t, m, "ctrl+s")
	if len(sp.calls) != 1 {
		t.Fatalf("calls = %v", sp.calls)
	}
	var types []string
	for _, n := range nodesIn(t, m.detail.Fields.Description) {
		types = append(types, n["type"].(string))
	}
	if strings.Join(types, ",") != "text,mention,text,inlineCard,text" {
		t.Errorf("the mention and the smart link must survive the edit: %v", types)
	}
}

func TestTwoPeopleWithTheSameNameCannotBeMentioned(t *testing.T) {
	m, _ := editModel(t, "PAY-142")
	m = press(t, m, "c")
	m.cmtMentions["Sofia M."] = "another-account"
	m = press(t, m, "@")
	m = typeText(t, m, "so")
	m = pause(t, m)
	m = press(t, m, "enter")
	if !m.sg.active || m.comment.Value() != "@so" || !strings.Contains(view(m), "deux personnes s'appellent « Sofia M. »") {
		t.Errorf("active=%v value=%q\n%s", m.sg.active, m.comment.Value(), view(m))
	}
}

func TestPickerEnglishAndSmallScreens(t *testing.T) {
	english(t)
	sp := &spy{Service: mock.New()}
	open := func(w, h int) Model {
		m := New(sp, config.Config{JQL: config.DefaultJQL}).OpenIssue("PAY-142")
		next, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
		return settle(t, next.(Model), next.(Model).Init())
	}
	m := press(t, open(130, 36), "c")
	m = press(t, m, "@")
	out := view(m)
	for _, want := range []string{"Users", "Sofia M."} {
		if !strings.Contains(out, want) {
			t.Errorf("EN picker misses %q\n%s", want, out)
		}
	}
	noFrench(t, "mention picker", out)
	if h := hintsOf(m); !strings.Contains(h, "insert") || !strings.Contains(h, "close the list") {
		t.Errorf("hints = %s", h)
	}
	noFrench(t, "picker hints", hintsOf(m))
	m = typeText(t, m, "zzz")
	m = pause(t, m)
	if out := view(m); !strings.Contains(out, "no result") {
		t.Errorf("EN empty state:\n%s", out)
	}
	m.comment.Reset()
	m = press(t, m, "#")
	noFrench(t, "ticket picker", view(m))

	// everything fits the terminal, in the comment box and in the editors
	for _, sz := range [][2]int{{80, 24}, {60, 20}} {
		for _, key := range []byte{'@', '#'} {
			c := press(t, open(sz[0], sz[1]), "c")
			c = press(t, c, string(key))
			for i, l := range strings.Split(view(c), "\n") {
				if w := lipgloss.Width(l); w > sz[0] {
					t.Fatalf("%dx%d comment picker %c: line %d is %d wide", sz[0], sz[1], key, i, w)
				}
			}
			e := openEntry(t, open(sz[0], sz[1]), itDesc)
			e.ed.area.SetValue("voir ")
			e = press(t, e, string(key))
			if !e.sg.active {
				t.Fatalf("%dx%d: no picker in the editor", sz[0], sz[1])
			}
			for i, l := range strings.Split(view(e), "\n") {
				if w := lipgloss.Width(l); w > sz[0] {
					t.Fatalf("%dx%d editor picker %c: line %d is %d wide\n%s", sz[0], sz[1], key, i, w, view(e))
				}
			}
		}
	}
}

func TestMentionPickerListsServiceDeskCustomers(t *testing.T) {
	m, _ := editModel(t, "PAY-142")
	m = press(t, m, "c")
	m = press(t, m, "@")
	out := view(m)
	for _, want := range []string{"Sofia M.", "Julie Martin  · client", "Paul Durand  · client"} {
		if !strings.Contains(out, want) {
			t.Errorf("picker misses %q\n%s", want, out)
		}
	}
	// a customer is found by typing a part of the name, and can be mentioned
	m = typeText(t, m, "jul")
	m = pause(t, m)
	if len(m.sg.users) != 1 || m.sg.users[0].AccountType != "customer" {
		t.Fatalf("users = %+v", m.sg.users)
	}
	m = press(t, m, "enter")
	if m.comment.Value() != "@Julie Martin " || m.cmtMentions["Julie Martin"] != "Julie Martin" {
		t.Fatalf("value=%q table=%v", m.comment.Value(), m.cmtMentions)
	}
	m = press(t, m, "ctrl+s")
	found := false
	for _, n := range nodesIn(t, lastComment(m).Body) {
		if n["type"] == "mention" && n["attrs"].(map[string]any)["id"] == "Julie Martin" {
			found = true
		}
	}
	if !found {
		t.Errorf("no mention of the customer in %v", nodesIn(t, lastComment(m).Body))
	}

	// customers cannot be assigned (they are not Jira users)
	a := openEntry(t, editModelOnly(t), itAssignee)
	if out := view(a); strings.Contains(out, "Julie Martin") {
		t.Errorf("a customer is offered as assignee:\n%s", out)
	}

	english(t)
	e, _ := editModel(t, "PAY-142")
	e = press(t, press(t, e, "c"), "@")
	if out := view(e); !strings.Contains(out, "Julie Martin  · customer") {
		t.Errorf("EN label:\n%s", out)
	}
	noFrench(t, "customer picker", view(e))
}

func editModelOnly(t *testing.T) Model { m, _ := editModel(t, "PAY-142"); return m }
