package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/kevin/jira-tui/internal/config"
	"github.com/kevin/jira-tui/internal/i18n"
	"github.com/kevin/jira-tui/internal/jira"
	"github.com/kevin/jira-tui/internal/mock"
)

// english switches the app to English for the duration of the test.
func english(t *testing.T) {
	t.Helper()
	i18n.Set(i18n.EN)
	t.Cleanup(func() { i18n.Set(i18n.FR) })
}

// frenchWords must never show up on an English screen.
var frenchWords = []string{
	"Liste", "Détail", "Commentaire", "Assigné", "Rapporteur", "chargement", "annuler", "valider", "retour",
	"quitter", "aide", "naviguer", "Aucun", "résultat", "Raccourcis", "Lancer", "règle", "saisie", "Étiquettes",
	"Temps passé", "Créé", "Mis à jour", "ouvrir", "filtrer", "rafraîchir", "statut", "tous", "ouverts", "Filtres",
	"À faire", "En cours", "En revue", "Terminé", "Tâche", "Haute", "Moyenne", "Basse", "il y a", "Non assigné",
}

// hintsOf returns the footer shortcuts of the current mode as plain text.
func hintsOf(m Model) string {
	var parts []string
	for _, line := range m.hintLines() {
		parts = append(parts, ansi.Strip(strings.Join(line, " · ")))
	}
	return strings.Join(parts, " | ")
}

func noFrench(t *testing.T, screen, out string) {
	t.Helper()
	for _, w := range frenchWords {
		if strings.Contains(out, w) {
			t.Errorf("%s: French %q on an English screen\n%s", screen, w, out)
		}
	}
}

func demoEN(t *testing.T) (Model, *mock.Automations) {
	t.Helper()
	english(t)
	ms := mock.New()
	as := mock.NewAutomations(ms)
	m := New(ms, config.Config{JQL: config.DefaultJQL, Filters: []config.Filter{{Name: "My bugs", JQL: "type = Bug"}}}).
		WithTime(mock.NewTime(), 8).WithAutomations(as, nil)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	m = next.(Model)
	return settle(t, m, m.cmdSearch(m.jqlString())), as
}

func TestEnglishScreens(t *testing.T) {
	m, as := demoEN(t)

	// list
	out := view(m)
	for _, want := range []string{"1 List", "3 Detail", "KEY", "SUMMARY", "STATUS", "PRIORITY", "ASSIGNEE", "UPDATED",
		"assignee: me", "status: open", "results", "Filter tickets…", "In Progress", "Payment fails with a 3DS2 card", "▲▲ High", "12 min ago",
		"enter open", "/ filter", "q quit", "? help", "m all assignees", "O all statuses", "x automation"} {
		if !strings.Contains(out, want) {
			t.Errorf("list misses %q", want)
		}
	}
	noFrench(t, "list", out)

	// kanban
	kb := press(t, m, "2")
	noFrench(t, "board", view(kb))
	if !strings.Contains(view(kb), "H/L move ticket") {
		t.Errorf("board hints:\n%s", view(kb))
	}

	// detail with time and comments
	d := press(t, m, "enter")
	out = view(d)
	for _, want := range []string{"Description", "Time spent", "Comments (3)", "New comment", "Details", "Assignee", "Reporter", "Labels", "Component", "Estimate", "Created", "Updated",
		"[internal]", "[customer]", "ago", "02 ", "scroll", "comment", "add time", "change status", "assign to me", "unassign", "open in Jira", "reload"} {
		if !strings.Contains(out, want) {
			t.Errorf("detail misses %q\n%s", want, out)
		}
	}
	noFrench(t, "detail", out)

	// comment mode: visibility labels
	c := press(t, d, "c")
	out = view(c)
	for _, want := range []string{"Customer", "visible to the customer", "customer/internal", "send", "cancel"} {
		if !strings.Contains(out, want) {
			t.Errorf("comment misses %q", want)
		}
	}
	noFrench(t, "comment", out)
	out = view(press(t, c, "tab"))
	if !strings.Contains(out, "not visible to the customer") {
		t.Errorf("internal label missing:\n%s", out)
	}

	if h := hintsOf(l0(t, d)); !strings.Contains(h, "next field") {
		t.Errorf("log hints: %s", h)
	}

	// posting confirms in English
	c = typeText(t, c, "hi")
	c = press(t, c, "ctrl+s")
	if !strings.Contains(view(c), "customer comment added to PAY-142") {
		t.Errorf("comment confirmation:\n%s", view(c))
	}

	// log time modal
	l := press(t, d, "t")
	out = view(l)
	for _, want := range []string{"Add time to PAY-142", "Duration", "Description", "E.g. 1h30", "1 d = 8 h"} {
		if !strings.Contains(out, want) {
			t.Errorf("log modal misses %q", want)
		}
	}
	noFrench(t, "log", out)
	l = typeText(t, l, "1h30")
	if out := view(l); !strings.Contains(out, "= 1 h 30 min on ") {
		t.Errorf("preview:\n%s", out)
	}

	// status modal, JQL modal, filters, help
	for name, key := range map[string]string{"status": "s", "jql": "ctrl+j", "help": "?"} {
		var mm Model
		if key == "ctrl+j" {
			next, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: 'j', Mod: tea.ModCtrl}))
			mm = settle(t, next.(Model), cmd)
		} else {
			mm = press(t, m, key)
		}
		noFrench(t, name, view(mm))
	}
	if out := view(press(t, m, "?")); !strings.Contains(out, "Shortcuts") || !strings.Contains(out, "any key to close") {
		t.Errorf("help:\n%s", out)
	}
	if out := view(press(t, m, "s")); !strings.Contains(out, "Change the status of PAY-142") {
		t.Errorf("status modal:\n%s", out)
	}

	// automations
	a := press(t, m, "x")
	if h := hintsOf(a); !strings.Contains(h, "choose") || !strings.Contains(h, "select") || !strings.Contains(h, "close") {
		t.Errorf("automation hints: %s", h)
	}
	noFrench(t, "automation hints", hintsOf(a))
	out = view(a)
	for _, want := range []string{"Automations — PAY-142", "Add an internal follow-up comment", "Escalate to L2 support", "(inputs)", "input USER not supported"} {
		if !strings.Contains(out, want) {
			t.Errorf("automations misses %q\n%s", want, out)
		}
	}
	noFrench(t, "automations", out)
	a = press(t, a, "down")
	a = press(t, a, "enter")
	out = view(a)
	if !strings.Contains(out, "Urgency (1-5) (number)") || !strings.Contains(out, "Reason") {
		t.Errorf("inputs form:\n%s", out)
	}
	a = typeText(t, a, "slow")
	a = press(t, a, "tab")
	a = press(t, a, "enter")
	out = view(a)
	for _, want := range []string{"Run this automation?", "Escalate to L2 support", " on ", "The rule may modify the ticket or write to the customer."} {
		if !strings.Contains(out, want) {
			t.Errorf("confirm misses %q\n%s", want, out)
		}
	}
	noFrench(t, "confirm", out)
	a = press(t, a, "enter")
	if len(as.Runs) != 1 || !strings.Contains(view(a), "started on PAY-142") {
		t.Errorf("run: %+v\n%s", as.Runs, view(a))
	}
}

func TestEnglishNoTempoAndEmptyFilter(t *testing.T) {
	english(t)
	m := New(mock.New(), config.Config{JQL: config.DefaultJQL})
	next, _ := m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	m = settle(t, next.(Model), nil)
	m = settle(t, m, m.cmdSearch(m.jqlString()))
	d := press(t, m, "enter")
	if out := view(d); !strings.Contains(out, "Tempo not configured — add tempo_token to config.yaml (or TEMPO_TOKEN).") {
		t.Errorf("no tempo:\n%s", out)
	}
	d = press(t, d, "t")
	if out := view(d); !strings.Contains(out, "Tempo not configured") {
		t.Errorf("t without tempo:\n%s", out)
	}
	// no result
	l := press(t, m, "/")
	l = typeText(t, l, "zzzzzz")
	if out := view(l); !strings.Contains(out, "No ticket for this query.") {
		t.Errorf("empty:\n%s", out)
	}
}

func TestEnglishRelativeDatesAndPriorities(t *testing.T) {
	english(t)
	if got := monthName(11); got != "Nov" {
		t.Errorf("month = %q", got)
	}
	for in, want := range map[string]string{"High": "▲▲ High", "Medium": "▲ Medium", "Low": "▽ Low", "Haute": "▲▲ High", "Moyenne": "▲ Medium"} {
		if got, _ := priorityInfo(&jira.Named{Name: in}); got != want {
			t.Errorf("priority %s = %q, want %q", in, got, want)
		}
	}
	if got := assigneeName(nil); got != "Unassigned" {
		t.Errorf("assignee = %q", got)
	}
	if got := longDate("2026-11-04"); got != "2026-11-04" {
		t.Errorf("longDate = %q", got)
	}
	if got := shortDate("2026-11-04"); got != "04 Nov" {
		t.Errorf("shortDate = %q", got)
	}
	i18n.Set(i18n.FR)
	if got := longDate("2026-11-04"); got != "04/11/2026" {
		t.Errorf("longDate FR = %q", got)
	}
	if got := monthName(11); got != "nov." {
		t.Errorf("month FR = %q", got)
	}
}

func TestEnglishSetupForm(t *testing.T) {
	english(t)
	s := NewSetup(config.Config{}, "/tmp/x/config.yaml", func(string, string, string) (string, error) { return "", nil })
	next, _ := s.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	out := ansi.Strip(next.(Setup).View().Content)
	for _, want := range []string{"First-time setup", "No configuration found", "Jira site URL", "Atlassian account email", "API token", "Create one at", "next field", "confirm", "cancel"} {
		if !strings.Contains(out, want) {
			t.Errorf("setup misses %q\n%s", want, out)
		}
	}
	noFrench(t, "setup", out)
	s2, _ := next.(Setup).submit()
	if got := s2.(Setup).errMsg; got != "all fields are required" {
		t.Errorf("errMsg = %q", got)
	}
}

// l0 opens the "add time" modal.
func l0(t *testing.T, d Model) Model { return press(t, d, "t") }
