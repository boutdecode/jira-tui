package ui

import (
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/kevin/jira-tui/internal/i18n"
	"github.com/kevin/jira-tui/internal/jira"
)

// Typing "@" opens a user picker, typing "#" a ticket picker, in every text area
// (new comment, comment edit, description edit). The text stays readable ("@Sofia M.",
// "#PAY-142"); the account ids are kept in a name -> id table that the Markdown
// converter turns into real mentions and smart links when the text is saved.

const (
	suggestMaxQuery = 40
	suggestWindow   = 6
)

// suggest is the state of the picker that follows the cursor.
type suggest struct {
	active bool
	kind   byte   // '@' users, '#' tickets
	query  string // what was typed after the trigger
	users  []jira.User
	issues []jira.IssueRef
	cur    int
	seq    int // identifies the latest search: older answers are dropped
	busy   bool
	err    string
}

type suggestQueryMsg struct{ seq int }
type suggestMsg struct {
	seq    int
	users  []jira.User
	issues []jira.IssueRef
	err    error
}

func (s suggest) count() int {
	if s.kind == '@' {
		return len(s.users)
	}
	return len(s.issues)
}

func (m Model) cmdSuggest(kind byte, query string, seq int) tea.Cmd {
	svc, key := m.svc, m.detail.Key
	return func() tea.Msg {
		if kind == '@' {
			us, err := svc.SearchMentionable(key, query)
			return suggestMsg{seq: seq, users: us, err: err}
		}
		is, err := svc.SearchIssues(query)
		return suggestMsg{seq: seq, issues: is, err: err}
	}
}

func (m *Model) closeSuggest() { m.sg = suggest{seq: m.sg.seq + 1} } // the new seq drops late answers

// mentionTable is the name -> account id table of the text area being edited.
func (m Model) mentionTable() map[string]string {
	if m.mode == modeComment {
		return m.cmtMentions
	}
	return m.ed.mentions
}

// converter reads and writes mentions and smart links for this Jira site.
func (m Model) converter(mentions map[string]string) jira.Converter {
	return jira.Converter{BrowseURL: m.svc.BrowseURL, Mentions: mentions}
}

// markdownOf returns the Markdown to edit for doc and its mentions table; ok is false when
// editing it as Markdown would lose something (images, tables, colors…).
func (m Model) markdownOf(doc any) (string, map[string]string, bool) {
	return m.converter(nil).ToMarkdown(doc)
}

func isPrintable(msg tea.KeyPressMsg) bool {
	return msg.Text != "" && msg.Mod&(tea.ModCtrl|tea.ModAlt) == 0
}

func isWordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }

// triggerOK says whether a "@" or "#" typed now starts a reference: at the start of a line
// or after a space or punctuation, never glued to a word (an e-mail address, "C#").
func triggerOK(ta *textarea.Model) bool {
	lines := strings.Split(ta.Value(), "\n")
	row, col := ta.Line(), ta.Column()
	if row < 0 || row >= len(lines) {
		return true
	}
	rs := []rune(lines[row])
	if col <= 0 || col > len(rs) {
		return col <= 0
	}
	prev := rs[col-1]
	return !isWordRune(prev) && prev != '\\'
}

// suggestKey handles a key typed in a text area. consumed means the key was used by the
// picker and must not reach the text area; the returned command (a delayed search) must be
// run in both cases.
func (m *Model) suggestKey(msg tea.KeyPressMsg, ta *textarea.Model) (consumed bool, cmd tea.Cmd) {
	k := msg.String()
	sg := &m.sg
	if sg.active {
		switch k {
		case "esc": // closes the list only: a second esc leaves the editor
			m.closeSuggest()
			return true, nil
		case "up", "ctrl+p":
			sg.cur = max(sg.cur-1, 0)
			return true, nil
		case "down", "ctrl+n":
			sg.cur = min(sg.cur+1, max(sg.count()-1, 0))
			return true, nil
		case "enter", "tab":
			if sg.count() > 0 {
				return true, m.acceptSuggest(ta)
			}
			m.closeSuggest() // nothing to pick: the key keeps its usual meaning
			return false, nil
		case "backspace":
			if sg.query == "" { // it deletes the trigger itself
				m.closeSuggest()
				return false, nil
			}
			_, size := utf8.DecodeLastRuneInString(sg.query)
			sg.query = sg.query[:len(sg.query)-size]
			return false, m.scheduleSuggest()
		}
		if isPrintable(msg) {
			text := msg.Text
			r, _ := utf8.DecodeRuneInString(text)
			// "# Title" and "## Title" are headings, "@ " is just an at sign
			if sg.query == "" && ((sg.kind == '#' && !isWordRune(r)) || (sg.kind == '@' && unicode.IsSpace(r))) {
				m.closeSuggest()
				return false, nil
			}
			sg.query += text
			if utf8.RuneCountInString(sg.query) > suggestMaxQuery {
				m.closeSuggest()
				return false, nil
			}
			return false, m.scheduleSuggest()
		}
		m.closeSuggest() // an arrow, home, a shortcut…: the user moved on
		return false, nil
	}

	if isPrintable(msg) && (k == "@" || k == "#") && triggerOK(ta) {
		m.sg = suggest{active: true, kind: k[0], seq: sg.seq + 1, busy: true}
		return false, m.cmdSuggest(m.sg.kind, "", m.sg.seq)
	}
	return false, nil
}

// scheduleSuggest searches once the typing pauses.
func (m *Model) scheduleSuggest() tea.Cmd {
	m.sg.seq++
	m.sg.busy, m.sg.err, m.sg.cur = true, "", 0
	seq := m.sg.seq
	return tea.Tick(userDebounce, func(time.Time) tea.Msg { return suggestQueryMsg{seq} })
}

func (m Model) onSuggestQuery(msg suggestQueryMsg) tea.Cmd {
	if !m.sg.active || msg.seq != m.sg.seq {
		return nil
	}
	return m.cmdSuggest(m.sg.kind, strings.TrimSpace(m.sg.query), msg.seq)
}

func (m *Model) onSuggest(msg suggestMsg) {
	if !m.sg.active || msg.seq != m.sg.seq {
		return // a newer search is pending, or the picker is closed
	}
	m.sg.busy = false
	if msg.err != nil {
		m.sg.err, m.sg.users, m.sg.issues = msg.err.Error(), nil, nil
		return
	}
	m.sg.users, m.sg.issues = msg.users, msg.issues
	m.sg.cur = min(m.sg.cur, max(m.sg.count()-1, 0))
}

// acceptSuggest replaces what was typed ("@so") by the chosen reference ("@Sofia M. ").
func (m *Model) acceptSuggest(ta *textarea.Model) tea.Cmd {
	sg := &m.sg
	var insert string
	switch sg.kind {
	case '@':
		u := sg.users[sg.cur]
		name := strings.TrimSpace(u.DisplayName)
		table := m.mentionTable()
		if old, ok := table[name]; ok && old != u.AccountID {
			sg.err = i18n.F("deux personnes s'appellent « %s » : mention impossible", "two people are called “%s”: cannot mention", name)
			return nil
		}
		table[name] = u.AccountID
		insert = "@" + name + " "
	default:
		insert = "#" + sg.issues[sg.cur].Key + " "
	}
	for i := 0; i < 1+utf8.RuneCountInString(sg.query); i++ { // the trigger and what followed it
		*ta, _ = ta.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyBackspace}))
	}
	ta.InsertString(insert)
	m.closeSuggest()
	return nil
}

// suggestView renders the picker, at most window items tall.
func (m Model) suggestView(width, window int) string {
	sg := m.sg
	title := i18n.T("Utilisateurs", "Users")
	if sg.kind == '#' {
		title = "Tickets"
	}
	var sb strings.Builder
	sb.WriteString(bold(colLav, title) + txt(colSub, "  "+string(sg.kind)+truncate(sg.query, max(width-len(title)-6, 4))) + "\n")
	n := sg.count()
	start := max(min(sg.cur-window+1, n-window), 0)
	for i := start; i < n && i < start+window; i++ {
		var label string
		if sg.kind == '@' {
			label = sg.users[i].DisplayName
			if sg.users[i].AccountType == "customer" { // a service desk customer, not a Jira user
				label += txt(colSub, i18n.T("  · client", "  · customer"))
			}
		} else {
			is := sg.issues[i]
			label = bold(colPink, is.Key) + "  " + txt(colFg, truncate(is.Summary, max(width-len(is.Key)-6, 8)))
		}
		if i == sg.cur {
			sb.WriteString(lipgloss.NewStyle().Background(colPurple).Foreground(colWhite).Render("▸ "+truncate(ansi.Strip(label), width-3)+" ") + "\n")
		} else {
			sb.WriteString("  " + label + "\n")
		}
	}
	switch {
	case sg.busy:
		sb.WriteString(txt(colSub, i18n.T("  recherche…", "  searching…")))
	case sg.err != "":
		sb.WriteString(txt(colPink, "✗ "+truncate(sg.err, width-2)))
	case n == 0:
		sb.WriteString(txt(colSub, i18n.T("  aucun résultat", "  no result")))
	}
	return strings.TrimRight(sb.String(), "\n")
}
