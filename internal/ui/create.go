package ui

import (
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/kevin/jira-tui/internal/i18n"
	"github.com/kevin/jira-tui/internal/jira"
)

// "n" opens the form that creates an issue: project, type, title, description (Markdown with
// the "@" and "#" pickers), priority, labels and assignee. What the form offers comes from
// Jira's create metadata, so it follows each project's configuration; a required field the
// app cannot fill blocks the creation with an explicit message instead of a failed request.

type createField int

const (
	fProject createField = iota
	fType
	fTitle
	fDesc
	fPriority
	fLabels
	fAssignee
	nCreateFields
)

// --- messages ---

type createTypesMsg struct {
	seq     int
	project string
	types   []jira.IssueType
	err     error
}
type createMetaMsg struct {
	seq  int
	meta jira.NewIssueMeta
	err  error
}
type projectsMsg struct {
	seq      int
	projects []jira.Project
	err      error
}
type projectQueryMsg struct{ seq int }
type createdMsg struct {
	ref jira.IssueRef
	err error
}

// createState is the form being filled (embedded in Model as cr).
type createState struct {
	project jira.Project
	parent  string // ticket the form was opened from: the parent of a sub-task
	types   []jira.IssueType
	tcur    int
	meta    jira.NewIssueMeta
	loading bool // the types or the form of the type are being loaded
	reqSeq  int  // identifies the latest load: older answers are dropped

	title    textinput.Model
	desc     textarea.Model
	labels   textinput.Model
	prio     int  // -1: the project's default; otherwise an index in meta.Priorities
	me       bool // assign to me (otherwise the project's default assignee applies)
	focus    createField
	mentions map[string]string // name -> account id of the mentions typed in the description

	err      string
	creating bool
	confirm  bool // "abandon the draft?"

	picking  bool // the project list is open
	pquery   textinput.Model
	projects []jira.Project
	pcur     int
	pseq     int
	pbusy    bool
	perr     string
}

func newCreateState() createState {
	line := func() textinput.Model {
		ti := textinput.New()
		ti.Prompt = ""
		ti.SetVirtualCursor(true)
		ti.SetStyles(inputStyles(ti.Styles()))
		return ti
	}
	cr := createState{title: line(), labels: line(), pquery: line(), desc: newComment(), prio: -1, mentions: map[string]string{}}
	cr.desc.Placeholder = ""
	return cr
}

func (cr createState) dirty() bool {
	return strings.TrimSpace(cr.title.Value()) != "" || strings.TrimSpace(cr.desc.Value()) != "" || strings.TrimSpace(cr.labels.Value()) != ""
}

func (cr createState) selectedType() (jira.IssueType, bool) {
	if cr.tcur < 0 || cr.tcur >= len(cr.types) {
		return jira.IssueType{}, false
	}
	return cr.types[cr.tcur], true
}

// --- commands ---

func (m Model) cmdProjects(query string, seq int) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		ps, err := svc.Projects(query)
		return projectsMsg{seq, ps, err}
	}
}

func (m Model) cmdCreateTypes(project string, seq int) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		ts, err := svc.IssueTypes(project)
		return createTypesMsg{seq, project, ts, err}
	}
}

func (m Model) cmdCreateMeta(project, typeID string, seq int) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		meta, err := svc.NewIssueMeta(project, typeID)
		return createMetaMsg{seq, meta, err}
	}
}

func (m Model) cmdCreate(in jira.NewIssue) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		ref, err := svc.CreateIssue(in)
		return createdMsg{ref, err}
	}
}

// --- opening ---

// defaultProject is the project the form starts on: the one of the open ticket, the last
// one used, or the one of the tickets on screen.
func (m Model) defaultProject() string {
	if m.view == viewDetail && m.hasDetail {
		if p, _, ok := strings.Cut(m.detail.Key, "-"); ok {
			return p
		}
	}
	if m.lastProject != "" {
		return m.lastProject
	}
	if k := jira.ProjectKey(m.issues); k != "" {
		return k
	}
	return jira.ProjectKey(m.bissues)
}

func (m *Model) startCreate() tea.Cmd {
	cr := newCreateState()
	if m.view == viewDetail && m.hasDetail {
		cr.parent = m.detail.Key
	}
	w := max(m.editW()-16, 12)
	cr.title.SetWidth(w)
	cr.title.Placeholder = i18n.T("Résumé du ticket", "Issue summary")
	cr.labels.SetWidth(w)
	cr.labels.Placeholder = i18n.T("séparées par des virgules", "separated by commas")
	cr.desc.SetWidth(max(m.editW()-6, 16))
	cr.desc.SetHeight(max(min(m.h-21, 8), 3))
	cr.pquery.SetWidth(max(m.editW()-6, 12))
	cr.pquery.Placeholder = i18n.T("Rechercher un projet…", "Search a project…")
	cr.focus = fTitle
	m.cr = cr
	m.mode = modeCreate
	m.closeSuggest()

	key := m.defaultProject()
	if key == "" { // nothing on screen to guess from: start by choosing the project
		return m.openProjectPicker()
	}
	m.cr.project = jira.Project{Key: key}
	m.cr.loading = true
	m.cr.reqSeq++
	return tea.Batch(m.focusCreate(fTitle), m.cmdCreateTypes(key, m.cr.reqSeq))
}

// focusCreate moves the focus to field f (the text widgets take the keyboard).
func (m *Model) focusCreate(f createField) tea.Cmd {
	m.cr.focus = (f + nCreateFields) % nCreateFields
	m.cr.title.Blur()
	m.cr.desc.Blur()
	m.cr.labels.Blur()
	m.closeSuggest()
	switch m.cr.focus {
	case fTitle:
		return m.cr.title.Focus()
	case fDesc:
		return m.cr.desc.Focus()
	case fLabels:
		return m.cr.labels.Focus()
	}
	return nil
}

// visibleFields are the fields of the form for this project and type, in order.
func (m Model) createFields() []createField {
	fs := []createField{fProject, fType, fTitle}
	if m.cr.meta.HasDesc || m.cr.loading {
		fs = append(fs, fDesc)
	}
	if m.cr.meta.HasPriority && len(m.cr.meta.Priorities) > 0 {
		fs = append(fs, fPriority)
	}
	if m.cr.meta.HasLabels {
		fs = append(fs, fLabels)
	}
	if m.cr.meta.HasAssignee {
		fs = append(fs, fAssignee)
	}
	return fs
}

func (m *Model) stepCreate(delta int) tea.Cmd {
	fs := m.createFields()
	i := slices.Index(fs, m.cr.focus)
	if i < 0 {
		i = 0
	}
	return m.focusCreate(fs[(i+delta+len(fs))%len(fs)])
}

func (m *Model) closeCreate() {
	m.closeSuggest()
	m.cr.title.Blur()
	m.cr.desc.Blur()
	m.cr.labels.Blur()
	m.cr.pquery.Blur()
	m.cr = newCreateState()
	m.mode = modeNormal
}

// --- answers ---

func (m *Model) onCreateTypes(msg createTypesMsg) tea.Cmd {
	if msg.seq != m.cr.reqSeq || m.mode != modeCreate {
		return nil
	}
	if msg.err != nil {
		m.cr.loading, m.cr.err = false, msg.err.Error()
		return nil
	}
	var types []jira.IssueType
	for _, t := range msg.types {
		if !t.Subtask || m.cr.parent != "" { // a sub-task needs a parent: only offered from an open ticket
			types = append(types, t)
		}
	}
	if len(types) == 0 {
		m.cr.loading, m.cr.types = false, nil
		m.cr.err = i18n.F("aucun type de ticket disponible dans %s", "no issue type available in %s", msg.project)
		return nil
	}
	m.cr.err = ""
	m.cr.types = types
	m.cr.tcur = slices.IndexFunc(types, func(t jira.IssueType) bool { return !t.Subtask })
	if m.cr.tcur < 0 {
		m.cr.tcur = 0
	}
	return m.loadCreateMeta()
}

// loadCreateMeta reads the form of the selected type.
func (m *Model) loadCreateMeta() tea.Cmd {
	t, ok := m.cr.selectedType()
	if !ok {
		return nil
	}
	m.cr.loading, m.cr.err = true, ""
	m.cr.reqSeq++
	return m.cmdCreateMeta(m.cr.project.Key, t.ID, m.cr.reqSeq)
}

func (m *Model) onCreateMeta(msg createMetaMsg) {
	if msg.seq != m.cr.reqSeq || m.mode != modeCreate {
		return
	}
	m.cr.loading = false
	if msg.err != nil {
		m.cr.err = msg.err.Error()
		return
	}
	m.cr.meta = msg.meta
	if m.cr.prio >= len(msg.meta.Priorities) {
		m.cr.prio = -1
	}
	if !slices.Contains(m.createFields(), m.cr.focus) {
		m.cr.focus = fTitle
	}
}

func (m *Model) onCreated(msg createdMsg) tea.Cmd {
	if msg.err != nil { // the draft stays as it was
		m.cr.creating, m.cr.err = false, msg.err.Error()
		return nil
	}
	m.lastProject = m.cr.project.Key
	m.closeCreate()
	m.setInfo(i18n.F("%s créé : %s", "%s created: %s", msg.ref.Key, msg.ref.Summary))
	m.holdInfo = true // the reload below must not overwrite the confirmation
	m.pendingOpen = msg.ref.Key
	cmds := []tea.Cmd{m.busy(m.cmdDetail(msg.ref.Key)), m.busy(m.cmdSearch(m.jqlString()))}
	if len(m.cols) > 0 {
		cmds = append(cmds, m.busy(m.cmdBoard(m.board)))
	}
	return tea.Batch(cmds...)
}

// --- project picker ---

func (m *Model) openProjectPicker() tea.Cmd {
	m.cr.picking, m.cr.pcur, m.cr.perr = true, 0, ""
	m.cr.title.Blur()
	m.cr.desc.Blur()
	m.cr.labels.Blur()
	m.cr.pquery.SetValue("")
	m.cr.pseq++
	m.cr.pbusy = true
	return tea.Batch(m.cr.pquery.Focus(), m.cmdProjects("", m.cr.pseq))
}

func (m *Model) onProjects(msg projectsMsg) {
	if msg.seq != m.cr.pseq || !m.cr.picking {
		return
	}
	m.cr.pbusy = false
	if msg.err != nil {
		m.cr.perr, m.cr.projects = msg.err.Error(), nil
		return
	}
	m.cr.projects = msg.projects
	m.cr.pcur = min(m.cr.pcur, max(len(msg.projects)-1, 0))
}

func (m Model) onProjectQuery(msg projectQueryMsg) tea.Cmd {
	if !m.cr.picking || msg.seq != m.cr.pseq {
		return nil
	}
	return m.cmdProjects(strings.TrimSpace(m.cr.pquery.Value()), msg.seq)
}

func (m Model) keyProjectPicker(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.cr.picking = false
		m.cr.pquery.Blur()
		if m.cr.project.Key == "" { // no project chosen yet: nothing to fill in
			m.closeCreate()
			return m, nil
		}
		return m, m.focusCreate(m.cr.focus)
	case "up", "ctrl+p":
		m.cr.pcur = max(m.cr.pcur-1, 0)
		return m, nil
	case "down", "ctrl+n":
		m.cr.pcur = min(m.cr.pcur+1, max(len(m.cr.projects)-1, 0))
		return m, nil
	case "enter":
		if m.cr.pcur >= len(m.cr.projects) {
			return m, nil
		}
		p := m.cr.projects[m.cr.pcur]
		m.cr.picking = false
		m.cr.pquery.Blur()
		if p.Key == m.cr.project.Key {
			m.cr.project = p
			return m, m.focusCreate(fTitle)
		}
		m.cr.project = p
		m.cr.types, m.cr.meta, m.cr.prio = nil, jira.NewIssueMeta{}, -1
		m.cr.loading, m.cr.err = true, ""
		m.cr.reqSeq++
		return m, tea.Batch(m.focusCreate(fTitle), m.cmdCreateTypes(p.Key, m.cr.reqSeq))
	}
	before := m.cr.pquery.Value()
	var cmd tea.Cmd
	m.cr.pquery, cmd = m.cr.pquery.Update(msg)
	if m.cr.pquery.Value() == before {
		return m, cmd
	}
	m.cr.pseq++
	m.cr.pbusy, m.cr.perr, m.cr.pcur = true, "", 0
	seq := m.cr.pseq
	tick := tea.Tick(userDebounce, func(time.Time) tea.Msg { return projectQueryMsg{seq} })
	return m, tea.Batch(cmd, tick)
}

// --- keys ---

func (m Model) keyCreate(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	cr := &m.cr
	if cr.creating { // the request is on its way
		return m, nil
	}
	if cr.picking {
		return m.keyProjectPicker(msg)
	}
	k := msg.String()
	if cr.confirm {
		switch k {
		case "enter", "y":
			m.closeCreate()
		case "esc", "n":
			cr.confirm = false
		}
		return m, nil
	}

	// the "@" / "#" picker of the description comes first
	var scmd tea.Cmd
	if cr.focus == fDesc {
		var consumed bool
		if consumed, scmd = m.suggestKey(msg, &cr.desc); consumed {
			return m, scmd
		}
	}

	switch k {
	case "ctrl+s":
		return m.submitCreate()
	case "ctrl+o":
		return m, openURL(m.createURL())
	case "esc":
		if cr.dirty() {
			cr.confirm = true
			return m, nil
		}
		m.closeCreate()
		return m, nil
	case "tab":
		return m, m.stepCreate(1)
	case "shift+tab":
		return m, m.stepCreate(-1)
	}

	switch cr.focus {
	case fProject:
		switch k {
		case "enter", "space", " ":
			return m, m.openProjectPicker()
		case "down":
			return m, m.stepCreate(1)
		case "up":
			return m, m.stepCreate(-1)
		}
	case fType:
		switch k {
		case "left", "right":
			if len(cr.types) > 1 && !cr.loading {
				step := 1
				if k == "left" {
					step = -1
				}
				cr.tcur = (cr.tcur + step + len(cr.types)) % len(cr.types)
				return m, m.loadCreateMeta()
			}
		case "down", "enter":
			return m, m.stepCreate(1)
		case "up":
			return m, m.stepCreate(-1)
		}
	case fTitle, fLabels:
		in := &cr.title
		if cr.focus == fLabels {
			in = &cr.labels
		}
		switch k {
		case "enter", "down":
			return m, m.stepCreate(1)
		case "up":
			return m, m.stepCreate(-1)
		}
		var cmd tea.Cmd
		*in, cmd = in.Update(msg)
		cr.err = ""
		return m, cmd
	case fDesc:
		var cmd tea.Cmd
		cr.desc, cmd = cr.desc.Update(msg)
		cr.err = ""
		return m, tea.Batch(cmd, scmd)
	case fPriority:
		n := len(cr.meta.Priorities)
		switch k {
		case "left":
			cr.prio = (cr.prio+1+n)%(n+1) - 1
		case "right":
			cr.prio = (cr.prio+2)%(n+1) - 1
		case "down", "enter":
			return m, m.stepCreate(1)
		case "up":
			return m, m.stepCreate(-1)
		}
	case fAssignee:
		switch k {
		case "left", "right", "space", " ":
			cr.me = !cr.me
		case "down", "enter":
			return m, m.stepCreate(1)
		case "up":
			return m, m.stepCreate(-1)
		}
	}
	return m, nil
}

// createURL is the page of Jira's own create dialog, for what the app cannot fill.
func (m Model) createURL() string {
	return strings.TrimSuffix(m.svc.BrowseURL(""), "/browse/") + "/secure/CreateIssue!default.jspa"
}

func (m Model) submitCreate() (tea.Model, tea.Cmd) {
	cr := &m.cr
	t, ok := cr.selectedType()
	title := strings.TrimSpace(cr.title.Value())
	switch {
	case cr.loading || !ok:
		cr.err = i18n.T("chargement du formulaire en cours…", "the form is still loading…")
		return m, nil
	case len(cr.meta.Unsupported) > 0:
		cr.err = unsupportedMsg(cr.meta.Unsupported)
		return m, nil
	case title == "":
		cr.err = i18n.T("le titre ne peut pas être vide", "the title cannot be empty")
		return m, m.focusCreate(fTitle)
	case t.Subtask && cr.parent == "":
		cr.err = i18n.T("une sous-tâche a besoin d'un ticket parent", "a sub-task needs a parent ticket")
		return m, nil
	}
	in := jira.NewIssue{ProjectKey: cr.project.Key, TypeID: t.ID, Summary: title}
	if cr.meta.HasDesc {
		if text := strings.TrimSpace(cr.desc.Value()); text != "" {
			body := m.converter(cr.mentions).ToADF(text)
			in.Description = &body
		}
	}
	if cr.meta.HasPriority && cr.prio >= 0 && cr.prio < len(cr.meta.Priorities) {
		in.PriorityID = cr.meta.Priorities[cr.prio].ID
	}
	if cr.meta.HasLabels {
		in.Labels = parseLabels(cr.labels.Value())
	}
	in.AssignToMe = cr.me && cr.meta.HasAssignee
	if t.Subtask {
		in.ParentKey = cr.parent
	}
	cr.creating, cr.err = true, ""
	return m, m.cmdCreate(in)
}

func unsupportedMsg(fields []string) string {
	return i18n.F("champ obligatoire non géré : %s — créez ce ticket dans Jira (ctrl+o)", "unsupported required field: %s — create this ticket in Jira (ctrl+o)", strings.Join(fields, ", "))
}

// --- rendering ---

func (m Model) createContent() string {
	cr := m.cr
	cw := m.editW()
	var sb strings.Builder

	if cr.confirm {
		sb.WriteString(bold(colYellow, i18n.T("Abandonner le brouillon ?", "Discard the draft?")) + "\n\n")
		sb.WriteString(txt(colSub, truncate(strings.TrimSpace(cr.title.Value()), cw)) + "\n\n")
		sb.WriteString(txt(colSub, truncate(i18n.T("enter abandonner · esc continuer à écrire", "enter discard · esc keep writing"), cw)))
		return sb.String()
	}
	if cr.picking {
		sb.WriteString(bold(colLav, i18n.T("Choisir un projet", "Choose a project")) + "\n\n")
		sb.WriteString(boxStyle(colPurple).Width(min(cw, 60)).Render(cr.pquery.View()) + "\n")
		const window = 8
		start := max(min(cr.pcur-window+1, len(cr.projects)-window), 0)
		for i := start; i < len(cr.projects) && i < start+window; i++ {
			p := cr.projects[i]
			label := bold(colPink, p.Key) + "  " + txt(colFg, truncate(p.Name, max(cw-len(p.Key)-6, 8)))
			if i == cr.pcur {
				sb.WriteString(lipgloss.NewStyle().Background(colPurple).Foreground(colWhite).Render("▸ "+truncate(ansi.Strip(label), cw-3)+" ") + "\n")
			} else {
				sb.WriteString("  " + label + "\n")
			}
		}
		switch {
		case cr.pbusy:
			sb.WriteString(txt(colSub, i18n.T("  recherche…", "  searching…")) + "\n")
		case cr.perr != "":
			sb.WriteString(txt(colPink, "✗ "+truncate(cr.perr, cw-2)) + "\n")
		case len(cr.projects) == 0:
			sb.WriteString(txt(colSub, i18n.T("  aucun projet", "  no project")) + "\n")
		}
		sb.WriteString("\n" + txt(colSub, truncate(i18n.T("tapez pour chercher · ↑/↓ choisir · enter sélectionner · esc retour", "type to search · ↑/↓ choose · enter select · esc back"), cw)))
		return sb.String()
	}

	t, hasType := cr.selectedType()
	title := i18n.T("Nouveau ticket", "New issue")
	if hasType && t.Subtask && cr.parent != "" {
		title = i18n.T("Nouvelle sous-tâche de ", "New sub-task of ") + cr.parent
	}
	sb.WriteString(bold(colLav, title) + "\n\n")

	label := func(f createField, name string) string {
		if cr.focus == f {
			return bold(colPink, "▸ "+padTo(name, 12))
		}
		return txt(colSub, "  "+padTo(name, 12))
	}
	val := func(f createField, s string) string {
		if cr.focus == f {
			return txt(colWhite, s)
		}
		return txt(colFg, s)
	}
	cycle := func(f createField, s string) string {
		if cr.focus == f {
			return txt(colPink, "◂ ") + txt(colWhite, s) + txt(colPink, " ▸")
		}
		return txt(colFg, s)
	}

	// while the "@" / "#" list is open the other rows make room for it
	compact := m.sg.active && cr.focus == fDesc
	if compact {
		sb.WriteString(label(fDesc, "Description") + txt(colSub, cr.project.Key+" · "+truncate(strings.TrimSpace(cr.title.Value()), max(cw-30, 8))) + "\n")
		sb.WriteString(boxStyle(colPurple).Background(colInput).BorderBackground(colInput).Render(cr.desc.View()) + "\n")
		sb.WriteString(m.suggestView(cw, max(min(suggestWindow, m.h-14), 2)) + "\n")
		sb.WriteString("\n" + txt(colSub, truncate(i18n.T("↑/↓ choisir · enter insérer · esc fermer la liste", "↑/↓ choose · enter insert · esc close the list"), cw)))
		return sb.String()
	}

	proj := cr.project.Key
	if cr.project.Name != "" {
		proj += " — " + cr.project.Name
	}
	if proj == "" {
		proj = "—"
	}
	sb.WriteString(label(fProject, i18n.T("Projet", "Project")) + val(fProject, truncate(proj, cw-16)) + "\n")

	typ := "…"
	switch {
	case hasType:
		typ = t.Name
		if t.Subtask {
			typ += txt(colSub, i18n.T("  (sous-tâche de ", "  (sub-task of ")+cr.parent+")")
		}
	case !cr.loading:
		typ = "—"
	}
	if cr.loading {
		typ += txt(colSub, i18n.T("  chargement…", "  loading…"))
	}
	sb.WriteString(label(fType, "Type") + cycle(fType, ansi.Strip(typ)) + "\n")
	sb.WriteString(label(fTitle, i18n.T("Titre", "Title")) + cr.title.View() + "\n")

	if slices.Contains(m.createFields(), fDesc) {
		sb.WriteString(label(fDesc, "Description") + txt(colSub, "Markdown · @ · #") + "\n")
		border := colBorder
		if cr.focus == fDesc {
			border = colPurple
		}
		sb.WriteString(boxStyle(border).Background(colInput).BorderBackground(colInput).Render(cr.desc.View()) + "\n")
	}
	if slices.Contains(m.createFields(), fPriority) {
		p := i18n.T("— (défaut du projet)", "— (project default)")
		if cr.prio >= 0 && cr.prio < len(cr.meta.Priorities) {
			p = cr.meta.Priorities[cr.prio].Name
		}
		sb.WriteString(label(fPriority, i18n.T("Priorité", "Priority")) + cycle(fPriority, p) + "\n")
	}
	if slices.Contains(m.createFields(), fLabels) {
		sb.WriteString(label(fLabels, i18n.T("Étiquettes", "Labels")) + cr.labels.View() + "\n")
	}
	if slices.Contains(m.createFields(), fAssignee) {
		a := i18n.T("Automatique (défaut du projet)", "Automatic (project default)")
		if cr.me {
			a = i18n.T("Moi", "Me")
		}
		sb.WriteString(label(fAssignee, i18n.T("Assigné", "Assignee")) + cycle(fAssignee, a) + "\n")
	}

	switch {
	case cr.creating:
		sb.WriteString("\n" + txt(colSub, i18n.T("création en cours…", "creating…")))
	case cr.err != "":
		sb.WriteString("\n" + txt(colPink, "✗ "+truncate(cr.err, cw-2)))
	case len(cr.meta.Unsupported) > 0:
		for _, l := range wrapLines(unsupportedMsg(cr.meta.Unsupported), cw, 2) {
			sb.WriteString("\n" + txt(colYellow, l))
		}
	}
	hint := i18n.T("tab champ suivant · ctrl+s créer · ctrl+o Jira · esc annuler", "tab next field · ctrl+s create · ctrl+o Jira · esc cancel")
	sb.WriteString("\n\n" + txt(colSub, truncate(hint, cw)))
	return sb.String()
}

func padTo(s string, n int) string {
	if w := lipgloss.Width(s); w < n {
		return s + strings.Repeat(" ", n-w)
	}
	return s
}
