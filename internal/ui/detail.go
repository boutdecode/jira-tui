package ui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/kevin/jira-tui/internal/i18n"
	"github.com/kevin/jira-tui/internal/jira"
	"github.com/kevin/jira-tui/internal/tempo"
)

func (m Model) viewDetail() string {
	is := m.detail
	f := is.Fields
	areaH := m.bodyH() - 2
	leftW, rightW := m.detailWidths()

	// title + badges
	label, sc := statusLabel(f.Status)
	prio, pc := priorityInfo(f.Priority)
	title := bold(colWhite, truncate(f.Summary, m.w))
	badges := badge(f.IssueType.Name, colPink) + " " + txt(sc, "[ "+label+" ]") + " " + txt(pc, prio)

	// left: scrollable panel + comment input
	panelH := max(areaH-6, 4)
	panelContent := m.vp.View()
	if m.mode == modeComment && m.sg.active {
		panelContent = m.suggestView(max(leftW-4, 10), max(min(suggestWindow, panelH-5), 2)) // the picker takes the place of the text above the comment box
	}
	panel := boxStyle(colBorder).Width(leftW).Height(panelH).Render(panelContent)
	inputBorder, label := colBorder, bold(colLav, i18n.T("Nouveau commentaire", "New comment"))
	if m.mode == modeComment {
		// the visibility must be unmistakable before sending
		if m.commentInternal {
			inputBorder = colYellow
			label += "  " + badge(i18n.T("Interne", "Internal"), colYellow) + " " + txt(colYellow, i18n.T("non visible du client", "not visible to the customer"))
		} else {
			inputBorder = colBlue
			label += "  " + badge(i18n.T("Client", "Customer"), colBlue) + " " + txt(colBlue, i18n.T("visible du client", "visible to the customer"))
		}
	}
	input := boxStyle(inputBorder).Background(colInput).BorderBackground(colInput).Width(leftW).Render(m.comment.View())
	left := lipgloss.JoinVertical(lipgloss.Left, panel, label, input)

	body := left
	if rightW > 0 {
		body = lipgloss.JoinHorizontal(lipgloss.Top, left, " ", m.detailsPanel(rightW, areaH))
	}
	return title + "\n" + badges + "\n" + body
}

func (m Model) detailsPanel(w, h int) string {
	f := m.detail.Fields
	join := func(items []string) string {
		if len(items) == 0 {
			return "—"
		}
		return strings.Join(items, ", ")
	}
	var comps []string
	for _, c := range f.Components {
		comps = append(comps, c.Name)
	}
	reporter := "—"
	if f.Reporter != nil {
		reporter = f.Reporter.DisplayName
	}
	created := "—"
	if f.Created != "" {
		created = createdFmt(f.Created)
	}
	updated := agoFull(f.Updated)
	if updated == "" {
		updated = "—"
	}
	spent, estimate := "—", "—"
	if m.tsvc != nil && m.wlLoaded {
		spent = tempo.FormatDuration(tempo.Total(m.worklogs))
	}
	if f.OriginalEstimate != nil && *f.OriginalEstimate > 0 {
		estimate = tempo.FormatDuration(*f.OriginalEstimate)
	}
	rows := [][2]string{
		{i18n.T("Assigné", "Assignee"), assigneeName(f.Assignee)},
		{i18n.T("Rapporteur", "Reporter"), reporter},
		{i18n.T("Étiquettes", "Labels"), join(f.Labels)},
		{i18n.T("Composant", "Component"), join(comps)},
		{i18n.T("Temps passé", "Time spent"), spent},
		{i18n.T("Estimation", "Estimate"), estimate},
		{i18n.T("Créé", "Created"), created},
		{i18n.T("Mis à jour", "Updated"), updated},
	}
	// blank separators only when they fit (the panel must never outgrow its box)
	sep := 2+len(rows)*3 <= h-2
	lines := []string{bold(colLav, i18n.T("Détails", "Details")), ""}
	for _, r := range rows {
		lines = append(lines, txt(colSub, r[0]))
		for _, l := range strings.Split(ansi.Wrap(r[1], max(w-4, 8), ""), "\n") {
			lines = append(lines, txt(colFg, l))
		}
		if sep {
			lines = append(lines, "")
		}
	}
	return boxStyle(colBorder).Width(w).Height(h).Render(fit(strings.Join(lines, "\n"), max(h-2, 1)))
}

// renderDetail builds the scrollable content: description then comments.
// timeView is the Tempo data needed to render the detail.
type timeView struct {
	enabled, loaded bool
	logs            []tempo.Worklog
	hpd             int
}

func (m Model) timeView() timeView {
	return timeView{enabled: m.tsvc != nil, loaded: m.wlLoaded, logs: m.worklogs, hpd: m.hpd}
}

// timeSection renders "Temps passé": header with the total, then one line per worklog.
func timeSection(tv timeView, estimate *int, width int) string {
	title := i18n.T("Temps passé", "Time spent")
	if tv.enabled && tv.loaded && len(tv.logs) > 0 {
		spent := tempo.FormatDuration(tempo.Total(tv.logs))
		if estimate != nil && *estimate > 0 {
			title += i18n.F(" (%s sur %s estimées)", " (%s of %s estimated)", spent, tempo.FormatDuration(*estimate))
		} else {
			title += " (" + spent + ")"
		}
	}
	out := section(title, width) + "\n"
	switch {
	case !tv.enabled:
		return out + txt(colSub, noTempoMsg()+".") + "\n"
	case !tv.loaded:
		return out + txt(colSub, i18n.T("chargement…", "loading…")) + "\n"
	case len(tv.logs) == 0:
		return out + txt(colSub, i18n.T("Aucun temps saisi — appuyez sur t pour en ajouter.", "No time logged — press t to add some.")) + "\n"
	}
	const maxLines = 12
	descW := width - 7 - 14 - 14 - 6
	for i, w := range tv.logs {
		if i == maxLines {
			out += txt(colSub, i18n.F("… %d de plus", "… %d more", len(tv.logs)-maxLines)) + "\n"
			break
		}
		line := padRight(txt(colSub, shortDate(w.Date)), 7, paint{}) + "  " +
			padRight(txt(authorColor(w.Author), w.Author), 14, paint{}) + "  " +
			padRight(txt(colFg, tempo.FormatDuration(w.Seconds)), 14, paint{})
		if descW >= 8 && w.Description != "" {
			line += "  " + txt(colSub, truncate(w.Description, descW))
		}
		out += line + "\n"
	}
	return out
}

func renderDetail(is jira.Issue, width int, tv timeView) string {
	f := is.Fields
	var sb strings.Builder

	sb.WriteString(section("Description", width) + "\n")
	desc := jira.ADFToText(f.Description)
	if desc == "" {
		desc = i18n.T("(vide)", "(empty)")
	}
	sb.WriteString(txt(colFg, ansi.Wrap(desc, width, "")) + "\n")

	sb.WriteString("\n" + timeSection(tv, f.OriginalEstimate, width))

	cs := f.Comment.Comments
	if is.CommentsErr != "" {
		sb.WriteString("\n" + section(i18n.T("Commentaires", "Comments"), width) + "\n" + txt(colPink, i18n.T("Commentaires indisponibles : ", "Comments unavailable: ")+ansi.Truncate(is.CommentsErr, max(width-30, 10), "…")))
		return strings.TrimRight(sb.String(), "\n")
	}
	sb.WriteString("\n" + section(i18n.F("Commentaires (%d)", "Comments (%d)", len(cs)), width) + "\n")
	bar := txt(colBorder, "│ ")
	for i, c := range cs {
		if i > 0 {
			sb.WriteString("\n")
		}
		when := agoFull(c.RawDate)
		if when == "" {
			when = c.RawDate
		}
		vis := ""
		switch {
		case c.Public == nil: // Jira does not say: show nothing rather than guess
		case *c.Public:
			vis = "  " + txt(colBlue, i18n.T("[client]", "[customer]"))
		default:
			vis = "  " + txt(colYellow, i18n.T("[interne]", "[internal]"))
		}
		sb.WriteString(bar + bold(authorColor(c.Author.DisplayName), c.Author.DisplayName) + "  " + txt(colSub, when) + vis + "\n")
		for _, l := range strings.Split(ansi.Wrap(jira.ADFToText(c.Body), max(width-2, 8), ""), "\n") {
			sb.WriteString(bar + txt(colFg, l) + "\n")
		}
	}
	return strings.TrimRight(sb.String(), "\n")
}

func section(title string, width int) string {
	t := bold(colLav, title)
	return t + " " + txt(colBorder, strings.Repeat("─", max(width-lipgloss.Width(t)-1, 0)))
}
