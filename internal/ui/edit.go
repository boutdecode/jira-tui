package ui

import (
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/kevin/jira-tui/internal/i18n"
	"github.com/kevin/jira-tui/internal/jira"
)

// userDebounce is how long the assignee search waits after the last keystroke.
const userDebounce = 250 * time.Millisecond

// editKind says what the shared line / text editors are editing.
type editKind int

const (
	editTitle editKind = iota
	editLabels
	editDesc
	editCommentText
)

// pickKind says what to do with the comment chosen in the picker.
type pickKind int

const (
	pickEdit pickKind = iota
	pickDelete
)

// --- messages ---

type editMetaMsg struct {
	key  string
	meta jira.EditMeta
	me   jira.User
	err  error
}
type userQueryMsg struct{ seq int }
type usersMsg struct {
	seq   int
	users []jira.User
	err   error
}

// menuItem is one line of the "modifier" menu; a non-empty reason disables it.
type menuItem struct {
	id, label, reason string
}

// editState groups the fields of the edit feature (embedded in Model).
type editState struct {
	meta  jira.EditMeta
	me    jira.User
	menu  []menuItem
	cur   int // cursor of the menu / priority list / comment picker
	kind  editKind
	pick  pickKind
	err   string
	line  textinput.Model // title, labels, assignee search
	area  textarea.Model  // description, comment text
	cmt   jira.Comment    // comment being edited / deleted
	users []jira.User
	ucur  int
	seq   int  // identifies the latest assignee search: older answers are dropped
	ubusy bool // an assignee search is pending
}

func newEditState() editState {
	ln := textinput.New()
	ln.Prompt = ""
	ln.SetWidth(56)
	ln.SetVirtualCursor(true)
	ln.SetStyles(inputStyles(ln.Styles()))
	ar := newComment()
	ar.Placeholder = ""
	return editState{line: ln, area: ar}
}

func (m Model) cmdEditMeta(key string) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		meta, err := svc.EditMeta(key)
		if err != nil {
			return editMetaMsg{key: key, err: err}
		}
		me, _ := svc.Myself() // without it only the comment entries are disabled
		return editMetaMsg{key: key, meta: meta, me: me}
	}
}

func (m Model) cmdUsers(key, query string, seq int) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		us, err := svc.SearchAssignable(key, query)
		return usersMsg{seq, us, err}
	}
}

// startEdit is bound to "e" in the detail: it fetches what Jira allows, then opens the menu.
func (m *Model) startEdit() tea.Cmd {
	if !m.hasDetail || m.detail.Key == "" {
		return nil
	}
	return m.busy(m.cmdEditMeta(m.detail.Key))
}

func (m *Model) onEditMeta(msg editMetaMsg) {
	m.loading--
	if msg.err != nil {
		m.setErr(msg.err)
		return
	}
	if msg.key != m.detail.Key {
		return // the user moved on
	}
	m.ed.meta, m.ed.me = msg.meta, msg.me
	m.ed.menu = m.buildEditMenu()
	m.ed.cur = 0
	m.mode = modeEditMenu
}

// myComments returns the comments written by the current user.
func (m Model) myComments() []jira.Comment {
	if m.ed.me.AccountID == "" {
		return nil
	}
	var out []jira.Comment
	for _, c := range m.detail.Fields.Comment.Comments {
		if c.Author.AccountID == m.ed.me.AccountID {
			out = append(out, c)
		}
	}
	return out
}

// markdownOf returns the Markdown to edit for doc; ok is false when editing it as Markdown
// would lose something (mentions, images, tables, colors…).
func markdownOf(doc any) (string, bool) { return jira.ADFToMarkdown(doc) }

func richReason() string {
	return i18n.T("contenu non modifiable ici (mentions, images, tableaux…) — à modifier dans Jira (touche o)",
		"content not editable here (mentions, images, tables…) — edit it in Jira (key o)")
}

func (m Model) buildEditMenu() []menuItem {
	f, ed := m.detail.Fields, m.ed.meta.Editable
	notEditable := i18n.T("non modifiable pour ce ticket", "not editable on this ticket")
	why := func(ok bool) string {
		if ok {
			return ""
		}
		return notEditable
	}
	descWhy := why(ed["description"])
	if _, ok := markdownOf(f.Description); descWhy == "" && !ok {
		descWhy = richReason()
	}
	prioWhy := why(ed["priority"] && len(m.ed.meta.Priorities) > 0)

	mine := m.myComments()
	editWhy, delWhy := "", ""
	switch {
	case m.detail.CommentsErr != "":
		editWhy = i18n.T("commentaires indisponibles", "comments unavailable")
		delWhy = editWhy
	case len(mine) == 0:
		editWhy = i18n.T("aucun commentaire de vous", "none of your comments")
		delWhy = editWhy
	case !slices.ContainsFunc(mine, func(c jira.Comment) bool { _, ok := markdownOf(c.Body); return ok }):
		editWhy = richReason()
	}
	return []menuItem{
		{"title", i18n.T("Titre", "Title"), why(ed["summary"])},
		{"desc", "Description", descWhy},
		{"priority", i18n.T("Priorité", "Priority"), prioWhy},
		{"labels", i18n.T("Étiquettes", "Labels"), why(ed["labels"])},
		{"assignee", i18n.T("Assigné", "Assignee"), ""},
		{"editcomment", i18n.T("Modifier un commentaire", "Edit a comment"), editWhy},
		{"delcomment", i18n.T("Supprimer un commentaire", "Delete a comment"), delWhy},
	}
}

func (m *Model) closeEdit() {
	m.ed.line.Blur()
	m.ed.area.Blur()
	m.ed.err = ""
	m.mode = modeNormal
}

// --- menu ---

func (m Model) keyEditMenu(k string) (tea.Model, tea.Cmd) {
	switch k {
	case "esc", "q", "e":
		m.closeEdit()
	case "up", "k":
		m.ed.cur = max(m.ed.cur-1, 0)
	case "down", "j":
		m.ed.cur = min(m.ed.cur+1, len(m.ed.menu)-1)
	case "enter":
		it := m.ed.menu[m.ed.cur]
		if it.reason != "" {
			return m, nil // the reason is shown under the menu
		}
		f := m.detail.Fields
		var cmd tea.Cmd
		switch it.id {
		case "title":
			cmd = m.startLine(editTitle, f.Summary)
		case "labels":
			cmd = m.startLine(editLabels, strings.Join(f.Labels, ", "))
		case "desc":
			md, _ := markdownOf(f.Description) // enabled only when editable
			cmd = m.startArea(editDesc, md)
		case "priority":
			m.ed.cur = 0
			if f.Priority != nil {
				if i := slices.IndexFunc(m.ed.meta.Priorities, func(p jira.Named) bool { return p.ID == f.Priority.ID || p.Name == f.Priority.Name }); i >= 0 {
					m.ed.cur = i
				}
			}
			m.mode = modeEditPriority
		case "assignee":
			cmd = m.startAssignee()
		case "editcomment", "delcomment":
			m.ed.pick = pickEdit
			if it.id == "delcomment" {
				m.ed.pick = pickDelete
			}
			m.ed.cur, m.ed.err = 0, ""
			m.mode = modeCommentPick
		}
		return m, cmd
	}
	return m, nil
}

// --- single line editors: title, labels ---

func (m *Model) startLine(kind editKind, value string) tea.Cmd {
	m.ed.kind, m.ed.err = kind, ""
	m.ed.line.Placeholder = ""
	m.ed.line.SetWidth(max(m.editW()-6, 16))
	m.ed.line.SetValue(value)
	m.ed.line.CursorEnd()
	m.mode = modeEditLine
	return m.ed.line.Focus()
}

func parseLabels(s string) []string {
	var out []string
	for _, l := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ';' || r == ' ' || r == '\t' }) {
		if !slices.Contains(out, l) {
			out = append(out, l)
		}
	}
	return out
}

func (m Model) keyEditLine(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.closeEdit()
		return m, nil
	case "enter":
		return m.submitLine()
	}
	var cmd tea.Cmd
	m.ed.line, cmd = m.ed.line.Update(msg)
	m.ed.err = ""
	return m, cmd
}

func (m Model) submitLine() (tea.Model, tea.Cmd) {
	key, f := m.detail.Key, m.detail.Fields
	v := strings.TrimSpace(m.ed.line.Value())
	switch m.ed.kind {
	case editTitle:
		switch {
		case v == "":
			m.ed.err = i18n.T("le titre ne peut pas être vide", "the title cannot be empty")
			return m, nil
		case v == f.Summary:
			m.closeEdit()
			return m, nil
		}
		m.closeEdit()
		return m, m.busy(m.cmdDo(i18n.F("titre de %s modifié", "title of %s updated", key), true,
			func() error { return m.svc.UpdateIssue(key, jira.FieldUpdate{Summary: &v}) }))
	case editLabels:
		labels := parseLabels(v)
		if slices.Equal(labels, f.Labels) {
			m.closeEdit()
			return m, nil
		}
		m.closeEdit()
		return m, m.busy(m.cmdDo(i18n.F("étiquettes de %s modifiées", "labels of %s updated", key), true,
			func() error { return m.svc.UpdateIssue(key, jira.FieldUpdate{Labels: &labels}) }))
	}
	return m, nil
}

// --- multi-line editors: description, comment ---

func (m *Model) startArea(kind editKind, value string) tea.Cmd {
	m.ed.kind, m.ed.err = kind, ""
	m.ed.area.SetWidth(max(m.editW()-6, 16))
	m.ed.area.SetHeight(max(min(m.h-12, 10), 3))
	m.ed.area.SetValue(value)
	m.mode = modeEditText
	return m.ed.area.Focus()
}

func (m Model) keyEditText(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.closeEdit()
		return m, nil
	case "ctrl+s":
		return m.submitText()
	}
	var cmd tea.Cmd
	m.ed.area, cmd = m.ed.area.Update(msg)
	m.ed.err = ""
	return m, cmd
}

func (m Model) submitText() (tea.Model, tea.Cmd) {
	key := m.detail.Key
	text := strings.TrimSpace(m.ed.area.Value())
	switch m.ed.kind {
	case editDesc:
		if cur, _ := markdownOf(m.detail.Fields.Description); text == strings.TrimSpace(cur) {
			m.closeEdit()
			return m, nil
		}
		m.closeEdit()
		return m, m.busy(m.cmdDo(i18n.F("description de %s modifiée", "description of %s updated", key), true,
			func() error { return m.svc.UpdateIssue(key, jira.FieldUpdate{Description: &text}) }))
	case editCommentText:
		c := m.ed.cmt
		switch {
		case text == "":
			m.ed.err = i18n.T("le commentaire ne peut pas être vide", "the comment cannot be empty")
			return m, nil
		case func() bool { cur, _ := markdownOf(c.Body); return text == strings.TrimSpace(cur) }():
			m.closeEdit()
			return m, nil
		}
		m.closeEdit()
		return m, m.busy(m.cmdDo(i18n.F("commentaire de %s modifié", "comment on %s updated", key), true,
			func() error { return m.svc.EditComment(key, c, text) }))
	}
	return m, nil
}

// --- priority ---

func (m Model) keyEditPriority(k string) (tea.Model, tea.Cmd) {
	ps := m.ed.meta.Priorities
	switch k {
	case "esc", "q":
		m.mode = modeEditMenu
		m.ed.cur = slices.IndexFunc(m.ed.menu, func(it menuItem) bool { return it.id == "priority" })
	case "up", "k":
		m.ed.cur = max(m.ed.cur-1, 0)
	case "down", "j":
		m.ed.cur = min(m.ed.cur+1, len(ps)-1)
	case "enter":
		p, key := ps[m.ed.cur], m.detail.Key
		cur := m.detail.Fields.Priority
		m.closeEdit()
		if cur != nil && (cur.ID == p.ID || (cur.ID == "" && cur.Name == p.Name)) {
			return m, nil
		}
		return m, m.busy(m.cmdDo(i18n.F("priorité de %s : %s", "priority of %s: %s", key, p.Name), true,
			func() error { return m.svc.UpdateIssue(key, jira.FieldUpdate{PriorityID: &p.ID}) }))
	}
	return m, nil
}

// --- comment picker and deletion ---

func (m Model) keyCommentPick(k string) (tea.Model, tea.Cmd) {
	mine := m.myComments()
	switch k {
	case "esc", "q":
		m.mode = modeEditMenu
		m.ed.cur = slices.IndexFunc(m.ed.menu, func(it menuItem) bool { return it.id == "editcomment" || it.id == "delcomment" })
		m.ed.err = ""
	case "up", "k":
		m.ed.cur = max(m.ed.cur-1, 0)
	case "down", "j":
		m.ed.cur = min(m.ed.cur+1, len(mine)-1)
	case "enter":
		if m.ed.cur >= len(mine) {
			return m, nil
		}
		c := mine[m.ed.cur]
		if m.ed.pick == pickDelete {
			m.ed.cmt, m.mode = c, modeCommentDelete
			return m, nil
		}
		md, ok := markdownOf(c.Body)
		if !ok {
			m.ed.err = richReason()
			return m, nil
		}
		m.ed.cmt = c
		return m, m.startArea(editCommentText, md)
	}
	return m, nil
}

func (m Model) keyCommentDelete(k string) (tea.Model, tea.Cmd) {
	switch k {
	case "esc", "n":
		m.mode = modeCommentPick
		m.ed.cur = 0
	case "enter", "y":
		key, id := m.detail.Key, m.ed.cmt.ID
		m.closeEdit()
		return m, m.busy(m.cmdDo(i18n.F("commentaire supprimé de %s", "comment deleted from %s", key), true,
			func() error { return m.svc.DeleteComment(key, id) }))
	}
	return m, nil
}

// --- assignee search ---

func (m *Model) startAssignee() tea.Cmd {
	m.ed.err, m.ed.users, m.ed.ucur = "", nil, 0
	m.ed.line.Placeholder = i18n.T("Rechercher un utilisateur…", "Search a user…")
	m.ed.line.SetWidth(max(min(m.editW(), 60)-6, 16))
	m.ed.line.SetValue("")
	m.ed.seq++
	m.ed.ubusy = true
	m.mode = modeAssignee
	return tea.Batch(m.ed.line.Focus(), m.cmdUsers(m.detail.Key, "", m.ed.seq))
}

// assigneeChoices is what the list shows: "unassigned" first while nothing is typed, then the users found.
func (m Model) assigneeChoices() []jira.User {
	var out []jira.User
	if strings.TrimSpace(m.ed.line.Value()) == "" {
		out = append(out, jira.User{DisplayName: i18n.T("Non assigné", "Unassigned")})
	}
	return append(out, m.ed.users...)
}

func (m Model) keyAssignee(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	choices := m.assigneeChoices()
	switch msg.String() {
	case "esc":
		m.mode = modeEditMenu
		m.ed.cur = slices.IndexFunc(m.ed.menu, func(it menuItem) bool { return it.id == "assignee" })
		m.ed.line.Blur()
		return m, nil
	case "up", "ctrl+p":
		m.ed.ucur = max(m.ed.ucur-1, 0)
		return m, nil
	case "down", "ctrl+n":
		m.ed.ucur = min(m.ed.ucur+1, max(len(choices)-1, 0))
		return m, nil
	case "enter":
		if m.ed.ucur >= len(choices) {
			return m, nil
		}
		c, key := choices[m.ed.ucur], m.detail.Key
		cur := m.detail.Fields.Assignee
		m.closeEdit()
		switch {
		case c.AccountID == "" && cur == nil, cur != nil && c.AccountID == cur.AccountID:
			return m, nil // nothing to change
		case c.AccountID == "":
			return m, m.busy(m.cmdDo(i18n.F("%s désassigné", "%s unassigned", key), true,
				func() error { return m.svc.Unassign(key) }))
		}
		return m, m.busy(m.cmdDo(i18n.F("%s assigné à %s", "%s assigned to %s", key, c.DisplayName), true,
			func() error { return m.svc.AssignTo(key, c.AccountID) }))
	}
	before := m.ed.line.Value()
	var cmd tea.Cmd
	m.ed.line, cmd = m.ed.line.Update(msg)
	if m.ed.line.Value() == before {
		return m, cmd
	}
	// the text changed: search once typing pauses; answers to older searches are dropped
	m.ed.seq++
	m.ed.ubusy, m.ed.err = true, ""
	seq := m.ed.seq
	tick := tea.Tick(userDebounce, func(time.Time) tea.Msg { return userQueryMsg{seq} })
	return m, tea.Batch(cmd, tick)
}

func (m Model) onUserQuery(msg userQueryMsg) tea.Cmd {
	if msg.seq != m.ed.seq || m.mode != modeAssignee {
		return nil
	}
	return m.cmdUsers(m.detail.Key, strings.TrimSpace(m.ed.line.Value()), msg.seq)
}

func (m *Model) onUsers(msg usersMsg) {
	if msg.seq != m.ed.seq {
		return // a newer search is pending or done
	}
	m.ed.ubusy = false
	if msg.err != nil {
		m.ed.err = msg.err.Error()
		m.ed.users = nil
		return
	}
	m.ed.users = msg.users
	m.ed.ucur = min(m.ed.ucur, max(len(m.assigneeChoices())-1, 0))
}

// --- rendering ---

// editW is the width of the text in the edit modals: they must fit the terminal (border and padding add 4).
func (m Model) editW() int { return max(min(m.w-6, 66), 24) }

func (m Model) editContent() string {
	var sb strings.Builder
	key, cw := m.detail.Key, m.editW()
	pick := func(sel bool, label string) string {
		if sel {
			return lipgloss.NewStyle().Background(colPurple).Foreground(colWhite).Render("▸ " + label + " ")
		}
		return "  " + label
	}
	hintLine := func(s string) { sb.WriteString("\n\n" + txt(colSub, truncate(s, cw))) }
	errLine := func() {
		if m.ed.err != "" {
			sb.WriteString("\n" + txt(colPink, "✗ "+truncate(m.ed.err, cw-2)))
		}
	}

	switch m.mode {
	case modeEditMenu:
		sb.WriteString(bold(colLav, i18n.T("Modifier ", "Edit ")+key) + "\n\n")
		for i, it := range m.ed.menu {
			label := it.label
			if it.reason != "" {
				if i == m.ed.cur {
					sb.WriteString(lipgloss.NewStyle().Background(colBorder).Foreground(colSub).Render("▸ "+label+" ") + "\n")
				} else {
					sb.WriteString("  " + txt(colSub, label) + "\n")
				}
				continue
			}
			sb.WriteString(pick(i == m.ed.cur, label) + "\n")
		}
		// the reason of a disabled entry: two lines, always reserved so that the modal does not jump
		reason := []string{"", ""}
		if r := m.ed.menu[m.ed.cur].reason; r != "" {
			copy(reason, wrapLines(r, cw, 2))
		}
		for _, l := range reason {
			sb.WriteString("\n" + txt(colYellow, l))
		}
		hintLine(i18n.T("↑/↓ choisir · enter ouvrir · esc fermer", "↑/↓ choose · enter open · esc close"))

	case modeEditLine:
		title, help := i18n.T("Titre de ", "Title of "), ""
		if m.ed.kind == editLabels {
			title = i18n.T("Étiquettes de ", "Labels of ")
			help = i18n.T("séparées par des virgules ou des espaces (une étiquette n'a pas d'espace)", "separated by commas or spaces (a label cannot contain spaces)")
		}
		sb.WriteString(bold(colLav, title+key) + "\n\n")
		sb.WriteString(boxStyle(colPurple).Width(cw).Render(m.ed.line.View()))
		if help != "" {
			sb.WriteString("\n" + txt(colSub, truncate(help, cw)))
		}
		errLine()
		hintLine(i18n.T("enter enregistrer · esc annuler", "enter save · esc cancel"))

	case modeEditText:
		title := i18n.T("Description de ", "Description of ") + key
		if m.ed.kind == editCommentText {
			title = i18n.T("Modifier le commentaire de ", "Edit your comment on ") + key
		}
		sb.WriteString(bold(colLav, title) + "\n\n")
		sb.WriteString(boxStyle(colPurple).Background(colInput).BorderBackground(colInput).Render(m.ed.area.View()))
		sb.WriteString("\n" + txt(colSub, truncate(i18n.T("Markdown : **gras** *italique* `code` [lien](url) - liste 1. liste", "Markdown: **bold** *italic* `code` [link](url) - list 1. list"), cw)))
		errLine()
		hintLine(i18n.T("ctrl+s enregistrer · esc annuler", "ctrl+s save · esc cancel"))

	case modeEditPriority:
		sb.WriteString(bold(colLav, i18n.T("Priorité de ", "Priority of ")+key) + "\n\n")
		cur := m.detail.Fields.Priority
		for i, p := range m.ed.meta.Priorities {
			label := p.Name
			if cur != nil && (cur.ID == p.ID || (cur.ID == "" && cur.Name == p.Name)) {
				label += txt(colSub, "  ✓")
			}
			sb.WriteString(pick(i == m.ed.cur, label) + "\n")
		}
		hintLine(i18n.T("↑/↓ choisir · enter valider · esc retour", "↑/↓ choose · enter confirm · esc back"))

	case modeAssignee:
		sb.WriteString(bold(colLav, i18n.T("Assigner ", "Assign ")+key) + "\n\n")
		sb.WriteString(boxStyle(colPurple).Width(min(cw, 60)).Render(m.ed.line.View()) + "\n")
		cur := m.detail.Fields.Assignee
		choices := m.assigneeChoices()
		const window = 8
		start := max(min(m.ed.ucur-window+1, len(choices)-window), 0)
		for i := start; i < len(choices) && i < start+window; i++ {
			c := choices[i]
			label := c.DisplayName
			if (cur == nil && c.AccountID == "") || (cur != nil && c.AccountID == cur.AccountID) {
				label += txt(colSub, "  ✓")
			}
			sb.WriteString(pick(i == m.ed.ucur, label) + "\n")
		}
		switch {
		case m.ed.ubusy:
			sb.WriteString(txt(colSub, i18n.T("  recherche…", "  searching…")) + "\n")
		case len(m.ed.users) == 0 && m.ed.err == "":
			sb.WriteString(txt(colSub, i18n.T("  aucun utilisateur trouvé", "  no user found")) + "\n")
		}
		errLine()
		hintLine(i18n.T("tapez pour chercher · ↑/↓ choisir · enter assigner · esc retour", "type to search · ↑/↓ choose · enter assign · esc back"))

	case modeCommentPick:
		mine := m.myComments()
		title := i18n.T("Modifier quel commentaire ?", "Edit which comment?")
		if m.ed.pick == pickDelete {
			title = i18n.T("Supprimer quel commentaire ?", "Delete which comment?")
		}
		sb.WriteString(bold(colLav, title) + "\n\n")
		for i, c := range mine {
			when := agoFull(c.RawDate)
			excerpt := strings.Join(strings.Fields(jira.ADFToText(c.Body)), " ")
			label := truncate(when+" · "+excerpt, cw-2)
			if _, ok := markdownOf(c.Body); m.ed.pick == pickEdit && !ok {
				label = truncate(label, max(cw-18, 8)) + txt(colSub, i18n.T("  (non modifiable ici)", "  (not editable here)"))
			}
			sb.WriteString(pick(i == m.ed.cur, label) + "\n")
		}
		errLine()
		hintLine(i18n.T("↑/↓ choisir · enter sélectionner · esc retour", "↑/↓ choose · enter select · esc back"))

	case modeCommentDelete:
		sb.WriteString(bold(colYellow, i18n.T("Supprimer ce commentaire ?", "Delete this comment?")) + "\n\n")
		excerpt := strings.Join(strings.Fields(jira.ADFToText(m.ed.cmt.Body)), " ")
		sb.WriteString(txt(colSub, agoFull(m.ed.cmt.RawDate)) + "\n")
		for _, l := range wrapLines(excerpt, cw-2, 4) {
			sb.WriteString(txt(colFg, l) + "\n")
		}
		hintLine(i18n.T("enter supprimer · esc retour (suppression définitive)", "enter delete · esc back (this cannot be undone)"))
	}
	return strings.TrimRight(sb.String(), "\n")
}
