package ui

import (
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/kevin/jira-tui/internal/i18n"
	"github.com/kevin/jira-tui/internal/tempo"
)

// focusLog moves the focus to field i (wrapping) of the "add time" form.
func (m *Model) focusLog(i int) tea.Cmd {
	m.logFocus = (i + len(m.logIn)) % len(m.logIn)
	var cmd tea.Cmd
	for j := range m.logIn {
		if j == m.logFocus {
			cmd = m.logIn[j].Focus()
		} else {
			m.logIn[j].Blur()
		}
	}
	return cmd
}

func (m *Model) closeLog() {
	for j := range m.logIn {
		m.logIn[j].Blur()
	}
	m.mode = modeNormal
}

func (m Model) keyLog(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.closeLog()
		return m, nil
	case "tab", "down":
		return m, m.focusLog(m.logFocus + 1)
	case "shift+tab", "up":
		return m, m.focusLog(m.logFocus - 1)
	case "enter":
		if m.logFocus < len(m.logIn)-1 {
			return m, m.focusLog(m.logFocus + 1)
		}
		return m.submitLog()
	}
	var cmd tea.Cmd
	m.logIn[m.logFocus], cmd = m.logIn[m.logFocus].Update(msg)
	m.logErr = ""
	return m, cmd
}

func (m Model) submitLog() (tea.Model, tea.Cmd) {
	secs, err := tempo.ParseDuration(m.logIn[0].Value(), m.hpd)
	if err != nil {
		m.logErr = err.Error()
		return m, m.focusLog(0)
	}
	date, err := tempo.ParseDate(m.logIn[1].Value(), time.Now())
	if err != nil {
		m.logErr = err.Error()
		return m, m.focusLog(1)
	}
	desc := strings.TrimSpace(m.logIn[2].Value())
	key, id := m.detail.Key, m.detail.ID
	m.closeLog()
	return m, m.busy(m.cmdLog(id, key, secs, date, desc))
}

func (m Model) logContent() string {
	labels := []string{i18n.T("Durée", "Duration"), "Date", "Description"}
	var sb strings.Builder
	sb.WriteString(bold(colLav, i18n.T("Ajouter du temps à ", "Add time to ")+m.detail.Key) + "\n\n")
	for i, in := range m.logIn {
		border := colBorder
		if i == m.logFocus {
			border = colPurple
		}
		sb.WriteString(txt(colSub, labels[i]) + "\n" + boxStyle(border).Width(54).Render(in.View()) + "\n")
	}

	// live preview of what will be sent
	switch {
	case m.logErr != "":
		sb.WriteString(txt(colPink, "✗ "+truncate(m.logErr, 52)))
	default:
		secs, derr := tempo.ParseDuration(m.logIn[0].Value(), m.hpd)
		date, terr := tempo.ParseDate(m.logIn[1].Value(), time.Now())
		if derr == nil && terr == nil {
			sb.WriteString(txt(colGreen, "= "+tempo.FormatDuration(secs)+i18n.T(" le ", " on ")+longDate(date)))
		} else {
			sb.WriteString(txt(colSub, i18n.T("Ex. 1h30 · 45m · 2d · 1d 2h  (1 j = ", "E.g. 1h30 · 45m · 2d · 1d 2h  (1 d = ")+strconv.Itoa(m.hpd)+" h)"))
		}
	}
	return sb.String()
}

// longDate turns 2026-11-04 into 04/11/2026 (French) or leaves it as ISO (English).
func longDate(d string) string {
	if t, err := time.Parse("2006-01-02", d); err == nil {
		return t.Format(i18n.T("02/01/2006", "2006-01-02"))
	}
	return d
}

// shortDate turns 2026-11-04 into "04 nov.".
func shortDate(d string) string {
	if t, err := time.Parse("2006-01-02", d); err == nil {
		return t.Format("02") + " " + monthName(t.Month())
	}
	return d
}
