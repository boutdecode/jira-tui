package ui

import (
	tea "charm.land/bubbletea/v2"
	"errors"
	"strings"
	"testing"

	"github.com/kevin/jira-tui/internal/automation"
	"github.com/kevin/jira-tui/internal/config"
	"github.com/kevin/jira-tui/internal/mock"
)

func demoAuto(t *testing.T, extra ...config.Automation) (Model, *mock.Automations) {
	t.Helper()
	ms := mock.New()
	as := mock.NewAutomations(ms)
	m := New(ms, config.Config{JQL: config.DefaultJQL}).WithAutomations(as, extra)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 130, Height: 36})
	m = next.(Model)
	return settle(t, m, m.cmdSearch(m.jqlString())), as
}

func TestAutomationList(t *testing.T) {
	m, _ := demoAuto(t)
	m = press(t, m, "x")
	out := view(m)
	for _, want := range []string{"Automatisations — PAY-142", "Ajouter un commentaire interne de suivi", "Escalader au support N2", "(saisies)", "saisie USER non gérée"} {
		if !strings.Contains(out, want) {
			t.Errorf("list misses %q", want)
		}
	}
	if m.mode != modeAutoList {
		t.Fatalf("mode = %v", m.mode)
	}
	// reachable from the board and the detail, on the focused ticket
	for _, seq := range [][]string{{"2"}, {"1", "enter"}} {
		mm, _ := demoAuto(t)
		for _, k := range seq {
			mm = press(t, mm, k)
		}
		if mm = press(t, mm, "x"); mm.mode != modeAutoList {
			t.Errorf("x should work after %v (mode %v)", seq, mm.mode)
		}
	}
}

func TestAutomationRunNeedsConfirmation(t *testing.T) {
	m, as := demoAuto(t)
	m = press(t, m, "x")
	m = press(t, m, "enter") // 1st rule, no input → confirmation
	if m.mode != modeAutoConfirm || !strings.Contains(view(m), "Lancer cette automatisation ?") {
		t.Fatalf("mode = %v", m.mode)
	}
	if len(as.Runs) != 0 {
		t.Fatal("nothing must run before the confirmation")
	}
	if m = press(t, m, "esc"); m.mode != modeAutoList || len(as.Runs) != 0 {
		t.Fatal("esc should go back without running")
	}
	m = press(t, m, "enter")
	m = press(t, m, "enter") // confirm
	if len(as.Runs) != 1 || as.Runs[0].RuleID != "rule-comment" || as.Runs[0].IssueID != "10000" {
		t.Fatalf("runs = %+v", as.Runs)
	}
	if !strings.Contains(m.status, "lancée sur PAY-142") || m.isErr {
		t.Errorf("status = %q", m.status)
	}
}

func TestAutomationInputs(t *testing.T) {
	m, as := demoAuto(t)
	m = press(t, m, "x")
	m = press(t, m, "down")
	m = press(t, m, "enter") // "Escalader" has two inputs
	if m.mode != modeAutoInputs || !strings.Contains(view(m), "Urgence (1-5) (nombre)") {
		t.Fatalf("mode = %v", m.mode)
	}
	m = typeText(t, m, "client en colère")
	m = press(t, m, "enter") // → 2nd field, prefilled with the default 3
	for range "3" {
		m = press(t, m, "backspace")
	}
	m = typeText(t, m, "abc")
	m = press(t, m, "enter")
	if m.mode != modeAutoInputs || !strings.Contains(view(m), "doit être un nombre") || len(as.Runs) != 0 {
		t.Fatalf("a bad NUMBER must be refused: mode=%v err=%q", m.mode, m.autoErr)
	}
	for range "abc" {
		m = press(t, m, "backspace")
	}
	m = typeText(t, m, "5")
	m = press(t, m, "enter")
	if m.mode != modeAutoConfirm || !strings.Contains(view(m), "client en colère") {
		t.Fatalf("expected the confirmation with the values, mode=%v", m.mode)
	}
	m = press(t, m, "enter")
	if len(as.Runs) != 1 || as.Runs[0].Values["Motif"] != "client en colère" || as.Runs[0].Values["Urgence (1-5)"] != "5" {
		t.Fatalf("runs = %+v", as.Runs)
	}
	// the demo escalation moved the ticket: the delayed reload picks it up
	m = settle(t, m, func() tea.Msg { return reloadTickMsg{} })
	if m.issues[0].Fields.Status.Name != "En revue" {
		t.Errorf("status after reload = %q", m.issues[0].Fields.Status.Name)
	}
	if !strings.Contains(m.status, "lancée") {
		t.Errorf("the confirmation must survive the reload: %q", m.status)
	}
}

func TestAutomationUnsupportedInputIsNeverRun(t *testing.T) {
	m, as := demoAuto(t)
	m = press(t, m, "x")
	m = press(t, press(t, m, "down"), "down")
	m = press(t, m, "enter") // "Assigner" needs a USER input
	if m.mode != modeNormal || !m.isErr || !strings.Contains(m.status, "non gérée") || len(as.Runs) != 0 {
		t.Fatalf("mode=%v status=%q runs=%d", m.mode, m.status, len(as.Runs))
	}
}

type failingAuto struct{}

func (failingAuto) List(string) ([]automation.Rule, error) { return nil, errors.New("400 bad request") }
func (failingAuto) Run(automation.Rule, string, map[string]string) error {
	return automation.ErrNotApplicable
}

func TestAutomationSearchFailure(t *testing.T) {
	cfg := config.Config{JQL: config.DefaultJQL}
	extra := []config.Automation{{Name: "Escalader (config)", ID: "r-1"}}
	m := New(mock.New(), cfg).WithAutomations(failingAuto{}, extra)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 130, Height: 36})
	m = settle(t, next.(Model), next.(Model).cmdSearch(config.DefaultJQL))

	// fallback: the configured rules are offered, with the reason
	m = press(t, m, "x")
	out := view(m)
	if m.mode != modeAutoList || !strings.Contains(out, "Escalader (config)") || !strings.Contains(out, "recherche automatique indisponible : 400 bad request") {
		t.Fatalf("fallback list: mode=%v\n%s", m.mode, out)
	}
	// an error from Jira at run time is shown readably
	m = press(t, press(t, m, "enter"), "enter")
	if !m.isErr || !strings.Contains(m.status, "règle non applicable à ce ticket") {
		t.Errorf("status = %q", m.status)
	}

	// no configured rules + search failure: no empty window, just the error
	m2 := New(mock.New(), cfg).WithAutomations(failingAuto{}, nil)
	next, _ = m2.Update(tea.WindowSizeMsg{Width: 130, Height: 36})
	m2 = settle(t, next.(Model), next.(Model).cmdSearch(config.DefaultJQL))
	m2 = press(t, m2, "x")
	if m2.mode != modeNormal || !m2.isErr || !strings.Contains(m2.status, "400 bad request") {
		t.Errorf("mode=%v status=%q", m2.mode, m2.status)
	}
}

func TestAutomationUnavailable(t *testing.T) {
	m := press(t, demo(t, 130, 36), "x")
	if m.mode != modeNormal || !m.isErr || !strings.Contains(m.status, "indisponibles") {
		t.Errorf("mode=%v status=%q", m.mode, m.status)
	}
}
