package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/kevin/jira-tui/internal/config"
	"github.com/kevin/jira-tui/internal/jira"
	"github.com/kevin/jira-tui/internal/mock"
)

// spy records the write calls on top of the in-memory Jira.
type spy struct {
	*mock.Service
	calls     []string
	created   []jira.NewIssue // what CreateIssue received
	createErr error           // makes CreateIssue fail
}

func (s *spy) CreateIssue(in jira.NewIssue) (jira.IssueRef, error) {
	s.calls = append(s.calls, "create")
	s.created = append(s.created, in)
	if s.createErr != nil {
		return jira.IssueRef{}, s.createErr
	}
	return s.Service.CreateIssue(in)
}

func (s *spy) UpdateIssue(key string, u jira.FieldUpdate) error {
	s.calls = append(s.calls, "update")
	return s.Service.UpdateIssue(key, u)
}
func (s *spy) AssignTo(key, id string) error {
	s.calls = append(s.calls, "assign")
	return s.Service.AssignTo(key, id)
}
func (s *spy) Unassign(key string) error {
	s.calls = append(s.calls, "unassign")
	return s.Service.Unassign(key)
}
func (s *spy) EditComment(key string, c jira.Comment, body jira.Doc) error {
	s.calls = append(s.calls, "editcomment")
	return s.Service.EditComment(key, c, body)
}
func (s *spy) DeleteComment(key, id string) error {
	s.calls = append(s.calls, "deletecomment")
	return s.Service.DeleteComment(key, id)
}

// editModel opens the detail of key on a fresh demo.
func editModel(t *testing.T, key string) (Model, *spy) {
	t.Helper()
	sp := &spy{Service: mock.New()}
	m := initModel(t, New(sp, config.Config{JQL: config.DefaultJQL}).OpenIssue(key))
	if m.view != viewDetail || m.detail.Key != key {
		t.Fatalf("detail of %s not open (view=%v key=%q)", key, m.view, m.detail.Key)
	}
	return m, sp
}

// downs presses "down" n times.
func downs(t *testing.T, m Model, n int) Model {
	for i := 0; i < n; i++ {
		m = press(t, m, "down")
	}
	return m
}

// menu indexes
const (
	itTitle = iota
	itDesc
	itPriority
	itLabels
	itAssignee
	itEditComment
	itDelComment
)

func openEntry(t *testing.T, m Model, idx int) Model {
	t.Helper()
	m = press(t, m, "e")
	if m.mode != modeEditMenu {
		t.Fatalf("e did not open the menu (mode=%v, status=%q)", m.mode, m.status)
	}
	return press(t, downs(t, m, idx), "enter")
}

func TestEditMenu(t *testing.T) {
	m, _ := editModel(t, "PAY-142")
	m = press(t, m, "e")
	out := view(m)
	for _, want := range []string{"Modifier PAY-142", "Titre", "Description", "Priorité", "Étiquettes", "Assigné", "Modifier un commentaire", "Supprimer un commentaire"} {
		if !strings.Contains(out, want) {
			t.Errorf("menu misses %q\n%s", want, out)
		}
	}
	if m = press(t, m, "esc"); m.mode != modeNormal {
		t.Errorf("esc should close the menu (mode=%v)", m.mode)
	}
	// "e" is a detail key: nothing happens on the list
	l := demo(t, 130, 36)
	if l = press(t, l, "e"); l.mode != modeNormal {
		t.Errorf("e on the list opened %v", l.mode)
	}
}

func TestEditTitle(t *testing.T) {
	m, sp := editModel(t, "PAY-142")
	m = openEntry(t, m, itTitle)
	if m.mode != modeEditLine || m.ed.line.Value() != "Le paiement échoue avec une carte 3DS2" {
		t.Fatalf("mode=%v value=%q", m.mode, m.ed.line.Value())
	}
	if !strings.Contains(view(m), "Titre de PAY-142") {
		t.Errorf("title modal:\n%s", view(m))
	}
	// empty: refused, nothing sent
	m.ed.line.SetValue("   ")
	m = press(t, m, "enter")
	if m.mode != modeEditLine || !strings.Contains(view(m), "le titre ne peut pas être vide") || len(sp.calls) != 0 {
		t.Fatalf("empty title: mode=%v calls=%v", m.mode, sp.calls)
	}
	// unchanged: closes without any call
	m.ed.line.SetValue("Le paiement échoue avec une carte 3DS2")
	m = press(t, m, "enter")
	if m.mode != modeNormal || len(sp.calls) != 0 {
		t.Fatalf("unchanged title: mode=%v calls=%v", m.mode, sp.calls)
	}
	// real change
	m = openEntry(t, m, itTitle)
	m.ed.line.SetValue("  Nouveau titre  ")
	m = press(t, m, "enter")
	if m.mode != modeNormal || len(sp.calls) != 1 {
		t.Fatalf("mode=%v calls=%v", m.mode, sp.calls)
	}
	if m.detail.Fields.Summary != "Nouveau titre" {
		t.Errorf("detail not reloaded: %q", m.detail.Fields.Summary)
	}
	if !strings.Contains(m.status, "titre de PAY-142 modifié") || m.isErr {
		t.Errorf("status = %q (err=%v)", m.status, m.isErr)
	}
	found := false
	for _, is := range m.issues {
		if is.Key == "PAY-142" && is.Fields.Summary == "Nouveau titre" {
			found = true
		}
	}
	if !found {
		t.Error("the list was not reloaded")
	}
}

func TestEditLabels(t *testing.T) {
	m, sp := editModel(t, "PAY-142")
	m = openEntry(t, m, itLabels)
	if m.mode != modeEditLine || m.ed.line.Value() != "paiement, 3ds, prod" {
		t.Fatalf("mode=%v value=%q", m.mode, m.ed.line.Value())
	}
	m.ed.line.SetValue("urgent, client vip;urgent  paiement")
	m = press(t, m, "enter")
	if len(sp.calls) != 1 {
		t.Fatalf("calls=%v", sp.calls)
	}
	got := strings.Join(m.detail.Fields.Labels, "|")
	if got != "urgent|client|vip|paiement" {
		t.Errorf("labels = %q (separators and duplicates)", got)
	}
	// clearing all labels is allowed
	m = openEntry(t, m, itLabels)
	m.ed.line.SetValue("")
	m = press(t, m, "enter")
	if len(m.detail.Fields.Labels) != 0 {
		t.Errorf("labels not cleared: %v", m.detail.Fields.Labels)
	}
	// unchanged: no call
	n := len(sp.calls)
	m = openEntry(t, m, itLabels)
	m = press(t, m, "enter")
	if len(sp.calls) != n {
		t.Errorf("unchanged labels still called Jira: %v", sp.calls)
	}
}

func TestEditDescription(t *testing.T) {
	m, sp := editModel(t, "PAY-142")
	m = openEntry(t, m, itDesc)
	if m.mode != modeEditText || !strings.Contains(m.ed.area.Value(), "erreur 502") {
		t.Fatalf("mode=%v value=%q", m.mode, m.ed.area.Value())
	}
	m.ed.area.SetValue("Première ligne\nseconde ligne\n\nUn autre paragraphe")
	m = press(t, m, "ctrl+s")
	if m.mode != modeNormal || len(sp.calls) != 1 {
		t.Fatalf("mode=%v calls=%v", m.mode, sp.calls)
	}
	if got := jira.ADFToText(m.detail.Fields.Description); got != "Première ligne\nseconde ligne\n\nUn autre paragraphe" {
		t.Errorf("description = %q", got)
	}
	if !strings.Contains(view(m), "Un autre paragraphe") {
		t.Error("the new description is not displayed")
	}
	// esc never calls Jira
	n := len(sp.calls)
	m = openEntry(t, m, itDesc)
	m.ed.area.SetValue("jamais envoyé")
	if m = press(t, m, "esc"); m.mode != modeNormal || len(sp.calls) != n {
		t.Errorf("esc: mode=%v calls=%v", m.mode, sp.calls)
	}
}

// A description with a link and a list is edited as Markdown: nothing is lost.
func TestRichDescriptionIsEditedAsMarkdown(t *testing.T) {
	m, sp := editModel(t, "PAY-135")
	if _, _, ok := m.markdownOf(m.detail.Fields.Description); !ok {
		t.Fatal("the demo description (link + list) must be editable as Markdown")
	}
	m = openEntry(t, m, itDesc)
	if m.mode != modeEditText {
		t.Fatalf("mode = %v (status %q)", m.mode, m.status)
	}
	want := "Maquette : [figma.com/payments](https://figma.com/payments)\n\n- Filtrer par date\n- Filtrer par moyen de paiement"
	if got := m.ed.area.Value(); got != want {
		t.Fatalf("the editor shows\n%q\nwant\n%q", got, want)
	}
	if out := view(m); !strings.Contains(out, "Markdown :") {
		t.Errorf("the Markdown legend is missing:\n%s", out)
	}
	// saving it untouched changes nothing and calls nothing
	if m2 := press(t, m, "ctrl+s"); len(sp.calls) != 0 || m2.mode != modeNormal {
		t.Fatalf("unchanged: mode=%v calls=%v", m2.mode, sp.calls)
	}
	// a real edit keeps the link and the list
	m.ed.area.SetValue("Maquette : [figma.com/payments](https://figma.com/payments)\n\n- Filtrer par date\n- **Filtrer** par moyen de paiement\n- Export CSV")
	m = press(t, m, "ctrl+s")
	if len(sp.calls) != 1 {
		t.Fatalf("calls = %v", sp.calls)
	}
	md, _, ok := m.markdownOf(m.detail.Fields.Description)
	if !ok || !strings.Contains(md, "[figma.com/payments](https://figma.com/payments)") || !strings.Contains(md, "- **Filtrer** par moyen de paiement") || !strings.Contains(md, "- Export CSV") {
		t.Errorf("description after the edit: %q (ok=%v)", md, ok)
	}
	if strings.Contains(m.detail.Fields.Description.(map[string]any)["content"].([]any)[1].(map[string]any)["type"].(string), "paragraph") {
		t.Error("the list must still be a list")
	}
}

func TestUnsupportedDescriptionIsNeverRewritten(t *testing.T) {
	m, sp := editModel(t, "PAY-137") // a mention and a status lozenge
	if _, _, ok := m.markdownOf(m.detail.Fields.Description); ok {
		t.Fatal("the demo ticket should not be editable as Markdown")
	}
	m = press(t, m, "e")
	m = downs(t, m, itDesc)
	out := view(m)
	if !strings.Contains(out, "contenu non modifiable ici") {
		t.Errorf("the reason should be shown:\n%s", out)
	}
	if m = press(t, m, "enter"); m.mode != modeEditMenu {
		t.Fatalf("enter on a disabled entry must not open an editor (mode=%v)", m.mode)
	}
	if len(sp.calls) != 0 {
		t.Errorf("calls = %v", sp.calls)
	}
	// the other fields of the same ticket remain editable
	m = press(t, press(t, m, "up"), "up") // back to "Titre"
	if m = press(t, m, "enter"); m.mode != modeEditLine {
		t.Errorf("the title of a rich ticket should stay editable (mode=%v)", m.mode)
	}
}

func TestEditPriority(t *testing.T) {
	m, sp := editModel(t, "PAY-142")
	m = openEntry(t, m, itPriority)
	out := view(m)
	for _, want := range []string{"Priorité de PAY-142", "High  ✓", "Medium", "Low"} {
		if !strings.Contains(out, want) {
			t.Errorf("priority modal misses %q\n%s", want, out)
		}
	}
	// same priority: no call
	if m2 := press(t, m, "enter"); len(sp.calls) != 0 || m2.mode != modeNormal {
		t.Fatalf("same priority: calls=%v mode=%v", sp.calls, m2.mode)
	}
	m = press(t, press(t, m, "down"), "enter")
	if len(sp.calls) != 1 || m.detail.Fields.Priority == nil || m.detail.Fields.Priority.Name != "Medium" {
		t.Fatalf("calls=%v priority=%+v", sp.calls, m.detail.Fields.Priority)
	}
	if !strings.Contains(m.status, "priorité de PAY-142 : Medium") {
		t.Errorf("status = %q", m.status)
	}
	// esc goes back to the menu
	m = openEntry(t, m, itPriority)
	if m = press(t, m, "esc"); m.mode != modeEditMenu {
		t.Errorf("esc = %v", m.mode)
	}
}

func TestEditAssignee(t *testing.T) {
	m, sp := editModel(t, "PAY-142")
	m = openEntry(t, m, itAssignee)
	if m.mode != modeAssignee {
		t.Fatalf("mode = %v", m.mode)
	}
	out := view(m)
	for _, want := range []string{"Assigner PAY-142", "Non assigné", "Kevin", "Camille R.", "Sofia M.", "Léo B.", "Camille R.  ✓"} {
		if !strings.Contains(out, want) {
			t.Errorf("assignee modal misses %q\n%s", want, out)
		}
	}
	// typing triggers a delayed search: simulate the end of the pause
	m = typeText(t, m, "so")
	if !m.ed.ubusy {
		t.Error("a search should be pending while typing")
	}
	next, cmd := m.Update(userQueryMsg{m.ed.seq})
	m = settle(t, next.(Model), cmd)
	out = view(m)
	if !strings.Contains(out, "Sofia M.") || strings.Contains(out, "Camille R.") || strings.Contains(out, "Non assigné") {
		t.Errorf("filtered list wrong:\n%s", out)
	}
	if m = press(t, m, "enter"); m.mode != modeNormal || len(sp.calls) != 1 {
		t.Fatalf("mode=%v calls=%v", m.mode, sp.calls)
	}
	if a := m.detail.Fields.Assignee; a == nil || a.DisplayName != "Sofia M." {
		t.Errorf("assignee = %+v", a)
	}
	if !strings.Contains(m.status, "PAY-142 assigné à Sofia M.") {
		t.Errorf("status = %q", m.status)
	}

	// unassign through the first entry
	m = openEntry(t, m, itAssignee)
	m = press(t, m, "enter") // "Non assigné" is first
	if m.detail.Fields.Assignee != nil || sp.calls[len(sp.calls)-1] != "unassign" {
		t.Errorf("unassign: %+v %v", m.detail.Fields.Assignee, sp.calls)
	}
	// choosing the current assignee again changes nothing
	n := len(sp.calls)
	m = openEntry(t, m, itAssignee)
	m = press(t, m, "enter")
	if len(sp.calls) != n {
		t.Errorf("assigning nobody to an unassigned ticket called Jira: %v", sp.calls)
	}
}

func TestAssigneeSearchIgnoresStaleAnswers(t *testing.T) {
	m, _ := editModel(t, "PAY-142")
	m = openEntry(t, m, itAssignee)
	m = typeText(t, m, "c") // seq advances
	old := m.ed.seq
	m = typeText(t, m, "a")
	if m.ed.seq == old {
		t.Fatal("each change must start a new search")
	}
	// the answer of the first search arrives late: it is dropped
	next, _ := m.Update(usersMsg{seq: old, users: []jira.User{{AccountID: "x", DisplayName: "Fantôme"}}})
	m = next.(Model)
	if strings.Contains(view(m), "Fantôme") || !m.ed.ubusy {
		t.Errorf("a stale answer was used (busy=%v)", m.ed.ubusy)
	}
	// a stale debounce tick does not start a search either
	if cmd := m.onUserQuery(userQueryMsg{old}); cmd != nil {
		t.Error("stale tick started a search")
	}
	// the current one is used
	next, _ = m.Update(usersMsg{seq: m.ed.seq, users: []jira.User{{AccountID: "Camille R.", DisplayName: "Camille R."}}})
	m = next.(Model)
	if !strings.Contains(view(m), "Camille R.") || m.ed.ubusy {
		t.Error("the current answer was ignored")
	}
	// an error is shown, not swallowed
	next, _ = m.Update(usersMsg{seq: m.ed.seq, err: jiraErr("accès interdit")})
	m = next.(Model)
	if !strings.Contains(view(m), "accès interdit") {
		t.Errorf("search error not displayed:\n%s", view(m))
	}
}

type jiraErr string

func (e jiraErr) Error() string { return string(e) }

func TestEditAndDeleteOwnComment(t *testing.T) {
	m, sp := editModel(t, "PAY-142")
	// only Kevin's comment is offered (the demo user), not Camille's or Sofia's
	m = openEntry(t, m, itEditComment)
	if m.mode != modeCommentPick || len(m.myComments()) != 1 {
		t.Fatalf("mode=%v mine=%d", m.mode, len(m.myComments()))
	}
	out := view(m)
	if !strings.Contains(out, "Modifier quel commentaire ?") || !strings.Contains(out, "return_url") || strings.Contains(out, "Stripe") || strings.Contains(out, "préprod") {
		t.Errorf("picker:\n%s", out)
	}
	m = press(t, m, "enter")
	if m.mode != modeEditText || !strings.Contains(m.ed.area.Value(), "return_url") {
		t.Fatalf("mode=%v value=%q", m.mode, m.ed.area.Value())
	}
	m.ed.area.SetValue("   ")
	if m = press(t, m, "ctrl+s"); m.mode != modeEditText || !strings.Contains(view(m), "le commentaire ne peut pas être vide") || len(sp.calls) != 0 {
		t.Fatalf("empty comment: mode=%v calls=%v", m.mode, sp.calls)
	}
	m.ed.area.SetValue("Piste confirmée : return_url tronqué.")
	m = press(t, m, "ctrl+s")
	if len(sp.calls) != 1 || sp.calls[0] != "editcomment" {
		t.Fatalf("calls = %v", sp.calls)
	}
	cs := m.detail.Fields.Comment.Comments
	if len(cs) != 3 || jira.ADFToText(cs[2].Body) != "Piste confirmée : return_url tronqué." {
		t.Errorf("comments after edit: %d, last=%q", len(cs), jira.ADFToText(cs[len(cs)-1].Body))
	}
	if cs[2].Public == nil || !*cs[2].Public {
		t.Error("the visibility must survive an edit")
	}

	// delete: confirmation first, esc goes back without deleting
	m = openEntry(t, m, itDelComment)
	m = press(t, m, "enter")
	if m.mode != modeCommentDelete || !strings.Contains(view(m), "Supprimer ce commentaire ?") {
		t.Fatalf("mode=%v\n%s", m.mode, view(m))
	}
	if m = press(t, m, "esc"); m.mode != modeCommentPick || len(sp.calls) != 1 {
		t.Fatalf("esc: mode=%v calls=%v", m.mode, sp.calls)
	}
	m = press(t, press(t, m, "enter"), "enter")
	if len(sp.calls) != 2 || sp.calls[1] != "deletecomment" {
		t.Fatalf("calls = %v", sp.calls)
	}
	if n := len(m.detail.Fields.Comment.Comments); n != 2 {
		t.Errorf("comments after delete = %d", n)
	}
	if !strings.Contains(m.status, "commentaire supprimé de PAY-142") {
		t.Errorf("status = %q", m.status)
	}
}

func TestNoOwnCommentToEdit(t *testing.T) {
	m, sp := editModel(t, "PAY-139") // no comment at all
	m = press(t, m, "e")
	m = downs(t, m, itEditComment)
	if out := view(m); !strings.Contains(out, "aucun commentaire de vous") {
		t.Errorf("reason missing:\n%s", out)
	}
	if m = press(t, m, "enter"); m.mode != modeEditMenu || len(sp.calls) != 0 {
		t.Errorf("mode=%v calls=%v", m.mode, sp.calls)
	}
}

func TestRichCommentIsNeverRewritten(t *testing.T) {
	m, sp := editModel(t, "PAY-142")
	// make Kevin's comment hold an image: Markdown cannot keep it, the app must refuse to flatten it
	cs := m.detail.Fields.Comment.Comments
	cs[2].Body = map[string]any{"type": "doc", "content": []any{
		map[string]any{"type": "paragraph", "content": []any{map[string]any{"type": "text", "text": "voir la capture"}}},
		map[string]any{"type": "mediaSingle", "content": []any{map[string]any{"type": "media", "attrs": map[string]any{"id": "1", "type": "file"}}}},
	}}
	m.detail.Fields.Comment.Comments = cs
	m = press(t, m, "e")
	m = downs(t, m, itEditComment)
	if out := view(m); !strings.Contains(out, "contenu non modifiable ici") {
		t.Errorf("the entry should be disabled with a reason:\n%s", out)
	}
	// deleting it stays possible
	m = press(t, downs(t, m, 1), "enter")
	if m.mode != modeCommentPick {
		t.Fatalf("delete picker mode = %v", m.mode)
	}
	if len(sp.calls) != 0 {
		t.Errorf("calls = %v", sp.calls)
	}
}

func TestEscNeverWrites(t *testing.T) {
	for idx := itTitle; idx <= itDelComment; idx++ {
		m, sp := editModel(t, "PAY-142")
		m = openEntry(t, m, idx)
		for i := 0; i < 4 && m.mode != modeNormal; i++ {
			m = press(t, m, "esc")
		}
		if m.mode != modeNormal || len(sp.calls) != 0 {
			t.Errorf("entry %d: mode=%v calls=%v", idx, m.mode, sp.calls)
		}
	}
}

func TestEditErrorIsShown(t *testing.T) {
	m, sp := editModel(t, "PAY-142")
	m = openEntry(t, m, itPriority)
	m.ed.meta.Priorities = []jira.Named{{ID: "99", Name: "Bogus"}} // Jira would refuse it
	m.ed.cur = 0
	m = press(t, m, "enter")
	if !m.isErr || !strings.Contains(m.status, "priorité 99 inconnue") || len(sp.calls) != 1 {
		t.Errorf("status=%q err=%v calls=%v", m.status, m.isErr, sp.calls)
	}
}

func TestEditModalsFitSmallTerminals(t *testing.T) {
	sp := &spy{Service: mock.New()}
	for _, sz := range [][2]int{{80, 24}, {60, 20}} {
		for idx := itTitle; idx <= itDelComment; idx++ {
			m := New(sp, config.Config{JQL: config.DefaultJQL}).OpenIssue("PAY-142")
			next, _ := m.Update(tea.WindowSizeMsg{Width: sz[0], Height: sz[1]})
			m = settle(t, next.(Model), next.(Model).Init())
			m = openEntry(t, m, idx)
			for step := 0; step < 2; step++ {
				for i, l := range strings.Split(view(m), "\n") {
					if w := lipgloss.Width(l); w > sz[0] {
						t.Fatalf("%dx%d entry %d: line %d is %d cells wide\n%s", sz[0], sz[1], idx, i, w, view(m))
					}
				}
				if m.mode == modeCommentPick {
					m = press(t, m, "enter") // also check the next modal of the comment flow
				} else {
					break
				}
			}
		}
	}
}
