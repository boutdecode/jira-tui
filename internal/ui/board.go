package ui

import (
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/kevin/jira-tui/internal/i18n"
	"github.com/kevin/jira-tui/internal/jira"
)

// columnIssues returns the board issues whose status maps to column c.
func (m Model) columnIssues(c int) []jira.Issue {
	if c < 0 || c >= len(m.cols) {
		return nil
	}
	var out []jira.Issue
	for _, is := range m.bissues {
		if slices.Contains(m.cols[c].StatusID, is.Fields.Status.ID) {
			out = append(out, is)
		}
	}
	return out
}

func (m Model) keyBoard(k string) (tea.Model, tea.Cmd) {
	n := len(m.cols)
	if n == 0 {
		return m, nil
	}
	clampRow := func() { m.br = min(m.br, max(len(m.columnIssues(m.bc))-1, 0)) }
	switch k {
	case "left", "h":
		m.bc = max(m.bc-1, 0)
		clampRow()
	case "right", "l":
		m.bc = min(m.bc+1, n-1)
		clampRow()
	case "up", "k":
		m.br = max(m.br-1, 0)
	case "down", "j":
		m.br = min(m.br+1, max(len(m.columnIssues(m.bc))-1, 0))
	case "r":
		return m, m.busy(m.cmdBoard(m.board))
	case "enter":
		if is := m.columnIssues(m.bc); m.br < len(is) {
			return m, m.openDetail(is[m.br])
		}
	case "H", "L":
		key := m.currentKey()
		target := m.bc - 1
		if k == "L" {
			target = m.bc + 1
		}
		if key == "" || target < 0 || target >= n {
			return m, nil
		}
		m.bc = target // follow the card
		m.br = 0
		return m, m.busy(m.cmdTransitions(key, m.cols[target].StatusID))
	}
	return m, nil
}

const cardH = 6 // border + key line + 2 summary lines + footer line + border

func (m Model) viewBoard() string {
	n := len(m.cols)
	if n == 0 {
		return txt(colSub, i18n.T("Aucun board chargé — appuyez sur b pour en choisir un.", "No board loaded — press b to choose one."))
	}
	h := m.bodyH()
	avail := m.w - (n - 1)
	var rendered []string
	for c, col := range m.cols {
		cw := avail / n
		if c < avail%n {
			cw++
		}
		rendered = append(rendered, m.renderColumn(c, col, cw, h))
	}
	parts := make([]string, 0, 2*n)
	for i, r := range rendered {
		if i > 0 {
			parts = append(parts, " ")
		}
		parts = append(parts, r)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, parts...)
}

func (m Model) renderColumn(c int, col jira.Column, cw, h int) string {
	issues := m.columnIssues(c)
	focus := c == m.bc
	inner := max(cw-4, 8) // border + padding

	status := jira.Status{Name: col.Name}
	if len(issues) > 0 {
		status = issues[0].Fields.Status
	}
	glyph, gc := statusKind(status)
	title := colFg
	if focus {
		title = colWhite
	}
	head := spread(
		bold(gc, glyph)+" "+bold(title, strings.ToUpper(truncate(col.Name, inner-6))),
		txt(colSub, fmt.Sprintf("%d", len(issues))), inner, paint{})
	lines := []string{head, txt(colBorder, strings.Repeat("─", inner))}

	avail := h - 2 - 2 // borders, header + separator
	perPage := max((avail-1)/cardH, 1)
	start := 0
	if focus && m.br >= perPage {
		start = m.br - perPage + 1
	}
	for i := start; i < len(issues) && i < start+perPage; i++ {
		lines = append(lines, renderCard(issues[i], inner, focus && i == m.br))
	}
	if len(issues) == 0 {
		lines = append(lines, txt(colSub, "—"))
	}
	if more := len(issues) - (start + perPage); more > 0 {
		lines = append(lines, lipgloss.PlaceHorizontal(inner, lipgloss.Center, txt(colSub, i18n.F("↓ %d de plus", "↓ %d more", more))))
	}

	border := colBorder
	if focus {
		border = colPurple
	}
	return boxStyle(border).Width(cw).Height(h).Render(strings.Join(lines, "\n"))
}

func renderCard(is jira.Issue, w int, selected bool) string {
	f := is.Fields
	p, border := paint{}, colBorder
	if selected {
		p, border = paint{bg: colCardSel}, colPurple
	}
	tw := max(w-4, 6) // card border + padding

	l1 := p.s(colPink, true).Render(is.Key) + p.space(2) + p.s(colSub, false).Render(f.IssueType.Name)
	sum := wrapLines(f.Summary, tw, 2)
	for len(sum) < 2 {
		sum = append(sum, "")
	}
	for i := range sum {
		sum[i] = padRight(p.s(colFg, false).Render(sum[i]), tw, p)
	}
	label, pc := priorityInfo(f.Priority)
	l3 := spread(p.s(pc, false).Render(label), p.s(colLav, false).Render(initials(f.Assignee)), tw, p)

	body := padRight(l1, tw, p) + "\n" + strings.Join(sum, "\n") + "\n" + l3
	st := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(border).Padding(0, 1).Width(w)
	if selected {
		st = st.Background(colCardSel).BorderBackground(colCardSel)
	}
	return st.Render(body)
}
