package ui

import (
	"fmt"
	"image/color"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/kevin/jira-tui/internal/i18n"
	"github.com/kevin/jira-tui/internal/jira"
)

// Palette taken from the "Jira TUI" mockup.
var (
	colBg      = lipgloss.Color("#15121f")
	colFg      = lipgloss.Color("#e6e1f0")
	colSub     = lipgloss.Color("#9d96b8")
	colPurple  = lipgloss.Color("#7D56F4")
	colPink    = lipgloss.Color("#FF5F87")
	colYellow  = lipgloss.Color("#ECFD66")
	colBlue    = lipgloss.Color("#6CB6FF")
	colGreen   = lipgloss.Color("#04B575")
	colBorder  = lipgloss.Color("#3a3358")
	colLav     = lipgloss.Color("#c4b5fd")
	colCardSel = lipgloss.Color("#2a2150")
	colInput   = lipgloss.Color("#1d1830")
	colWhite   = lipgloss.Color("#ffffff")
	colDark    = lipgloss.Color("#15121f")
)

// paint builds styles that all carry the same background (nil = none), so that
// a highlighted row/card keeps its background across differently colored segments.
type paint struct{ bg color.Color }

func (p paint) s(fg color.Color, bold bool) lipgloss.Style {
	st := lipgloss.NewStyle().Foreground(fg).Bold(bold)
	if p.bg != nil {
		st = st.Background(p.bg)
	}
	return st
}

func (p paint) space(n int) string {
	if n <= 0 {
		return ""
	}
	st := lipgloss.NewStyle()
	if p.bg != nil {
		st = st.Background(p.bg)
	}
	return st.Render(strings.Repeat(" ", n))
}

func txt(c color.Color, s string) string { return lipgloss.NewStyle().Foreground(c).Render(s) }
func bold(c color.Color, s string) string {
	return lipgloss.NewStyle().Foreground(c).Bold(true).Render(s)
}

func boxStyle(border color.Color) lipgloss.Style {
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(border).Padding(0, 1)
}

func badge(text string, bg color.Color) string {
	return lipgloss.NewStyle().Foreground(colDark).Background(bg).Bold(true).Padding(0, 1).Render(text)
}

// statusKind returns the glyph and color of a status (○ à faire, ◐ en cours, ◑ en revue, ✓ terminé).
func statusKind(s jira.Status) (string, color.Color) {
	n := strings.ToLower(s.Name)
	cat := s.Category.Key
	if cat == "" { // column names carry no category: infer it
		switch {
		case hasAny(n, "termin", "done", "fini", "clos", "résolu", "resolu"):
			cat = "done"
		case hasAny(n, "cours", "progress", "doing", "rev", "test", "valid", "dev"):
			cat = "indeterminate"
		}
	}
	switch cat {
	case "done":
		return "✓", colGreen
	case "indeterminate":
		if hasAny(n, "rev", "test", "valid") {
			return "◑", colBlue
		}
		return "◐", colYellow
	}
	return "○", colSub
}

func statusLabel(s jira.Status) (string, color.Color) {
	g, c := statusKind(s)
	return g + " " + s.Name, c
}

func hasAny(s string, subs ...string) bool {
	for _, x := range subs {
		if strings.Contains(s, x) {
			return true
		}
	}
	return false
}

// priorityInfo maps Jira priorities to the mockup's three levels.
func priorityInfo(p *jira.Named) (string, color.Color) {
	if p == nil {
		return "—", colSub
	}
	n := strings.ToLower(p.Name)
	switch {
	case hasAny(n, "highest", "high", "blocker", "critical", "major", "haute", "élevée", "critique"):
		return i18n.T("▲▲ Haute", "▲▲ High"), colPink
	case hasAny(n, "medium", "normal", "moyenne"):
		return i18n.T("▲ Moyenne", "▲ Medium"), colYellow
	case hasAny(n, "low", "minor", "trivial", "basse", "faible"):
		return i18n.T("▽ Basse", "▽ Low"), colSub
	}
	return "▲ " + p.Name, colYellow
}

func parseJira(raw string) (time.Time, bool) {
	for _, layout := range []string{"2006-01-02T15:04:05.000-0700", time.RFC3339} {
		if t, err := time.Parse(layout, raw); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// agoFull formats a Jira timestamp as "il y a 12 min", "hier"…
func agoFull(raw string) string {
	t, ok := parseJira(raw)
	if !ok {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return i18n.T("à l'instant", "just now")
	case d < time.Hour:
		return i18n.F("il y a %d min", "%d min ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return i18n.F("il y a %d h", "%d h ago", int(d.Hours()))
	case d < 48*time.Hour:
		return i18n.T("hier", "yesterday")
	case d < 30*24*time.Hour:
		return i18n.F("il y a %d j", "%d d ago", int(d.Hours()/24))
	}
	return t.Format(i18n.T("02/01/2006", "2006-01-02"))
}

var (
	monthsFR = [...]string{"janv.", "févr.", "mars", "avr.", "mai", "juin", "juil.", "août", "sept.", "oct.", "nov.", "déc."}
	monthsEN = [...]string{"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}
)

func monthName(m time.Month) string {
	if i18n.Current() == i18n.EN {
		return monthsEN[m-1]
	}
	return monthsFR[m-1]
}

// createdFmt formats "04 nov. 2026, 09:12".
func createdFmt(raw string) string {
	t, ok := parseJira(raw)
	if !ok {
		return raw
	}
	t = t.Local()
	return fmt.Sprintf("%02d %s %d, %02d:%02d", t.Day(), monthName(t.Month()), t.Year(), t.Hour(), t.Minute())
}

func assigneeName(u *jira.User) string {
	if u == nil {
		return i18n.T("Non assigné", "Unassigned")
	}
	return u.DisplayName
}

func initials(u *jira.User) string {
	if u == nil {
		return "—"
	}
	parts := strings.Fields(u.DisplayName)
	switch len(parts) {
	case 0:
		return "—"
	case 1:
		r := []rune(strings.ToUpper(parts[0]))
		return string(r[:min(len(r), 2)])
	}
	return strings.ToUpper(string([]rune(parts[0])[:1]) + string([]rune(parts[1])[:1]))
}

var authorColors = []color.Color{colPink, colBlue, colGreen, colYellow, colLav}

func authorColor(name string) color.Color {
	h := 0
	for _, r := range name {
		h = h*31 + int(r)
	}
	if h < 0 {
		h = -h
	}
	return authorColors[h%len(authorColors)]
}

// truncate shortens s to n cells (ANSI-safe) with an ellipsis.
func truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	return ansi.Truncate(s, n, "…")
}

// padRight truncates then pads s to exactly w cells.
func padRight(s string, w int, p paint) string {
	s = truncate(s, w)
	return s + p.space(w-lipgloss.Width(s))
}

// spread puts left and right at both ends of a w-cell line.
func spread(left, right string, w int, p paint) string {
	gap := w - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		return truncate(left, max(w-lipgloss.Width(right)-1, 1)) + p.space(1) + right
	}
	return left + p.space(gap) + right
}

// wrapLines wraps s to w cells and keeps at most n lines (last one ellipsized).
func wrapLines(s string, w, n int) []string {
	lines := strings.Split(ansi.Wrap(s, max(w, 1), ""), "\n")
	if len(lines) > n {
		lines = lines[:n]
		lines[n-1] = ansi.Truncate(lines[n-1], max(w-1, 1), "") + "…"
	}
	return lines
}

// fit forces s to exactly h lines.
func fit(s string, h int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > h {
		lines = lines[:h]
	}
	for len(lines) < h {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}
