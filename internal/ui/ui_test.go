package ui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/kevin/jira-tui/internal/config"
	"github.com/kevin/jira-tui/internal/jira"
	"github.com/kevin/jira-tui/internal/mock"
)

// runCmd executes cmd but gives up on timers (cursor blink, spinner ticks).
func runCmd(c tea.Cmd) tea.Msg {
	ch := make(chan tea.Msg, 1)
	go func() { ch <- c() }()
	select {
	case msg := <-ch:
		return msg
	case <-time.After(50 * time.Millisecond):
		return nil
	}
}

// settle feeds msgs produced by cmd (including batches) back into the model.
func settle(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	queue := []tea.Cmd{cmd}
	for i := 0; len(queue) > 0 && i < 200; i++ {
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		msg := runCmd(c)
		if msg == nil {
			continue
		}
		if b, ok := msg.(tea.BatchMsg); ok {
			queue = append(queue, b...)
			continue
		}
		next, c2 := m.Update(msg)
		m = next.(Model)
		queue = append(queue, c2)
	}
	return m
}

func press(t *testing.T, m Model, s string) Model {
	t.Helper()
	var k tea.Key
	switch s {
	case "enter":
		k = tea.Key{Code: tea.KeyEnter}
	case "esc":
		k = tea.Key{Code: tea.KeyEscape}
	case "tab":
		k = tea.Key{Code: tea.KeyTab}
	case "backspace":
		k = tea.Key{Code: tea.KeyBackspace}
	case "up":
		k = tea.Key{Code: tea.KeyUp}
	case "down":
		k = tea.Key{Code: tea.KeyDown}
	case "left":
		k = tea.Key{Code: tea.KeyLeft}
	case "right":
		k = tea.Key{Code: tea.KeyRight}
	case "ctrl+s":
		k = tea.Key{Code: 's', Mod: tea.ModCtrl}
	default:
		r := []rune(s)
		k = tea.Key{Code: r[0], Text: s}
	}
	next, cmd := m.Update(tea.KeyPressMsg(k))
	return settle(t, next.(Model), cmd)
}

func typeText(t *testing.T, m Model, s string) Model {
	for _, r := range s {
		m = press(t, m, string(r))
	}
	return m
}

func demo(t *testing.T, w, h int) Model {
	t.Helper()
	m := New(mock.New(), config.Config{JQL: config.DefaultJQL})
	next, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	m = next.(Model)
	return settle(t, m, m.cmdSearch(m.jqlString()))
}

func view(m Model) string { return ansi.Strip(m.View().Content) }

func TestHelpers(t *testing.T) {
	if g, _ := statusKind(jira.Status{Name: "En revue", Category: jira.StatusCategory{Key: "indeterminate"}}); g != "◑" {
		t.Errorf("review glyph = %q", g)
	}
	if g, _ := statusKind(jira.Status{Name: "Terminé"}); g != "✓" { // inferred from the name (board column)
		t.Errorf("done glyph = %q", g)
	}
	for in, want := range map[string]string{"Highest": "▲▲ Haute", "Medium": "▲ Moyenne", "Low": "▽ Basse"} {
		if got, _ := priorityInfo(&jira.Named{Name: in}); got != want {
			t.Errorf("priority %s = %q, want %q", in, got, want)
		}
	}
	if got := initials(&jira.User{DisplayName: "Camille Roux"}); got != "CR" {
		t.Errorf("initials = %q", got)
	}
	if got := initials(&jira.User{DisplayName: "Kevin"}); got != "KE" {
		t.Errorf("initials = %q", got)
	}
}

func TestJQLChips(t *testing.T) {
	m := New(mock.New(), config.Config{JQL: config.DefaultJQL})
	if got := m.jqlString(); got != config.DefaultJQL {
		t.Fatalf("default = %q", got)
	}
	m.onlyMine = false
	if got := m.jqlString(); got != "resolution = Unresolved ORDER BY updated DESC" {
		t.Fatalf("got %q", got)
	}
	m.onlyOpen = false
	if got := m.jqlString(); !strings.HasPrefix(got, "updated >=") {
		t.Fatalf("unbounded query must be bounded: %q", got)
	}
	m.rawJQL = "project = X"
	if got := m.jqlString(); got != "project = X" {
		t.Fatalf("raw JQL ignored: %q", got)
	}
}

func TestListView(t *testing.T) {
	m := demo(t, 130, 34)
	out := view(m)
	for _, want := range []string{"CLÉ", "PRIORITÉ", "PAY-142", "assigné : moi", "statut : ouverts", "11 résultats", "1 Liste", "PAY-142 · 1/11"} {
		if !strings.Contains(out, want) {
			t.Errorf("list view misses %q", want)
		}
	}
}

func TestTextFilter(t *testing.T) {
	m := demo(t, 130, 34)
	m = press(t, m, "/")
	m = typeText(t, m, "apple")
	if got := len(m.visible()); got != 1 || m.visible()[0].Key != "PAY-139" {
		t.Fatalf("visible = %d", got)
	}
	if !strings.Contains(view(m), "1 résultat") {
		t.Error("count not updated")
	}
	m = press(t, m, "esc")
	if len(m.visible()) != 11 || m.mode != modeNormal {
		t.Fatalf("esc must clear the filter, got %d / mode %v", len(m.visible()), m.mode)
	}
}

func TestChipsReload(t *testing.T) {
	m := demo(t, 130, 34)
	m = press(t, m, "O") // statut : tous → includes "Terminé" tickets
	if len(m.issues) != 19 || !strings.Contains(view(m), "statut : tous") {
		t.Fatalf("issues = %d", len(m.issues))
	}
	m = press(t, m, "m")
	if !strings.Contains(view(m), "assigné : tous") {
		t.Error("mine chip not toggled")
	}
}

func TestBoardView(t *testing.T) {
	m := press(t, demo(t, 130, 34), "2")
	out := view(m)
	for _, want := range []string{"À FAIRE", "EN COURS", "EN REVUE", "TERMINÉ", "de plus", "Tableau PAY", "PAY-141 · À faire 1/6"} {
		if !strings.Contains(out, want) {
			t.Errorf("board view misses %q", want)
		}
	}
	// move PAY-141 (À faire) to the next column
	m = press(t, m, "L")
	for _, is := range m.bissues {
		if is.Key == "PAY-141" && is.Fields.Status.Name != "En cours" {
			t.Fatalf("PAY-141 is %q", is.Fields.Status.Name)
		}
	}
}

func TestDetailAndComment(t *testing.T) {
	m := press(t, demo(t, 130, 34), "enter")
	out := view(m)
	for _, want := range []string{"Le paiement échoue", "Description", "Commentaires (3)", "Nouveau commentaire", "Détails", "Checkout", "3 Détail"} {
		if !strings.Contains(out, want) {
			t.Errorf("detail view misses %q", want)
		}
	}
	m = press(t, m, "c")
	m = typeText(t, m, "ok")
	m = press(t, m, "ctrl+s")
	if got := len(m.detail.Fields.Comment.Comments); got != 4 {
		t.Fatalf("comments = %d, want 4", got)
	}
	m = press(t, m, "esc")
	if m.view != viewList {
		t.Fatalf("esc should go back to the list, view = %v", m.view)
	}
	if m = press(t, m, "3"); m.view != viewDetail {
		t.Error("tab 3 should reopen the detail")
	}
}

func TestStatusModal(t *testing.T) {
	m := press(t, demo(t, 130, 34), "s")
	if m.mode != modeTransition || !strings.Contains(view(m), "Changer le statut de PAY-142") {
		t.Fatalf("mode = %v", m.mode)
	}
	m = press(t, m, "j") // → En cours is index 1, go to "En revue"
	m = press(t, m, "j")
	m = press(t, m, "enter")
	for _, is := range m.issues {
		if is.Key == "PAY-142" && is.Fields.Status.Name != "En revue" {
			t.Fatalf("status = %q", is.Fields.Status.Name)
		}
	}
}

func TestNarrowTerminalDoesNotPanic(t *testing.T) {
	for _, sz := range [][2]int{{60, 20}, {80, 24}, {100, 30}, {200, 60}} {
		m := demo(t, sz[0], sz[1])
		_ = view(m)
		m = press(t, m, "2")
		_ = view(m)
		m = press(t, m, "1")
		m = press(t, m, "enter")
		_ = view(m)
	}
}

func TestTransparent(t *testing.T) {
	m := demo(t, 100, 30)
	if m.View().BackgroundColor == nil {
		t.Error("default should paint the palette background")
	}
	if v := m.Transparent(true).View(); v.BackgroundColor != nil || v.ForegroundColor != nil {
		t.Error("transparent must not set terminal colors")
	}
}

func TestFooterShortcuts(t *testing.T) {
	m := demo(t, 130, 34)
	out := view(m)
	for _, want := range []string{"m tous les assignés", "O tous les statuts", "s statut", "ctrl+j JQL", "f filtres", "b board", "x automation", "o Jira", "r rafraîchir", "? aide"} {
		if !strings.Contains(out, want) {
			t.Errorf("list footer misses %q", want)
		}
	}
	m = press(t, m, "m") // chip off → the label offers to go back to "moi"
	if !strings.Contains(view(m), "m assigné à moi") {
		t.Error("label of m should flip once the chip is off")
	}
	m = press(t, m, "2")
	if out = view(m); !strings.Contains(out, "H/L déplacer le ticket") || !strings.Contains(out, "b board") {
		t.Error("board footer incomplete")
	}
	m = press(t, press(t, m, "1"), "enter")
	if out = view(m); !strings.Contains(out, "a m'assigner") || !strings.Contains(out, "u désassigner") {
		t.Error("detail footer incomplete")
	}
}

func TestFooterAdaptsToSize(t *testing.T) {
	// short terminal: essentials only, the body keeps its space
	m := demo(t, 130, 20)
	out := view(m)
	if strings.Contains(out, "assigné à moi") || strings.Contains(out, "tous les assignés") {
		t.Error("2nd shortcut line should be hidden on short terminals")
	}
	if !strings.Contains(out, "q quitter") {
		t.Error("essentials missing")
	}
	// no footer line may overflow the width, whatever the size
	for _, sz := range [][2]int{{60, 30}, {80, 24}, {100, 24}, {130, 34}} {
		m := demo(t, sz[0], sz[1])
		for _, line := range strings.Split(view(m), "\n") {
			if w := ansi.StringWidth(line); w > sz[0] {
				t.Errorf("%dx%d: line of %d cells: %q", sz[0], sz[1], w, line)
			}
		}
		if got := len(strings.Split(view(m), "\n")); got > sz[1] {
			t.Errorf("%dx%d: %d lines", sz[0], sz[1], got)
		}
	}
}

func TestCommentVisibility(t *testing.T) {
	m := press(t, demo(t, 130, 36), "enter") // PAY-142
	out := view(m)
	if !strings.Contains(out, "[interne]") || !strings.Contains(out, "[client]") {
		t.Fatal("existing comments should show their visibility")
	}

	// default = client, announced before typing
	m = press(t, m, "c")
	if out = view(m); !strings.Contains(out, "visible du client") || strings.Contains(out, "non visible du client") || m.commentInternal {
		t.Fatal("c should start on Client")
	}
	if !strings.Contains(out, "tab client/interne") {
		t.Error("footer should mention tab")
	}
	m = press(t, m, "tab")
	if out = view(m); !strings.Contains(out, "non visible du client") || !m.commentInternal {
		t.Fatal("tab should switch to Interne")
	}
	if m = press(t, m, "tab"); m.commentInternal {
		t.Fatal("tab again should come back to Client")
	}
	m = press(t, m, "tab") // Interne
	m = typeText(t, m, "note")
	m = press(t, m, "ctrl+s")
	cs := m.detail.Fields.Comment.Comments
	last := cs[len(cs)-1]
	if last.Public == nil || *last.Public {
		t.Fatalf("an internal comment must not be public: %+v", last.Public)
	}
	if !strings.Contains(m.status, "commentaire interne ajouté à PAY-142") {
		t.Errorf("status = %q", m.status)
	}

	// the next comment starts on Client again, whatever the previous choice was
	m = press(t, m, "c")
	if m.commentInternal {
		t.Error("each comment should start on Client")
	}
	m = typeText(t, m, "pour le client")
	m = press(t, m, "ctrl+s")
	cs = m.detail.Fields.Comment.Comments
	if last = cs[len(cs)-1]; last.Public == nil || !*last.Public || !strings.Contains(m.status, "commentaire client ajouté") {
		t.Fatalf("client comment: public=%v status=%q", last.Public, m.status)
	}
}

func TestCommentsErrorIsShown(t *testing.T) {
	m := press(t, demo(t, 130, 36), "enter")
	m.detail.CommentsErr = "jira 403: forbidden"
	m.relayout()
	if out := view(m); !strings.Contains(out, "Commentaires indisponibles") {
		t.Error("a comments failure must be visible")
	}
}

func initModel(t *testing.T, m Model) Model {
	t.Helper()
	next, _ := m.Update(tea.WindowSizeMsg{Width: 130, Height: 36})
	m = next.(Model)
	return settle(t, m, m.Init())
}

func TestOpenIssueFromCommandLine(t *testing.T) {
	m := New(mock.New(), config.Config{JQL: config.DefaultJQL}).WithTime(mock.NewTime(), 8).OpenIssue("PAY-139")
	m = initModel(t, m)
	if m.view != viewDetail || m.detail.Key != "PAY-139" || !m.hasDetail || m.pendingOpen != "" {
		t.Fatalf("view=%v key=%q pending=%q", m.view, m.detail.Key, m.pendingOpen)
	}
	if m.loading != 0 {
		t.Errorf("loading counter = %d", m.loading)
	}
	if out := view(m); !strings.Contains(out, "Ajouter Apple Pay au checkout") || !strings.Contains(out, "PAY › PAY-139") && !strings.Contains(out, "PAY-139") {
		t.Error("detail of PAY-139 not displayed")
	}
	if m = press(t, m, "esc"); m.view != viewList || len(m.issues) == 0 {
		t.Fatalf("esc should go back to the loaded list (view=%v, issues=%d)", m.view, len(m.issues))
	}
}

func TestOpenIssueLoadsTempo(t *testing.T) {
	m := New(mock.New(), config.Config{JQL: config.DefaultJQL}).WithTime(mock.NewTime(), 8).OpenIssue("PAY-142")
	m = initModel(t, m)
	if !m.wlLoaded || len(m.worklogs) != 3 || !strings.Contains(view(m), "Temps passé (5 h 15 min") {
		t.Error("worklogs of the opened ticket should load")
	}
}

func TestOpenUnknownIssueStaysOnList(t *testing.T) {
	m := New(mock.New(), config.Config{JQL: config.DefaultJQL}).OpenIssue("ZZZ-1")
	m = initModel(t, m)
	if m.view != viewList || m.hasDetail || !m.isErr || !strings.Contains(m.status, "ZZZ-1") {
		t.Fatalf("view=%v err=%v status=%q", m.view, m.isErr, m.status)
	}
	if len(m.issues) == 0 {
		t.Error("the list should still load")
	}
}
