package ui

import (
	"fmt"
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/kevin/jira-tui/internal/i18n"
	"github.com/kevin/jira-tui/internal/jira"
)

type tableCol struct {
	name  string
	title string
	w     int // 0 = flexible (summary)
}

// tableCols drops the least useful columns until the summary has room.
func tableCols(inner int) []tableCol {
	all := []tableCol{
		{"key", i18n.T("CLÉ", "KEY"), 10}, {"type", "TYPE", 7}, {"summary", i18n.T("RÉSUMÉ", "SUMMARY"), 0}, {"status", i18n.T("STATUT", "STATUS"), 13},
		{"prio", i18n.T("PRIORITÉ", "PRIORITY"), 11}, {"who", i18n.T("ASSIGNÉ", "ASSIGNEE"), 12}, {"upd", i18n.T("MAJ", "UPDATED"), 13},
	}
	drop := []string{"type", "upd", "who", "prio"}
	for {
		fixed, n := 2, 0 // row padding
		for _, c := range all {
			fixed += c.w
			n++
		}
		if inner-fixed-2*(n-1) >= 14 || len(drop) == 0 {
			for i := range all {
				if all[i].w == 0 {
					all[i].w = max(inner-fixed-2*(n-1), 6)
				}
			}
			return all
		}
		for i, c := range all {
			if c.name == drop[0] {
				all = append(all[:i], all[i+1:]...)
				break
			}
		}
		drop = drop[1:]
	}
}

func (m Model) viewList() string {
	vis := m.visible()
	bodyH := m.bodyH()

	// --- filter bar --- (compact chips, then no counter, when the width is short)
	chip := func(on bool, onText, offText string) string {
		label, c := offText, colSub
		if on {
			label, c = onText, colLav
		}
		return boxStyle(colBorder).Render(txt(c, label))
	}
	count := i18n.T("résultats", "results")
	if len(vis) == 1 {
		count = i18n.T("résultat", "result")
	}
	build := func(compact bool) (chips, results string) {
		switch {
		case m.rawJQL != "":
			chips = boxStyle(colBorder).Render(txt(colLav, i18n.T("requête JQL", "JQL query")))
		case compact:
			chips = lipgloss.JoinHorizontal(lipgloss.Center,
				chip(m.onlyMine, i18n.T("moi", "me"), i18n.T("tous", "all")), " ", chip(m.onlyOpen, i18n.T("ouverts", "open"), i18n.T("tous", "all")))
		default:
			chips = lipgloss.JoinHorizontal(lipgloss.Center,
				chip(m.onlyMine, i18n.T("assigné : moi", "assignee: me"), i18n.T("assigné : tous", "assignee: all")), " ",
				chip(m.onlyOpen, i18n.T("statut : ouverts", "status: open"), i18n.T("statut : tous", "status: all")))
		}
		if !compact {
			results = txt(colSub, fmt.Sprintf(" %d %s", len(vis), count))
		}
		return
	}
	chips, results := build(false)
	fw := m.w - lipgloss.Width(chips) - lipgloss.Width(results) - 2
	if fw < 20 {
		chips, results = build(true)
		fw = m.w - lipgloss.Width(chips) - lipgloss.Width(results) - 2
	}
	fw = max(fw, 12)

	var input string
	switch {
	case m.mode == modeFilter:
		input = m.filter.View()
	case m.filter.Value() != "":
		input = txt(colFg, m.filter.Value())
	default:
		input = txt(colSub, i18n.T("Filtrer les tickets…", "Filter tickets…"))
	}
	border := colBorder
	if m.mode == modeFilter {
		border = colPurple
	}
	bar := lipgloss.JoinHorizontal(lipgloss.Center,
		boxStyle(border).Width(fw).Render(bold(colPink, "/")+" "+input), " ", chips, results)

	// --- table ---
	tableH := bodyH - lipgloss.Height(bar)
	inner := m.w - 2
	cols := tableCols(inner)
	rows := max(tableH-2-2, 1) // borders, header + separator

	head := make([]string, len(cols))
	for i, c := range cols {
		head[i] = padRight(bold(colSub, c.title), c.w, paint{})
	}
	lines := []string{" " + strings.Join(head, "  ") + " ", txt(colBorder, strings.Repeat("─", inner))}

	if len(vis) == 0 && m.loading == 0 {
		lines = append(lines, "", " "+txt(colSub, i18n.T("Aucun ticket pour cette requête.", "No ticket for this query.")))
	}
	start := 0
	if m.cursor >= rows {
		start = m.cursor - rows + 1
	}
	for i := start; i < len(vis) && i < start+rows; i++ {
		lines = append(lines, listRow(vis[i], cols, i == m.cursor))
	}
	table := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colBorder).
		Width(m.w).Height(tableH).Render(strings.Join(lines, "\n"))

	return bar + "\n" + table
}

func listRow(is jira.Issue, cols []tableCol, selected bool) string {
	f := is.Fields
	p := paint{}
	pick := func(c color.Color) color.Color { return c }
	if selected {
		p = paint{bg: colPurple}
		pick = func(color.Color) color.Color { return colWhite }
	}
	cells := make([]string, len(cols))
	for i, c := range cols {
		var s string
		switch c.name {
		case "key":
			s = p.s(pick(colPink), true).Render(is.Key)
		case "type":
			s = p.s(pick(colSub), false).Render(f.IssueType.Name)
		case "summary":
			s = p.s(pick(colFg), false).Render(f.Summary)
		case "status":
			label, col := statusLabel(f.Status)
			s = p.s(pick(col), false).Render(label)
		case "prio":
			label, col := priorityInfo(f.Priority)
			s = p.s(pick(col), false).Render(label)
		case "who":
			s = p.s(pick(colSub), false).Render(assigneeName(f.Assignee))
		case "upd":
			s = p.s(pick(colSub), false).Render(agoFull(f.Updated))
		}
		cells[i] = padRight(s, c.w, p)
	}
	return p.space(1) + strings.Join(cells, p.space(2)) + p.space(1)
}
