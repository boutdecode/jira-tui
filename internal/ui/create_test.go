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

// listModel is the demo list on a terminal of the given size.
func listModel(t *testing.T, sp *spy, w, h int) Model {
	t.Helper()
	m := New(sp, config.Config{JQL: config.DefaultJQL})
	next, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	m = next.(Model)
	return settle(t, m, m.Init())
}

func newSpy() *spy { return &spy{Service: mock.New()} }

// typeInto types s in the focused field.
func typeInto(t *testing.T, m Model, s string) Model { return typeText(t, m, s) }

func typeNames(m Model) string {
	var out []string
	for _, ty := range m.cr.types {
		out = append(out, ty.Name)
	}
	return strings.Join(out, ",")
}

func TestCreateFormOpensWithDefaults(t *testing.T) {
	sp := newSpy()
	m := press(t, listModel(t, sp, 130, 40), "n")
	if m.mode != modeCreate || m.cr.project.Key != "PAY" || m.cr.focus != fTitle || m.cr.parent != "" {
		t.Fatalf("mode=%v project=%q focus=%v parent=%q", m.mode, m.cr.project.Key, m.cr.focus, m.cr.parent)
	}
	if got := typeNames(m); got != "Tâche,Bug,Story" {
		t.Errorf("types = %s (a sub-task is only offered from an open ticket)", got)
	}
	if t0, _ := m.cr.selectedType(); t0.Name != "Tâche" || m.cr.loading {
		t.Errorf("type=%+v loading=%v", t0, m.cr.loading)
	}
	out := view(m)
	for _, want := range []string{"Nouveau ticket", "Projet", "PAY", "Type", "Tâche", "Titre", "Résumé du ticket", "Description", "Priorité", "Étiquettes", "Assigné", "Automatique", "ctrl+s créer"} {
		if !strings.Contains(out, want) {
			t.Errorf("form misses %q\n%s", want, out)
		}
	}
	// the same key works from the board and the detail
	for name, mm := range map[string]Model{"board": press(t, listModel(t, sp, 130, 40), "2"), "detail": press(t, listModel(t, sp, 130, 40), "enter")} {
		if mm = press(t, mm, "n"); mm.mode != modeCreate {
			t.Errorf("%s: n opened %v", name, mm.mode)
		}
	}
	// and the key is in the footer
	if h := hintsOf(listModel(t, sp, 130, 40)); !strings.Contains(h, "n nouveau") {
		t.Errorf("hints = %s", h)
	}
}

func TestCreateEmptyTitleIsRefused(t *testing.T) {
	sp := newSpy()
	m := press(t, listModel(t, sp, 130, 40), "n")
	m = press(t, m, "ctrl+s")
	if m.mode != modeCreate || !strings.Contains(view(m), "le titre ne peut pas être vide") || len(sp.calls) != 0 {
		t.Fatalf("mode=%v calls=%v\n%s", m.mode, sp.calls, view(m))
	}
	m = typeInto(t, m, "   ")
	if m = press(t, m, "ctrl+s"); len(sp.calls) != 0 {
		t.Errorf("a blank title was sent: %v", sp.calls)
	}
}

func TestCreateAnIssue(t *testing.T) {
	sp := newSpy()
	m := press(t, listModel(t, sp, 130, 40), "n")
	m = typeInto(t, m, "Le panneau plante")
	m = press(t, m, "tab") // description
	if m.cr.focus != fDesc {
		t.Fatalf("focus = %v", m.cr.focus)
	}
	m = typeInto(t, m, "Voir avec @")
	m = typeInto(t, m, "so")
	m = pause(t, m)
	if !strings.Contains(view(m), "Sofia M.") {
		t.Errorf("the @ picker should list the people of the project:\n%s", view(m))
	}
	m = press(t, m, "enter")
	m = typeInto(t, m, "pour #")
	m = typeInto(t, m, "PAY-137")
	m = pause(t, m)
	m = press(t, m, "enter")
	if v := m.cr.desc.Value(); v != "Voir avec @Sofia M. pour #PAY-137 " {
		t.Fatalf("description = %q", v)
	}
	m = press(t, m, "tab") // priority
	m = press(t, m, "right")
	m = press(t, m, "tab") // labels
	m = typeInto(t, m, "urgent, paiement")
	m = press(t, m, "tab") // assignee
	m = press(t, m, "right")
	if !m.cr.me {
		t.Fatal("right should select Me")
	}
	out := view(m)
	for _, want := range []string{"High", "urgent, paiement", "Moi"} {
		if !strings.Contains(out, want) {
			t.Errorf("form misses %q\n%s", want, out)
		}
	}

	m = press(t, m, "ctrl+s")
	if len(sp.created) != 1 {
		t.Fatalf("creations = %d (%s)", len(sp.created), m.cr.err)
	}
	in := sp.created[0]
	if in.ProjectKey != "PAY" || in.TypeID != "10" || in.Summary != "Le panneau plante" || in.PriorityID != "1" ||
		strings.Join(in.Labels, "|") != "urgent|paiement" || !in.AssignToMe || in.ParentKey != "" {
		t.Errorf("sent %+v", in)
	}
	var kinds []string
	for _, n := range nodesIn(t, *in.Description) {
		kinds = append(kinds, n["type"].(string))
	}
	if strings.Join(kinds, ",") != "text,mention,text,inlineCard" {
		t.Errorf("description nodes = %v", kinds)
	}
	// the form is gone and the new ticket is open
	if m.mode != modeNormal || m.view != viewDetail || m.detail.Key != "PAY-147" {
		t.Fatalf("mode=%v view=%v detail=%q status=%q", m.mode, m.view, m.detail.Key, m.status)
	}
	if !strings.Contains(m.status, "PAY-147 créé : Le panneau plante") || m.isErr {
		t.Errorf("status = %q", m.status)
	}
	if m.detail.Fields.Assignee == nil || m.detail.Fields.Assignee.DisplayName != "Kevin" {
		t.Errorf("assignee = %+v", m.detail.Fields.Assignee)
	}
	found := false
	for _, is := range m.issues {
		found = found || is.Key == "PAY-147"
	}
	if !found {
		t.Error("the list was not reloaded")
	}
	if m.lastProject != "PAY" {
		t.Errorf("lastProject = %q", m.lastProject)
	}
}

func TestCreateTypeCyclingAndProjectPicker(t *testing.T) {
	sp := newSpy()
	m := listModelOpen(t, sp)
	m = typeInto(t, m, "Brouillon")
	m = press(t, m, "shift+tab") // title -> type
	if m.cr.focus != fType {
		t.Fatalf("focus = %v", m.cr.focus)
	}
	m = press(t, m, "right")
	if tn, _ := m.cr.selectedType(); tn.Name != "Bug" || m.cr.loading {
		t.Errorf("type = %+v loading=%v", tn, m.cr.loading)
	}
	m = press(t, m, "left")
	m = press(t, m, "left") // wraps around to the last one
	if tn, _ := m.cr.selectedType(); tn.Name != "Story" {
		t.Errorf("type = %+v", tn)
	}

	// the project picker
	m = listModelOpen(t, sp)
	m = typeInto(t, m, "Brouillon")
	m = press(t, press(t, m, "shift+tab"), "shift+tab") // type, then project
	if m.cr.focus != fProject {
		t.Fatalf("focus = %v", m.cr.focus)
	}
	m = press(t, m, "enter")
	if !m.cr.picking {
		t.Fatal("enter on the project should open the list")
	}
	out := view(m)
	for _, want := range []string{"Choisir un projet", "PAY", "OPS", "WEB", "Paiements", "Exploitation"} {
		if !strings.Contains(out, want) {
			t.Errorf("project list misses %q\n%s", want, out)
		}
	}
	m = typeInto(t, m, "ops")
	next, cmd := m.Update(projectQueryMsg{m.cr.pseq})
	m = settle(t, next.(Model), cmd)
	if len(m.cr.projects) != 1 || m.cr.projects[0].Key != "OPS" {
		t.Fatalf("projects = %+v", m.cr.projects)
	}
	m = press(t, m, "enter")
	if m.cr.picking || m.cr.project.Key != "OPS" || typeNames(m) != "Tâche,Incident" {
		t.Fatalf("picking=%v project=%q types=%s", m.cr.picking, m.cr.project.Key, typeNames(m))
	}
	if m.cr.title.Value() != "Brouillon" || m.cr.focus != fTitle {
		t.Errorf("the draft must survive a project change: title=%q focus=%v", m.cr.title.Value(), m.cr.focus)
	}
	// create in OPS, then the next form starts there
	m = press(t, m, "ctrl+s")
	if len(sp.created) != 1 || sp.created[0].ProjectKey != "OPS" || sp.created[0].TypeID != "20" || m.detail.Key != "OPS-1" {
		t.Fatalf("created %+v detail=%q", sp.created, m.detail.Key)
	}
	if m = press(t, press(t, m, "esc"), "n"); m.cr.project.Key != "OPS" {
		t.Errorf("the form should start on the last project used, got %q", m.cr.project.Key)
	}
}

// listModelOpen opens a fresh form on the demo list.
func listModelOpen(t *testing.T, sp *spy) Model { return press(t, listModel(t, sp, 130, 40), "n") }

func TestCreateBlockedByAnUnsupportedRequiredField(t *testing.T) {
	sp := newSpy()
	m := listModelOpen(t, sp)
	m = typeInto(t, m, "Sur le site")
	m = press(t, press(t, m, "shift+tab"), "shift+tab")
	m = press(t, m, "enter")
	m = typeInto(t, m, "web")
	next, cmd := m.Update(projectQueryMsg{m.cr.pseq})
	m = settle(t, next.(Model), cmd)
	m = press(t, m, "enter")
	if m.cr.project.Key != "WEB" || len(m.cr.meta.Unsupported) != 1 {
		t.Fatalf("project=%q unsupported=%v", m.cr.project.Key, m.cr.meta.Unsupported)
	}
	out := view(m)
	if !strings.Contains(out, "champ obligatoire non géré : Environnement") || !strings.Contains(out, "ctrl+o") {
		t.Errorf("the blocking reason is missing:\n%s", out)
	}
	if m = press(t, m, "ctrl+s"); len(sp.calls) != 0 || m.mode != modeCreate {
		t.Errorf("creation must be refused before any request: calls=%v mode=%v", sp.calls, m.mode)
	}
	if got := m.createURL(); got != "https://example.atlassian.net/secure/CreateIssue!default.jspa" {
		t.Errorf("createURL = %q", got)
	}
}

func TestCreateFailureKeepsTheDraft(t *testing.T) {
	sp := newSpy()
	sp.createErr = jiraErr("summary: Le résumé est trop long")
	m := listModelOpen(t, sp)
	m = typeInto(t, m, "Titre à garder")
	m = press(t, m, "tab")
	m = typeInto(t, m, "Une description\n")
	m = press(t, m, "ctrl+s")
	if m.mode != modeCreate || m.cr.creating || !strings.Contains(view(m), "Le résumé est trop long") {
		t.Fatalf("mode=%v creating=%v\n%s", m.mode, m.cr.creating, view(m))
	}
	if m.cr.title.Value() != "Titre à garder" || !strings.Contains(m.cr.desc.Value(), "Une description") {
		t.Errorf("the draft was lost: %q / %q", m.cr.title.Value(), m.cr.desc.Value())
	}
	// fix the problem and send again
	sp.createErr = nil
	m = press(t, m, "ctrl+s")
	if m.mode != modeNormal || m.detail.Key != "PAY-147" {
		t.Errorf("second attempt: mode=%v detail=%q", m.mode, m.detail.Key)
	}
}

func TestCreateSubTaskFromAnOpenTicket(t *testing.T) {
	sp := newSpy()
	m := press(t, press(t, listModel(t, sp, 130, 40), "enter"), "n") // detail of PAY-142
	if m.cr.parent != "PAY-142" || typeNames(m) != "Tâche,Bug,Story,Sous-tâche" {
		t.Fatalf("parent=%q types=%s", m.cr.parent, typeNames(m))
	}
	m = typeInto(t, m, "Écrire le test")
	m = press(t, m, "shift+tab") // title -> type
	for i := 0; i < 3; i++ {
		m = press(t, m, "right")
	}
	if tn, _ := m.cr.selectedType(); !tn.Subtask {
		t.Fatalf("type = %+v", tn)
	}
	out := view(m)
	if !strings.Contains(out, "Nouvelle sous-tâche de PAY-142") || !strings.Contains(out, "sous-tâche de PAY-142") {
		t.Errorf("the parent must be shown:\n%s", out)
	}
	m = press(t, m, "ctrl+s")
	if len(sp.created) != 1 || sp.created[0].ParentKey != "PAY-142" || sp.created[0].TypeID != "13" {
		t.Fatalf("created %+v (%s)", sp.created, m.cr.err)
	}
	// a normal type from the same form sends no parent
	sp2 := newSpy()
	m2 := press(t, press(t, listModel(t, sp2, 130, 40), "enter"), "n")
	m2 = typeInto(t, m2, "Pas une sous-tâche")
	m2 = press(t, m2, "ctrl+s")
	if len(sp2.created) != 1 || sp2.created[0].ParentKey != "" {
		t.Errorf("created %+v", sp2.created)
	}
}

func TestCreateEscAndDraftConfirmation(t *testing.T) {
	sp := newSpy()
	m := listModelOpen(t, sp)
	if m = press(t, m, "esc"); m.mode != modeNormal {
		t.Fatalf("an untouched form closes at once (mode=%v)", m.mode)
	}
	m = listModelOpen(t, sp)
	m = typeInto(t, m, "Un début")
	m = press(t, m, "esc")
	if m.mode != modeCreate || !m.cr.confirm || !strings.Contains(view(m), "Abandonner le brouillon ?") {
		t.Fatalf("mode=%v confirm=%v\n%s", m.mode, m.cr.confirm, view(m))
	}
	// esc: keep writing, the text is still there
	if m = press(t, m, "esc"); m.cr.confirm || m.cr.title.Value() != "Un début" {
		t.Errorf("confirm=%v title=%q", m.cr.confirm, m.cr.title.Value())
	}
	m = press(t, press(t, m, "esc"), "enter")
	if m.mode != modeNormal || len(sp.calls) != 0 {
		t.Errorf("abandon: mode=%v calls=%v", m.mode, sp.calls)
	}
	if m = press(t, m, "n"); m.cr.title.Value() != "" {
		t.Error("an abandoned draft must not come back")
	}
}

func TestCreateWithoutAnyProjectOnScreenStartsWithThePicker(t *testing.T) {
	sp := newSpy()
	m := New(sp, config.Config{JQL: config.DefaultJQL}) // nothing loaded yet
	next, _ := m.Update(tea.WindowSizeMsg{Width: 130, Height: 40})
	m = next.(Model)
	m = press(t, m, "n")
	if m.mode != modeCreate || !m.cr.picking || len(m.cr.projects) != 3 {
		t.Fatalf("mode=%v picking=%v projects=%d", m.mode, m.cr.picking, len(m.cr.projects))
	}
	// leaving without choosing closes the form
	if m = press(t, m, "esc"); m.mode != modeNormal {
		t.Errorf("mode = %v", m.mode)
	}
	m = press(t, m, "n")
	m = press(t, press(t, m, "down"), "enter")
	if m.cr.project.Key != "OPS" || m.cr.focus != fTitle || typeNames(m) != "Tâche,Incident" {
		t.Errorf("project=%q focus=%v types=%s", m.cr.project.Key, m.cr.focus, typeNames(m))
	}
}

func TestCreateEnglishAndSmallScreens(t *testing.T) {
	english(t)
	sp := newSpy()
	m := press(t, listModel(t, sp, 130, 40), "n")
	out := view(m)
	for _, want := range []string{"New issue", "Project", "Type", "Title", "Issue summary", "Task", "Priority", "Labels", "Assignee", "Automatic (project default)", "ctrl+s create"} {
		if !strings.Contains(out, want) {
			t.Errorf("EN form misses %q\n%s", want, out)
		}
	}
	noFrench(t, "create form", out)
	if h := hintsOf(m); !strings.Contains(h, "create") {
		t.Errorf("hints = %s", h)
	}
	m = typeInto(t, m, "x")
	m = press(t, m, "esc")
	noFrench(t, "discard", view(m))
	if !strings.Contains(view(m), "Discard the draft?") {
		t.Errorf("EN confirm:\n%s", view(m))
	}
	m = press(t, m, "esc")
	m = press(t, press(t, m, "shift+tab"), "shift+tab")
	m = press(t, m, "enter")
	noFrench(t, "project picker", view(m))
	if !strings.Contains(view(m), "Choose a project") {
		t.Errorf("EN picker:\n%s", view(m))
	}

	// every screen of the form fits the terminal
	for _, sz := range [][2]int{{80, 24}, {60, 20}} {
		mm := press(t, listModel(t, sp, sz[0], sz[1]), "n")
		screens := map[string]Model{"form": mm}
		d := press(t, mm, "tab")
		screens["description"] = d
		picker := press(t, press(t, press(t, mm, "shift+tab"), "shift+tab"), "enter")
		screens["picker"] = picker
		typed := typeInto(t, press(t, mm, "tab"), "a @")
		screens["mention picker"] = typed
		for name, s := range screens {
			lines := strings.Split(view(s), "\n")
			if len(lines) > sz[1] {
				t.Errorf("%dx%d %s: %d lines for a %d-row terminal", sz[0], sz[1], name, len(lines), sz[1])
			}
			for i, l := range lines {
				if w := lipgloss.Width(l); w > sz[0] {
					t.Errorf("%dx%d %s: line %d is %d wide\n%s", sz[0], sz[1], name, i, w, view(s))
					break
				}
			}
		}
	}
}

func TestCreateIgnoresStaleLoads(t *testing.T) {
	sp := newSpy()
	m := listModelOpen(t, sp)
	old := m.cr.reqSeq
	// a late answer for a previous project or type is dropped
	next, _ := m.Update(createMetaMsg{seq: old - 1, meta: jira.NewIssueMeta{Unsupported: []string{"Fantôme"}}})
	m = next.(Model)
	if len(m.cr.meta.Unsupported) != 0 {
		t.Error("a stale form description was applied")
	}
	next, _ = m.Update(createTypesMsg{seq: old - 1, project: "X", types: []jira.IssueType{{ID: "9", Name: "Fantôme"}}})
	m = next.(Model)
	if strings.Contains(typeNames(m), "Fantôme") {
		t.Error("stale issue types were applied")
	}
	// an error loading the types is shown and does not break the form
	next, _ = m.Update(createTypesMsg{seq: m.cr.reqSeq, project: "PAY", err: jiraErr("accès interdit (403)")})
	m = next.(Model)
	if !strings.Contains(view(m), "accès interdit (403)") {
		t.Errorf("error not shown:\n%s", view(m))
	}
}
