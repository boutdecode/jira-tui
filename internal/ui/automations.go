package ui

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/kevin/jira-tui/internal/automation"
	"github.com/kevin/jira-tui/internal/config"
	"github.com/kevin/jira-tui/internal/i18n"
	"github.com/kevin/jira-tui/internal/jira"
)

// AutomationService is the Jira Automation surface the UI depends on (nil = unavailable).
type AutomationService interface {
	List(issueID string) ([]automation.Rule, error)
	Run(rule automation.Rule, issueID string, values map[string]string) error
}

type rulesMsg struct {
	issue jira.Issue
	rules []automation.Rule
	err   error // the search failed: rules then only holds the configured ones
}

type ranMsg struct {
	rule automation.Rule
	key  string
	err  error
}

type reloadTickMsg struct{}

// autoReloadDelay is how long to wait before refreshing: the rule runs asynchronously in Jira.
const autoReloadDelay = 3 * time.Second

// WithAutomations enables the "x" key. extra are rules declared in config.yaml.
func (m Model) WithAutomations(svc AutomationService, extra []config.Automation) Model {
	m.asvc = svc
	m.autoExtra = nil
	for _, a := range extra {
		if a.ID != "" {
			name := a.Name
			if name == "" {
				name = a.ID
			}
			m.autoExtra = append(m.autoExtra, automation.Rule{ID: a.ID, Name: name})
		}
	}
	return m
}

// currentIssue returns the issue under focus in the current view.
func (m Model) currentIssue() (jira.Issue, bool) {
	switch m.view {
	case viewDetail:
		return m.detail, m.hasDetail
	case viewBoard:
		if is := m.columnIssues(m.bc); m.br < len(is) {
			return is[m.br], true
		}
	default:
		if vis := m.visible(); m.cursor < len(vis) {
			return vis[m.cursor], true
		}
	}
	return jira.Issue{}, false
}

func (m Model) cmdRules(is jira.Issue) tea.Cmd {
	svc, extra := m.asvc, m.autoExtra
	return func() tea.Msg {
		found, err := svc.List(is.ID)
		return rulesMsg{issue: is, rules: automation.Merge(found, extra), err: err}
	}
}

func (m Model) cmdRun(rule automation.Rule, is jira.Issue, values map[string]string) tea.Cmd {
	svc := m.asvc
	return func() tea.Msg {
		return ranMsg{rule: rule, key: is.Key, err: svc.Run(rule, is.ID, values)}
	}
}

// startAutomations is bound to the "x" key.
func (m *Model) startAutomations() tea.Cmd {
	if m.asvc == nil {
		m.setErr(errors.New(i18n.T("automatisations indisponibles", "automations unavailable")))
		return nil
	}
	is, ok := m.currentIssue()
	if !ok || is.ID == "" {
		return nil
	}
	return m.busy(m.cmdRules(is))
}

func (m *Model) onRules(msg rulesMsg) {
	m.loading--
	if len(msg.rules) == 0 {
		if msg.err != nil {
			m.setErr(fmt.Errorf(i18n.T("automatisations : %w", "automations: %w"), msg.err))
		} else {
			m.setErr(fmt.Errorf(i18n.T("aucune automatisation manuelle pour %s", "no manual automation for %s"), msg.issue.Key))
		}
		return
	}
	m.autoIssue, m.autoRules, m.autoCur, m.autoNote = msg.issue, msg.rules, 0, ""
	if msg.err != nil {
		m.autoNote = i18n.T("recherche automatique indisponible : ", "automatic search unavailable: ") + msg.err.Error()
	}
	m.mode = modeAutoList
}

func (m *Model) onRan(msg ranMsg) tea.Cmd {
	m.loading--
	if msg.err != nil {
		m.setErr(fmt.Errorf("“%s”: %w", msg.rule.Name, msg.err))
		return nil
	}
	m.setInfo(i18n.F("règle « %s » lancée sur %s (elle s'exécute dans Jira)", "rule “%s” started on %s (it runs in Jira)", msg.rule.Name, msg.key))
	return tea.Tick(autoReloadDelay, func(time.Time) tea.Msg { return reloadTickMsg{} })
}

func (m Model) keyAutoList(k string) (tea.Model, tea.Cmd) {
	switch k {
	case "esc", "q", "x":
		m.mode = modeNormal
	case "up", "k":
		m.autoCur = max(m.autoCur-1, 0)
	case "down", "j":
		m.autoCur = min(m.autoCur+1, len(m.autoRules)-1)
	case "enter":
		r := m.autoRules[m.autoCur]
		if in := r.Unsupported(); in != nil {
			m.setErr(fmt.Errorf(i18n.T("« %s » : saisie « %s » de type %s non gérée — lancez la règle depuis Jira", "“%s”: input “%s” of type %s is not supported — run the rule from Jira"), r.Name, in.Name, in.Type))
			m.mode = modeNormal
			return m, nil
		}
		m.autoRule = r
		if len(r.Inputs) == 0 {
			m.mode, m.autoVals = modeAutoConfirm, nil
			return m, nil
		}
		m.autoIn = make([]textinput.Model, len(r.Inputs))
		for i, in := range r.Inputs {
			ti := textinput.New()
			ti.Prompt = ""
			ti.SetWidth(48)
			ti.SetVirtualCursor(true)
			ti.SetStyles(inputStyles(ti.Styles()))
			ti.SetValue(in.Default)
			m.autoIn[i] = ti
		}
		m.autoErr, m.mode = "", modeAutoInputs
		return m, m.focusAuto(0)
	}
	return m, nil
}

func (m *Model) focusAuto(i int) tea.Cmd {
	m.autoFocus = (i + len(m.autoIn)) % len(m.autoIn)
	var cmd tea.Cmd
	for j := range m.autoIn {
		if j == m.autoFocus {
			cmd = m.autoIn[j].Focus()
		} else {
			m.autoIn[j].Blur()
		}
	}
	return cmd
}

func (m Model) keyAutoInputs(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		for j := range m.autoIn {
			m.autoIn[j].Blur()
		}
		m.mode = modeAutoList
		return m, nil
	case "tab", "down":
		return m, m.focusAuto(m.autoFocus + 1)
	case "shift+tab", "up":
		return m, m.focusAuto(m.autoFocus - 1)
	case "enter":
		if m.autoFocus < len(m.autoIn)-1 {
			return m, m.focusAuto(m.autoFocus + 1)
		}
		vals := map[string]string{}
		for i, in := range m.autoRule.Inputs {
			v := strings.TrimSpace(m.autoIn[i].Value())
			if err := automation.CheckValue(in, v); err != nil {
				m.autoErr = err.Error()
				return m, m.focusAuto(i)
			}
			vals[in.Name] = v
		}
		m.autoVals, m.autoErr = vals, ""
		for j := range m.autoIn {
			m.autoIn[j].Blur()
		}
		m.mode = modeAutoConfirm
		return m, nil
	}
	var cmd tea.Cmd
	m.autoIn[m.autoFocus], cmd = m.autoIn[m.autoFocus].Update(msg)
	m.autoErr = ""
	return m, cmd
}

func (m Model) keyAutoConfirm(k string) (tea.Model, tea.Cmd) {
	switch k {
	case "esc", "n":
		m.mode = modeAutoList
	case "enter", "y":
		m.mode = modeNormal
		return m, m.busy(m.cmdRun(m.autoRule, m.autoIssue, m.autoVals))
	}
	return m, nil
}

func (m Model) autoContent() string {
	var sb strings.Builder
	switch m.mode {
	case modeAutoList:
		sb.WriteString(bold(colLav, i18n.T("Automatisations — ", "Automations — ")+m.autoIssue.Key) + "\n")
		sb.WriteString(txt(colSub, truncate(m.autoIssue.Fields.Summary, 56)) + "\n\n")
		for i, r := range m.autoRules {
			label := r.Name
			if in := r.Unsupported(); in != nil {
				label += txt(colSub, i18n.F("  (saisie %s non gérée)", "  (input %s not supported)", in.Type))
			} else if len(r.Inputs) > 0 {
				label += txt(colSub, i18n.T("  (saisies)", "  (inputs)"))
			}
			if i == m.autoCur {
				sb.WriteString(lipgloss.NewStyle().Background(colPurple).Foreground(colWhite).Render("▸ "+ansi.Strip(label)+" ") + "\n")
			} else {
				sb.WriteString("  " + label + "\n")
			}
		}
		if m.autoNote != "" {
			sb.WriteString("\n" + txt(colSub, truncate(m.autoNote, 60)))
		}
	case modeAutoInputs:
		sb.WriteString(bold(colLav, m.autoRule.Name) + "\n\n")
		for i, in := range m.autoIn {
			border := colBorder
			if i == m.autoFocus {
				border = colPurple
			}
			label := m.autoRule.Inputs[i].Name
			if m.autoRule.Inputs[i].Type == "NUMBER" {
				label += i18n.T(" (nombre)", " (number)")
			}
			sb.WriteString(txt(colSub, label) + "\n" + boxStyle(border).Width(54).Render(in.View()) + "\n")
		}
		if m.autoErr != "" {
			sb.WriteString(txt(colPink, "✗ "+truncate(m.autoErr, 52)))
		}
	case modeAutoConfirm:
		sb.WriteString(bold(colYellow, i18n.T("Lancer cette automatisation ?", "Run this automation?")) + "\n\n")
		sb.WriteString(bold(colWhite, m.autoRule.Name) + txt(colSub, i18n.T("  sur ", "  on ")) + bold(colPink, m.autoIssue.Key) + "\n")
		sb.WriteString(txt(colSub, truncate(m.autoIssue.Fields.Summary, 56)) + "\n")
		for _, in := range m.autoRule.Inputs {
			sb.WriteString("\n" + txt(colSub, in.Name+": ") + txt(colFg, m.autoVals[in.Name]))
		}
		sb.WriteString("\n\n" + txt(colSub, i18n.T("La règle peut modifier le ticket ou écrire au client.", "The rule may modify the ticket or write to the customer.")))
	}
	return strings.TrimRight(sb.String(), "\n")
}
