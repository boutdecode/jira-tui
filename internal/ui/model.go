package ui

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/kevin/jira-tui/internal/automation"
	"github.com/kevin/jira-tui/internal/config"
	"github.com/kevin/jira-tui/internal/i18n"
	"github.com/kevin/jira-tui/internal/jira"
	"github.com/kevin/jira-tui/internal/tempo"
)

type viewKind int

const (
	viewList viewKind = iota
	viewBoard
	viewDetail
)

type modeKind int

const (
	modeNormal modeKind = iota
	modeFilter          // typing in the "Filtrer les tickets…" bar
	modeJQL             // raw JQL modal (ctrl+j)
	modeFilters
	modeTransition
	modeComment
	modeBoards
	modeLog // add time (Tempo)
	modeAutoList
	modeAutoInputs
	modeAutoConfirm
	modeEditMenu // "e" in the detail: what to edit
	modeEditLine // title, labels
	modeEditText // description, comment
	modeEditPriority
	modeAssignee
	modeCommentPick
	modeCommentDelete
	modeCreate // "n": the form that creates an issue
	modeHelp
)

// --- async messages ---

type issuesMsg struct {
	issues []jira.Issue
	err    error
}
type detailMsg struct {
	issue jira.Issue
	err   error
}
type transitionsMsg struct {
	key         string
	transitions []jira.Transition
	err         error
	forColumn   []string // non-nil: auto-pick a transition landing in these statuses (board move)
}
type doneMsg struct {
	info   string
	err    error
	reload bool
}
type boardsMsg struct {
	boards []jira.Board
	err    error
}
type boardMsg struct {
	board  jira.Board
	cols   []jira.Column
	issues []jira.Issue
	err    error
}

type worklogsMsg struct {
	key string
	ws  []tempo.Worklog
	err error
}
type loggedMsg struct {
	key  string
	text string
	err  error
}

// TimeService is the Tempo surface the UI depends on (nil = not configured).
type TimeService interface {
	Worklogs(issueID string) ([]tempo.Worklog, error)
	AddWorklog(issueID string, seconds int, date, description string) error
}

// RetryMsg is sent when a rate-limited request is waiting to be retried.
type RetryMsg struct {
	Wait    time.Duration
	Attempt int
}

type Model struct {
	svc  jira.Service
	cfg  config.Config
	spin spinner.Model

	transparent bool // keep the terminal's own background

	w, h         int // usable size, i.e. the terminal minus the margins
	mx, my       int // horizontal / vertical margin
	view         viewKind
	back         viewKind // view to return to from the detail
	mode         modeKind
	loading      int
	pendingOpen  string // ticket to open as soon as it is loaded (jira-tui PAY-142)
	startErr     string // error of that open, shown once the list has loaded
	issuesLoaded bool
	holdInfo     bool // keep the confirmation of an action instead of the reload's "N tickets loaded"
	retrying     bool // the status is a "rate limited, retrying" notice
	status       string
	isErr        bool

	// list
	issues   []jira.Issue
	cursor   int
	filter   textinput.Model
	onlyMine bool
	onlyOpen bool
	rawJQL   string // custom JQL (ctrl+j / saved filter); overrides the chips
	jql      textinput.Model
	fcur     int

	// detail
	detail    jira.Issue
	hasDetail bool
	vp        viewport.Model

	// time tracking (Tempo)
	tsvc     TimeService
	hpd      int // hours per day
	worklogs []tempo.Worklog
	wlLoaded bool
	logIn    [3]textinput.Model // duration, date, description
	logFocus int
	logErr   string

	// Jira automations (manual rules)
	asvc      AutomationService
	autoExtra []automation.Rule // declared in config.yaml
	autoIssue jira.Issue
	autoRules []automation.Rule
	autoCur   int
	autoNote  string
	autoRule  automation.Rule
	autoVals  map[string]string
	autoIn    []textinput.Model
	autoFocus int
	autoErr   string

	// editing the ticket (edit.go)
	ed editState

	// creating a ticket (create.go)
	cr          createState
	lastProject string // project key of the last ticket created in this session

	// modals
	trans           []jira.Transition
	tcur            int
	comment         textarea.Model
	commentInternal bool              // false = visible by the customer (the default)
	cmtMentions     map[string]string // name -> account id of the mentions typed in the new comment
	sg              suggest           // "@" / "#" picker of the text area being edited
	boards          []jira.Board
	bcur            int

	// board
	board   jira.Board
	cols    []jira.Column
	bissues []jira.Issue
	bc, br  int // focused column / row
}

func New(svc jira.Service, cfg config.Config) Model {
	fi := textinput.New()
	fi.Prompt = ""
	fi.Placeholder = i18n.T("Filtrer les tickets…", "Filter tickets…")
	fi.SetVirtualCursor(true)
	fi.SetStyles(inputStyles(fi.Styles()))

	ji := textinput.New()
	ji.Prompt = "JQL> "
	ji.SetValue(cfg.JQL)
	ji.SetVirtualCursor(true)
	ji.SetStyles(inputStyles(ji.Styles()))

	raw := ""
	if cfg.JQL != config.DefaultJQL {
		raw = cfg.JQL
	}
	sp := spinner.New(spinner.WithSpinner(spinner.MiniDot), spinner.WithStyle(lipgloss.NewStyle().Foreground(colLav)))
	var logs [3]textinput.Model
	for i, ph := range []string{"1h30, 45m, 2d…", i18n.T("aujourd'hui (ou hier, 04/11, 2026-11-04)", "today (or yesterday, 04/11, 2026-11-04)"), i18n.T("Sur quoi avez-vous travaillé ? (facultatif)", "What did you work on? (optional)")} {
		ti := textinput.New()
		ti.Prompt = ""
		ti.Placeholder = ph
		ti.SetWidth(48)
		ti.SetVirtualCursor(true)
		ti.SetStyles(inputStyles(ti.Styles()))
		logs[i] = ti
	}
	return Model{
		logIn: logs, hpd: 8, ed: newEditState(), cr: newCreateState(), cmtMentions: map[string]string{},
		svc: svc, cfg: cfg, spin: sp, loading: 1,
		filter: fi, jql: ji, comment: newComment(), vp: viewport.New(),
		onlyMine: true, onlyOpen: true, rawJQL: raw,
	}
}

// WithTime enables Tempo time tracking (svc == nil leaves it disabled).
func (m Model) WithTime(svc TimeService, hoursPerDay int) Model {
	m.tsvc = svc
	if hoursPerDay > 0 {
		m.hpd = hoursPerDay
	}
	return m
}

// Transparent keeps the terminal's own background instead of painting the palette's.
func (m Model) Transparent(on bool) Model { m.transparent = on; return m }

func inputStyles(s textinput.Styles) textinput.Styles {
	for _, st := range []*textinput.StyleState{&s.Focused, &s.Blurred} {
		st.Text = lipgloss.NewStyle().Foreground(colFg)
		st.Placeholder = lipgloss.NewStyle().Foreground(colSub)
		st.Prompt = lipgloss.NewStyle().Foreground(colPink).Bold(true)
	}
	return s
}

func newComment() textarea.Model {
	ta := textarea.New()
	ta.Placeholder = i18n.T("Écrire un commentaire…", "Write a comment…")
	ta.ShowLineNumbers = false
	ta.Prompt = ""
	st := textarea.DefaultDarkStyles()
	bg := lipgloss.NewStyle().Background(colInput)
	for _, s := range []*textarea.StyleState{&st.Focused, &st.Blurred} {
		s.Base = bg
		s.Text = bg.Foreground(colFg)
		s.CursorLine = bg.Foreground(colFg)
		s.Placeholder = bg.Foreground(colSub)
		s.EndOfBuffer = bg
		s.Prompt = bg
	}
	ta.SetStyles(st)
	return ta
}

// OpenIssue makes the app open on the detail of key as soon as it is loaded
// (the list still loads in the background and esc goes back to it).
func (m Model) OpenIssue(key string) Model {
	m.pendingOpen = key
	m.loading++
	return m
}

func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.spin.Tick, m.cmdSearch(m.jqlString())}
	if m.pendingOpen != "" {
		cmds = append(cmds, m.cmdDetail(m.pendingOpen))
	}
	return tea.Batch(cmds...)
}

// jqlString builds the query from the chips unless a custom JQL is active.
func (m Model) jqlString() string {
	if m.rawJQL != "" {
		return m.rawJQL
	}
	var c []string
	if m.onlyMine {
		c = append(c, "assignee = currentUser()")
	}
	if m.onlyOpen {
		c = append(c, "resolution = Unresolved")
	}
	if len(c) == 0 { // Jira Cloud rejects unbounded queries
		return "updated >= -30d ORDER BY updated DESC"
	}
	return strings.Join(c, " AND ") + " ORDER BY updated DESC"
}

// visible returns the issues matching the text filter.
func (m Model) visible() []jira.Issue {
	q := strings.ToLower(strings.TrimSpace(m.filter.Value()))
	if q == "" {
		return m.issues
	}
	var out []jira.Issue
	for _, is := range m.issues {
		hay := strings.ToLower(is.Key + " " + is.Fields.Summary + " " + assigneeName(is.Fields.Assignee))
		if strings.Contains(hay, q) {
			out = append(out, is)
		}
	}
	return out
}

func (m Model) projectKey() string {
	if k := jira.ProjectKey(m.issues); k != "" {
		return k
	}
	if k := jira.ProjectKey(m.bissues); k != "" {
		return k
	}
	return "Jira"
}

// --- layout ---

// margins keeps the UI off the terminal edges, shrinking on small terminals.
func margins(w, h int) (mx, my int) {
	mx, my = 2, 1
	if w < 90 {
		mx = 1
	}
	if w < 60 {
		mx = 0
	}
	if h < 24 {
		my = 0
	}
	return
}

// footerShortcutLines is 2 when there is room, else 1 (essentials only).
func (m Model) footerShortcutLines() int {
	if m.h >= 22 {
		return 2
	}
	return 1
}

// footerH is the status line plus the shortcut lines.
func (m Model) footerH() int { return 1 + m.footerShortcutLines() }

func (m Model) bodyH() int { return max(m.h-1-m.footerH(), 5) } // header(1) + footer

func (m Model) detailWidths() (leftW, rightW int) {
	rightW = 34
	if m.w < 96 {
		rightW = 0
	}
	leftW = m.w
	if rightW > 0 {
		leftW = m.w - rightW - 1
	}
	return
}

func (m *Model) relayout() {
	leftW, _ := m.detailWidths()
	panelH := max(m.bodyH()-2-6, 4) // title+badges above, comment block (6) below
	m.vp.SetWidth(max(leftW-4, 10))
	m.vp.SetHeight(max(panelH-2, 2))
	m.comment.SetWidth(max(leftW-4, 10))
	m.comment.SetHeight(3)
	m.jql.SetWidth(max(min(m.w-16, 70), 20))
	if m.hasDetail {
		m.vp.SetContent(renderDetail(m.detail, max(leftW-4, 10), m.timeView()))
	}
}

// --- commands ---

func (m *Model) busy(c tea.Cmd) tea.Cmd { m.loading++; return c }

func (m Model) cmdSearch(jql string) tea.Cmd {
	return func() tea.Msg {
		is, err := m.svc.Search(jql)
		return issuesMsg{is, err}
	}
}

func (m Model) cmdDetail(key string) tea.Cmd {
	return func() tea.Msg {
		is, err := m.svc.GetIssue(key)
		return detailMsg{is, err}
	}
}

func (m Model) cmdTransitions(key string, forColumn []string) tea.Cmd {
	return func() tea.Msg {
		ts, err := m.svc.Transitions(key)
		return transitionsMsg{key, ts, err, forColumn}
	}
}

func (m Model) cmdDo(info string, reload bool, f func() error) tea.Cmd {
	return func() tea.Msg { return doneMsg{info, f(), reload} }
}

func (m Model) cmdBoards() tea.Cmd {
	return func() tea.Msg {
		b, err := m.svc.Boards()
		return boardsMsg{b, err}
	}
}

func (m Model) cmdBoard(b jira.Board) tea.Cmd {
	return func() tea.Msg {
		cols, err := m.svc.BoardColumns(b.ID)
		if err != nil {
			return boardMsg{board: b, err: err}
		}
		is, err := m.svc.BoardIssues(b.ID)
		return boardMsg{b, cols, is, err}
	}
}

func (m Model) cmdWorklogs(issueID, key string) tea.Cmd {
	svc := m.tsvc
	return func() tea.Msg {
		ws, err := svc.Worklogs(issueID)
		return worklogsMsg{key, ws, err}
	}
}

func (m Model) cmdLog(issueID, key string, secs int, date, desc string) tea.Cmd {
	svc := m.tsvc
	return func() tea.Msg {
		err := svc.AddWorklog(issueID, secs, date, desc)
		return loggedMsg{key, i18n.F("%s ajoutés à %s", "%s added to %s", tempo.FormatDuration(secs), key), err}
	}
}

func openURL(u string) tea.Cmd {
	return func() tea.Msg {
		_ = exec.Command("xdg-open", u).Start()
		return nil
	}
}

func (m *Model) setErr(err error) { m.status, m.isErr, m.retrying = err.Error(), true, false }
func (m *Model) setInfo(s string) { m.status, m.isErr, m.retrying = s, false, false }

// noTempoMsg explains how to enable time tracking.
func noTempoMsg() string {
	return i18n.T("Tempo non configuré — ajoutez tempo_token dans config.yaml (ou TEMPO_TOKEN)", "Tempo not configured — add tempo_token to config.yaml (or TEMPO_TOKEN)")
}

// search reloads the list with the current chips / custom JQL.
func (m *Model) search() tea.Cmd {
	m.cursor = 0
	return m.busy(m.cmdSearch(m.jqlString()))
}

// show switches tab; the board is loaded on first use.
func (m *Model) show(v viewKind) tea.Cmd {
	switch v {
	case viewBoard:
		if len(m.cols) == 0 {
			return m.busy(m.cmdBoards())
		}
	case viewDetail:
		if !m.hasDetail {
			return nil
		}
	}
	m.view = v
	return nil
}

func (m *Model) openDetail(is jira.Issue) tea.Cmd {
	if m.view != viewDetail {
		m.back = m.view
	}
	m.detail, m.hasDetail, m.view = is, true, viewDetail
	m.worklogs, m.wlLoaded = nil, false
	m.relayout()
	m.vp.GotoTop()
	return m.reloadDetail()
}

// reloadDetail refreshes the open ticket and its Tempo worklogs.
func (m *Model) reloadDetail() tea.Cmd {
	cmds := []tea.Cmd{m.busy(m.cmdDetail(m.detail.Key))}
	if m.tsvc != nil && m.detail.ID != "" {
		cmds = append(cmds, m.busy(m.cmdWorklogs(m.detail.ID, m.detail.Key)))
	}
	return tea.Batch(cmds...)
}

// currentKey returns the issue key under focus in the current view.
func (m Model) currentKey() string {
	switch m.view {
	case viewDetail:
		return m.detail.Key
	case viewBoard:
		if is := m.columnIssues(m.bc); m.br < len(is) {
			return is[m.br].Key
		}
	default:
		if vis := m.visible(); m.cursor < len(vis) {
			return vis[m.cursor].Key
		}
	}
	return ""
}

// --- update ---

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.mx, m.my = margins(msg.Width, msg.Height)
		m.w, m.h = msg.Width-2*m.mx, msg.Height-2*m.my
		m.filter.SetWidth(max(m.w/3, 10))
		m.relayout()
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd

	case RetryMsg:
		m.setInfo(i18n.F("limite de Jira atteinte — nouvelle tentative dans %s (%d/3)", "Jira rate limit reached — retrying in %s (%d/3)", msg.Wait.Round(time.Second), msg.Attempt))
		m.retrying = true
		return m, nil

	case issuesMsg:
		m.loading--
		if msg.err != nil {
			m.setErr(msg.err)
			return m, nil
		}
		m.issues = msg.issues
		m.issuesLoaded = true
		m.cursor = min(m.cursor, max(len(m.visible())-1, 0))
		if m.holdInfo {
			m.holdInfo = false
		} else {
			m.setInfo(i18n.F("%d tickets chargés", "%d tickets loaded", len(m.issues)))
		}
		if m.startErr != "" {
			m.status, m.isErr, m.startErr = m.startErr, true, ""
		}
		return m, nil

	case detailMsg:
		m.loading--
		if msg.err != nil {
			if m.pendingOpen != "" { // the ticket asked on the command line
				m.pendingOpen = ""
				if !m.issuesLoaded { // do not let "N tickets chargés" hide the error
					m.startErr = msg.err.Error()
					return m, nil
				}
			}
			m.setErr(msg.err)
			return m, nil
		}
		if m.pendingOpen != "" && msg.issue.Key == m.pendingOpen {
			m.pendingOpen = ""
			m.back = viewList
			m.detail, m.hasDetail, m.view = msg.issue, true, viewDetail
			m.worklogs, m.wlLoaded = nil, false
			m.relayout()
			m.vp.GotoTop()
			if m.tsvc != nil && m.detail.ID != "" {
				return m, m.busy(m.cmdWorklogs(m.detail.ID, m.detail.Key))
			}
			return m, nil
		}
		if msg.issue.Key == m.detail.Key {
			m.detail = msg.issue
			m.relayout()
		}
		return m, nil

	case rulesMsg:
		m.onRules(msg)
		return m, nil

	case ranMsg:
		return m, m.onRan(msg)

	case reloadTickMsg:
		m.holdInfo = true // keep the "règle lancée" confirmation
		cmds := []tea.Cmd{m.busy(m.cmdSearch(m.jqlString()))}
		if m.hasDetail {
			cmds = append(cmds, m.busy(m.cmdDetail(m.detail.Key)))
		}
		if len(m.cols) > 0 {
			cmds = append(cmds, m.busy(m.cmdBoard(m.board)))
		}
		return m, tea.Batch(cmds...)

	case worklogsMsg:
		m.loading--
		if msg.err != nil {
			m.setErr(fmt.Errorf("Tempo : %w", msg.err))
			return m, nil
		}
		if msg.key == m.detail.Key {
			tempo.SortNewestFirst(msg.ws)
			m.worklogs, m.wlLoaded = msg.ws, true
			m.relayout()
		}
		return m, nil

	case loggedMsg:
		m.loading--
		if msg.err != nil {
			m.setErr(fmt.Errorf("Tempo : %w", msg.err))
			return m, nil
		}
		m.setInfo(msg.text)
		return m, m.reloadDetail()

	case transitionsMsg:
		m.loading--
		if msg.err != nil {
			m.setErr(msg.err)
			return m, nil
		}
		if msg.forColumn != nil {
			for _, t := range msg.transitions {
				for _, id := range msg.forColumn {
					if t.To.ID == id {
						key, tid, name := msg.key, t.ID, t.To.Name
						return m, m.busy(m.cmdDo(key+" → "+name, true, func() error { return m.svc.DoTransition(key, tid) }))
					}
				}
			}
			m.setErr(errors.New(i18n.T("aucune transition possible vers cette colonne", "no possible transition to this column")))
			return m, nil
		}
		m.trans, m.tcur, m.mode = msg.transitions, 0, modeTransition
		return m, nil

	case doneMsg:
		m.loading--
		if msg.err != nil {
			m.setErr(msg.err)
			return m, nil
		}
		m.setInfo(msg.info)
		if !msg.reload {
			return m, nil
		}
		m.holdInfo = true // the search below must not overwrite the confirmation
		cmds := []tea.Cmd{m.busy(m.cmdSearch(m.jqlString()))}
		if m.hasDetail {
			cmds = append(cmds, m.busy(m.cmdDetail(m.detail.Key)))
		}
		if len(m.cols) > 0 {
			cmds = append(cmds, m.busy(m.cmdBoard(m.board)))
		}
		return m, tea.Batch(cmds...)

	case editMetaMsg:
		m.onEditMeta(msg)
		return m, nil

	case createTypesMsg:
		return m, m.onCreateTypes(msg)

	case createMetaMsg:
		m.onCreateMeta(msg)
		return m, nil

	case projectsMsg:
		m.onProjects(msg)
		return m, nil

	case projectQueryMsg:
		return m, m.onProjectQuery(msg)

	case createdMsg:
		return m, m.onCreated(msg)

	case suggestQueryMsg:
		return m, m.onSuggestQuery(msg)

	case suggestMsg:
		m.onSuggest(msg)
		return m, nil

	case userQueryMsg:
		return m, m.onUserQuery(msg)

	case usersMsg:
		m.onUsers(msg)
		return m, nil

	case boardsMsg:
		m.loading--
		if msg.err != nil {
			m.setErr(msg.err)
			return m, nil
		}
		m.boards, m.bcur = msg.boards, 0
		switch len(m.boards) {
		case 0:
			m.setErr(errors.New(i18n.T("aucun board accessible", "no accessible board")))
		case 1:
			m.bc, m.br = 0, 0
			return m, m.busy(m.cmdBoard(m.boards[0]))
		default:
			m.mode = modeBoards
		}
		return m, nil

	case boardMsg:
		m.loading--
		if msg.err != nil {
			m.setErr(msg.err)
			return m, nil
		}
		m.board, m.cols, m.bissues = msg.board, msg.cols, msg.issues
		m.bc = min(m.bc, max(len(m.cols)-1, 0))
		m.br = min(m.br, max(len(m.columnIssues(m.bc))-1, 0))
		m.view = viewBoard
		return m, nil

	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}

	// forward other messages (cursor blink, ...) to the focused input
	var cmd tea.Cmd
	switch m.mode {
	case modeFilter:
		m.filter, cmd = m.filter.Update(msg)
	case modeJQL:
		m.jql, cmd = m.jql.Update(msg)
	case modeComment:
		m.comment, cmd = m.comment.Update(msg)
	case modeLog:
		m.logIn[m.logFocus], cmd = m.logIn[m.logFocus].Update(msg)
	case modeEditLine, modeAssignee:
		m.ed.line, cmd = m.ed.line.Update(msg)
	case modeEditText:
		m.ed.area, cmd = m.ed.area.Update(msg)
	case modeCreate:
		switch {
		case m.cr.picking:
			m.cr.pquery, cmd = m.cr.pquery.Update(msg)
		case m.cr.focus == fTitle:
			m.cr.title, cmd = m.cr.title.Update(msg)
		case m.cr.focus == fDesc:
			m.cr.desc, cmd = m.cr.desc.Update(msg)
		case m.cr.focus == fLabels:
			m.cr.labels, cmd = m.cr.labels.Update(msg)
		}
	}
	return m, cmd
}

func (m Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	k := msg.String()
	if k == "ctrl+c" {
		return m, tea.Quit
	}
	switch m.mode {
	case modeFilter:
		return m.keyFilter(msg)
	case modeJQL:
		return m.keyJQL(msg)
	case modeFilters:
		return m.keyFilters(k)
	case modeTransition:
		return m.keyTransition(k)
	case modeComment:
		return m.keyComment(msg)
	case modeBoards:
		return m.keyBoards(k)
	case modeLog:
		return m.keyLog(msg)
	case modeAutoList:
		return m.keyAutoList(k)
	case modeAutoInputs:
		return m.keyAutoInputs(msg)
	case modeAutoConfirm:
		return m.keyAutoConfirm(k)
	case modeEditMenu:
		return m.keyEditMenu(k)
	case modeEditLine:
		return m.keyEditLine(msg)
	case modeEditText:
		return m.keyEditText(msg)
	case modeEditPriority:
		return m.keyEditPriority(k)
	case modeAssignee:
		return m.keyAssignee(msg)
	case modeCommentPick:
		return m.keyCommentPick(k)
	case modeCommentDelete:
		return m.keyCommentDelete(k)
	case modeCreate:
		return m.keyCreate(msg)
	case modeHelp:
		m.mode = modeNormal
		return m, nil
	}

	// global keys
	switch k {
	case "q":
		return m, tea.Quit
	case "?":
		m.mode = modeHelp
		return m, nil
	case "1":
		return m, m.show(viewList)
	case "2":
		return m, m.show(viewBoard)
	case "3":
		return m, m.show(viewDetail)
	case "tab":
		next := (m.view + 1) % 3
		if next == viewDetail && !m.hasDetail {
			next = viewList
		}
		return m, m.show(next)
	case "b":
		if m.view != viewDetail {
			return m, m.busy(m.cmdBoards())
		}
	case "s":
		if key := m.currentKey(); key != "" {
			return m, m.busy(m.cmdTransitions(key, nil))
		}
		return m, nil
	case "o":
		if key := m.currentKey(); key != "" {
			return m, openURL(m.svc.BrowseURL(key))
		}
		return m, nil
	case "x":
		return m, m.startAutomations()
	case "n":
		return m, m.startCreate()
	}

	switch m.view {
	case viewList:
		return m.keyList(k)
	case viewDetail:
		return m.keyDetail(msg)
	case viewBoard:
		return m.keyBoard(k)
	}
	return m, nil
}

func (m Model) keyFilter(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.filter.SetValue("")
		m.filter.Blur()
		m.mode, m.cursor = modeNormal, 0
		return m, nil
	case "enter":
		m.filter.Blur()
		m.mode = modeNormal
		return m, nil
	case "up":
		m.cursor = max(m.cursor-1, 0)
		return m, nil
	case "down":
		m.cursor = min(m.cursor+1, max(len(m.visible())-1, 0))
		return m, nil
	}
	before := m.filter.Value()
	var cmd tea.Cmd
	m.filter, cmd = m.filter.Update(msg)
	if m.filter.Value() != before {
		m.cursor = 0
	}
	return m, cmd
}

func (m Model) keyJQL(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.jql.Blur()
		m.mode = modeNormal
		return m, nil
	case "enter":
		m.jql.Blur()
		m.mode = modeNormal
		m.rawJQL = strings.TrimSpace(m.jql.Value())
		return m, m.search()
	}
	var cmd tea.Cmd
	m.jql, cmd = m.jql.Update(msg)
	return m, cmd
}

func (m Model) keyFilters(k string) (tea.Model, tea.Cmd) {
	n := len(m.cfg.Filters)
	switch k {
	case "esc", "f", "q":
		m.mode = modeNormal
	case "up", "k":
		m.fcur = max(m.fcur-1, 0)
	case "down", "j":
		m.fcur = min(m.fcur+1, n-1)
	case "enter":
		if n > 0 {
			m.rawJQL = m.cfg.Filters[m.fcur].JQL
			m.jql.SetValue(m.rawJQL)
			m.mode = modeNormal
			return m, m.search()
		}
	}
	return m, nil
}

func (m Model) keyTransition(k string) (tea.Model, tea.Cmd) {
	switch k {
	case "esc", "q":
		m.mode = modeNormal
	case "up", "k":
		m.tcur = max(m.tcur-1, 0)
	case "down", "j":
		m.tcur = min(m.tcur+1, len(m.trans)-1)
	case "enter":
		if len(m.trans) == 0 {
			return m, nil
		}
		t, key := m.trans[m.tcur], m.currentKey()
		m.mode = modeNormal
		return m, m.busy(m.cmdDo(key+" → "+t.To.Name, true, func() error { return m.svc.DoTransition(key, t.ID) }))
	}
	return m, nil
}

func (m Model) keyComment(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	consumed, scmd := m.suggestKey(msg, &m.comment)
	if consumed {
		return m, scmd
	}
	switch msg.String() {
	case "esc":
		m.comment.Blur()
		m.mode = modeNormal
		return m, nil
	case "tab": // customer-visible <-> internal
		m.commentInternal = !m.commentInternal
		return m, nil
	case "ctrl+s":
		text := strings.TrimSpace(m.comment.Value())
		if text == "" {
			return m, nil
		}
		key := m.detail.Key
		body := m.converter(m.cmtMentions).ToADF(text)
		clear(m.cmtMentions) // the draft is gone, so are its mentions
		m.comment.Blur()
		m.comment.Reset()
		m.mode = modeNormal
		internal := m.commentInternal
		info := i18n.F("commentaire client ajouté à %s", "customer comment added to %s", key)
		if internal {
			info = i18n.F("commentaire interne ajouté à %s", "internal comment added to %s", key)
		}
		return m, m.busy(m.cmdDo(info, true, func() error { return m.svc.AddComment(key, body, internal) }))
	}
	var cmd tea.Cmd
	m.comment, cmd = m.comment.Update(msg)
	return m, tea.Batch(cmd, scmd)
}

func (m Model) keyBoards(k string) (tea.Model, tea.Cmd) {
	switch k {
	case "esc", "q":
		m.mode = modeNormal
	case "up", "k":
		m.bcur = max(m.bcur-1, 0)
	case "down", "j":
		m.bcur = min(m.bcur+1, len(m.boards)-1)
	case "enter":
		m.mode = modeNormal
		m.bc, m.br = 0, 0
		return m, m.busy(m.cmdBoard(m.boards[m.bcur]))
	}
	return m, nil
}

func (m Model) keyList(k string) (tea.Model, tea.Cmd) {
	vis := m.visible()
	switch k {
	case "up", "k":
		m.cursor = max(m.cursor-1, 0)
	case "down", "j":
		m.cursor = min(m.cursor+1, max(len(vis)-1, 0))
	case "pgup":
		m.cursor = max(m.cursor-10, 0)
	case "pgdown":
		m.cursor = min(m.cursor+10, max(len(vis)-1, 0))
	case "g":
		m.cursor = 0
	case "G":
		m.cursor = max(len(vis)-1, 0)
	case "/":
		m.mode = modeFilter
		return m, m.filter.Focus()
	case "esc":
		if m.filter.Value() != "" {
			m.filter.SetValue("")
			m.cursor = 0
		}
	case "ctrl+j", "J":
		m.mode = modeJQL
		return m, m.jql.Focus()
	case "f":
		if len(m.cfg.Filters) > 0 {
			m.mode = modeFilters
		}
	case "m":
		m.rawJQL, m.onlyMine = "", !m.onlyMine
		return m, m.search()
	case "O":
		m.rawJQL, m.onlyOpen = "", !m.onlyOpen
		return m, m.search()
	case "r":
		return m, m.search()
	case "enter", "l":
		if m.cursor < len(vis) {
			return m, m.openDetail(vis[m.cursor])
		}
	}
	return m, nil
}

func (m Model) keyDetail(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := m.detail.Key
	switch msg.String() {
	case "esc", "h", "backspace":
		m.view = m.back
		if m.back == viewDetail {
			m.view = viewList
		}
	case "c":
		m.mode, m.commentInternal = modeComment, false // always start on "Client"
		return m, m.comment.Focus()
	case "a":
		return m, m.busy(m.cmdDo(i18n.F("%s assigné à vous", "%s assigned to you", key), true, func() error { return m.svc.AssignToMe(key) }))
	case "u":
		return m, m.busy(m.cmdDo(i18n.F("%s désassigné", "%s unassigned", key), true, func() error { return m.svc.Unassign(key) }))
	case "e":
		return m, m.startEdit()
	case "t":
		if m.tsvc == nil {
			m.setErr(errors.New(noTempoMsg()))
			return m, nil
		}
		for i := range m.logIn {
			m.logIn[i].SetValue("")
		}
		m.logErr, m.mode = "", modeLog
		return m, m.focusLog(0)
	case "r":
		return m, m.reloadDetail()
	default:
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(msg)
		return m, cmd
	}
	return m, nil
}

// --- view ---

func (m Model) View() tea.View {
	if m.w == 0 {
		return tea.NewView("")
	}
	var body string
	switch m.view {
	case viewList:
		body = m.viewList()
	case viewBoard:
		body = m.viewBoard()
	case viewDetail:
		body = m.viewDetail()
	}
	screen := lipgloss.JoinVertical(lipgloss.Left, m.header(), fit(body, m.bodyH()), m.footer())
	switch m.mode {
	case modeJQL, modeFilters, modeTransition, modeBoards, modeLog, modeAutoList, modeAutoInputs, modeAutoConfirm,
		modeEditMenu, modeEditLine, modeEditText, modeEditPriority, modeAssignee, modeCommentPick, modeCommentDelete, modeCreate, modeHelp:
		screen = lipgloss.Place(m.w, m.h, lipgloss.Center, lipgloss.Center,
			boxStyle(colPurple).Render(m.modalContent()),
			lipgloss.WithWhitespaceStyle(lipgloss.NewStyle().Foreground(colBorder)))
	}
	screen = lipgloss.NewStyle().Padding(m.my, m.mx).Render(screen)
	v := tea.NewView(screen)
	v.AltScreen = true
	if !m.transparent {
		v.BackgroundColor = colBg
		v.ForegroundColor = colFg
	}
	return v
}

func (m Model) header() string {
	logo := lipgloss.NewStyle().Background(colPurple).Foreground(colWhite).Bold(true).Padding(0, 1).Render("JIRA")
	left := logo + "  " + txt(colFg, m.projectKey())
	switch {
	case m.view == viewDetail:
		left += "  " + txt(colSub, "›") + "  " + txt(colFg, m.detail.Key)
	case m.board.Name != "":
		left += "  " + txt(colSub, "·") + "  " + txt(colSub, m.board.Name)
	}

	tab := func(label string, v viewKind, enabled bool) string {
		switch {
		case m.view == v:
			return lipgloss.NewStyle().Foreground(colWhite).Bold(true).Underline(true).UnderlineColor(colPink).Render(label)
		case !enabled:
			return lipgloss.NewStyle().Foreground(colBorder).Render(label)
		}
		return txt(colSub, label)
	}
	tabs := tab(i18n.T("1 Liste", "1 List"), viewList, true) + "   " + tab("2 Kanban", viewBoard, true) + "   " + tab(i18n.T("3 Détail", "3 Detail"), viewDetail, m.hasDetail)

	gap := max(m.w-lipgloss.Width(left)-lipgloss.Width(tabs), 1)
	return left + strings.Repeat(" ", gap) + tabs
}

func hint(k, label string) string { return bold(colLav, k) + " " + txt(colSub, label) }

// hintLines returns the shortcuts of the current mode, one slice per footer line.
// The first line holds the essentials, the second the actions.
func (m Model) hintLines() [][]string {
	mine, open := i18n.T("assigné à moi", "assigned to me"), i18n.T("seulement ouverts", "open only")
	if m.onlyMine {
		mine = i18n.T("tous les assignés", "all assignees")
	}
	if m.onlyOpen {
		open = i18n.T("tous les statuts", "all statuses")
	}
	quit, help, back := i18n.T("quitter", "quit"), i18n.T("aide", "help"), i18n.T("retour", "back")
	nav, confirm, cancel := i18n.T("naviguer", "navigate"), i18n.T("valider", "confirm"), i18n.T("annuler", "cancel")
	next, send, jiraOpen := i18n.T("champ suivant", "next field"), i18n.T("envoyer", "send"), i18n.T("ouvrir dans Jira", "open in Jira")
	changeStatus, autos := i18n.T("changer le statut", "change status"), i18n.T("automatisations", "automations")
	refresh := i18n.T("rafraîchir", "refresh")
	switch {
	case m.mode == modeCreate:
		if m.sg.active && m.cr.focus == fDesc {
			return [][]string{{hint("↑/↓", i18n.T("choisir", "choose")), hint("enter", i18n.T("insérer", "insert")), hint("esc", i18n.T("fermer la liste", "close the list"))}}
		}
		return [][]string{{hint("tab", next), hint("ctrl+s", i18n.T("créer", "create")), hint("esc", cancel)}}
	case m.sg.active && (m.mode == modeComment || m.mode == modeEditText):
		return [][]string{{hint("↑/↓", i18n.T("choisir", "choose")), hint("enter", i18n.T("insérer", "insert")), hint("esc", i18n.T("fermer la liste", "close the list"))}}
	case m.mode == modeFilter:
		return [][]string{{hint("↑/↓", nav), hint("enter", confirm), hint("esc", i18n.T("effacer", "clear"))}}
	case m.mode == modeComment:
		return [][]string{{hint("ctrl+s", send), hint("tab", i18n.T("client/interne", "customer/internal")), hint("esc", cancel)}}
	case m.mode == modeLog:
		return [][]string{{hint("tab", next), hint("enter", confirm), hint("esc", cancel)}}
	case m.mode == modeAutoList:
		return [][]string{{hint("↑/↓", i18n.T("choisir", "choose")), hint("enter", i18n.T("sélectionner", "select")), hint("esc", i18n.T("fermer", "close"))}}
	case m.mode == modeAutoInputs:
		return [][]string{{hint("tab", next), hint("enter", confirm), hint("esc", back)}}
	case m.mode == modeAutoConfirm:
		return [][]string{{hint("enter", i18n.T("lancer", "run")), hint("esc", back)}}
	case m.mode == modeEditMenu, m.mode == modeEditPriority, m.mode == modeCommentPick:
		return [][]string{{hint("↑/↓", i18n.T("choisir", "choose")), hint("enter", i18n.T("sélectionner", "select")), hint("esc", back)}}
	case m.mode == modeEditLine:
		return [][]string{{hint("enter", i18n.T("enregistrer", "save")), hint("esc", cancel)}}
	case m.mode == modeEditText:
		return [][]string{{hint("ctrl+s", i18n.T("enregistrer", "save")), hint("esc", cancel)}}
	case m.mode == modeAssignee:
		return [][]string{{hint("↑/↓", i18n.T("choisir", "choose")), hint("enter", i18n.T("assigner", "assign")), hint("esc", back)}}
	case m.mode == modeCommentDelete:
		return [][]string{{hint("enter", i18n.T("supprimer", "delete")), hint("esc", back)}}
	case m.view == viewDetail:
		return [][]string{
			{hint("↑/↓", i18n.T("défiler", "scroll")), hint("c", i18n.T("commenter", "comment")), hint("ctrl+s", send), hint("esc", back), hint("q", quit), hint("?", help)},
			{hint("e", i18n.T("modifier", "edit")), hint("n", i18n.T("nouveau", "new")), hint("t", i18n.T("ajouter du temps", "add time")), hint("s", changeStatus), hint("x", autos), hint("a", i18n.T("m'assigner", "assign to me")), hint("u", i18n.T("désassigner", "unassign")), hint("o", jiraOpen), hint("r", i18n.T("recharger", "reload"))},
		}
	case m.view == viewBoard:
		return [][]string{
			{hint("←/→", i18n.T("colonne", "column")), hint("↑/↓", i18n.T("carte", "card")), hint("enter", i18n.T("ouvrir", "open")), hint("n", i18n.T("nouveau", "new")), hint("tab", i18n.T("liste", "list")), hint("q", quit), hint("?", help)},
			{hint("H/L", i18n.T("déplacer le ticket", "move ticket")), hint("s", changeStatus), hint("x", autos), hint("b", "board"), hint("o", jiraOpen), hint("r", refresh)},
		}
	}
	return [][]string{
		{hint("↑/↓", nav), hint("enter", i18n.T("ouvrir", "open")), hint("n", i18n.T("nouveau", "new")), hint("/", i18n.T("filtrer", "filter")), hint("tab", "kanban"), hint("r", refresh), hint("q", quit), hint("?", help)},
		{hint("m", mine), hint("O", open), hint("s", i18n.T("statut", "status")), hint("ctrl+j", "JQL"), hint("f", i18n.T("filtres", "filters")), hint("b", "board"), hint("x", "automation"), hint("o", "Jira")},
	}
}

// fitHints joins items with " · ", dropping trailing ones until they fit in w cells.
func fitHints(items []string, w int) string {
	sep := txt(colSub, " · ")
	for len(items) > 1 {
		if line := strings.Join(items, sep); lipgloss.Width(line) <= w {
			return line
		}
		items = items[:len(items)-1]
	}
	return truncate(strings.Join(items, sep), w)
}

func (m Model) counter() string {
	switch m.view {
	case viewDetail:
		return fmt.Sprintf("%d %%", int(m.vp.ScrollPercent()*100))
	case viewBoard:
		if len(m.cols) == 0 {
			return ""
		}
		is := m.columnIssues(m.bc)
		if m.br >= len(is) {
			return m.cols[m.bc].Name + " 0/0"
		}
		return fmt.Sprintf("%s · %s %d/%d", is[m.br].Key, m.cols[m.bc].Name, m.br+1, len(is))
	}
	vis := m.visible()
	if m.cursor >= len(vis) {
		return ""
	}
	return fmt.Sprintf("%s · %d/%d", vis[m.cursor].Key, m.cursor+1, len(vis))
}

func (m Model) footer() string {
	msg := ""
	switch {
	case m.loading > 0:
		msg = m.spin.View() + " " + txt(colSub, i18n.T("chargement…", "loading…"))
		if m.status != "" && m.retrying {
			msg = m.spin.View() + " " + txt(colYellow, truncate(m.status, m.w-4))
		}
	case m.status != "" && m.isErr:
		msg = txt(colPink, "✗ "+truncate(m.status, m.w-4))
	case m.status != "":
		msg = txt(colGreen, "✓ ") + txt(colSub, truncate(m.status, m.w-4))
	}
	right := txt(colSub, m.counter())
	lines := m.hintLines()
	n := m.footerShortcutLines()
	if len(lines) > n {
		lines = lines[:n]
	}
	out := []string{msg}
	for i := 0; i < n; i++ {
		var items []string
		if i < len(lines) {
			items = lines[i]
		}
		switch {
		case i == n-1: // counter sits on the last footer line
			out = append(out, spread(fitHints(items, max(m.w-lipgloss.Width(right)-2, 10)), right, m.w, paint{}))
		case len(items) == 0:
			out = append(out, "")
		default:
			out = append(out, fitHints(items, m.w))
		}
	}
	return strings.Join(out, "\n")
}

func (m Model) modalContent() string {
	var sb strings.Builder
	pick := func(sel bool, s string) string {
		if sel {
			return lipgloss.NewStyle().Background(colPurple).Foreground(colWhite).Render("▸ " + s + " ")
		}
		return "  " + s
	}
	switch m.mode {
	case modeJQL:
		sb.WriteString(bold(colLav, i18n.T("Requête JQL", "JQL query")) + "\n\n" + m.jql.View() + "\n\n" +
			txt(colSub, i18n.T("enter lancer · esc annuler · vide = revenir aux puces", "enter run · esc cancel · empty = back to the chips")))
	case modeFilters:
		sb.WriteString(bold(colLav, i18n.T("Filtres", "Filters")) + "\n\n")
		for i, f := range m.cfg.Filters {
			sb.WriteString(pick(i == m.fcur, f.Name) + "\n")
		}
	case modeTransition:
		sb.WriteString(bold(colLav, i18n.T("Changer le statut de ", "Change the status of ")+m.currentKey()) + "\n\n")
		for i, t := range m.trans {
			g, c := statusKind(t.To)
			sb.WriteString(pick(i == m.tcur, t.Name+" "+txt(c, g)) + "\n")
		}
	case modeBoards:
		sb.WriteString(bold(colLav, "Boards") + "\n\n")
		for i, b := range m.boards {
			sb.WriteString(pick(i == m.bcur, b.Name+txt(colSub, "  "+b.Type)) + "\n")
		}
	case modeLog:
		sb.WriteString(m.logContent())
	case modeAutoList, modeAutoInputs, modeAutoConfirm:
		sb.WriteString(m.autoContent())
	case modeEditMenu, modeEditLine, modeEditText, modeEditPriority, modeAssignee, modeCommentPick, modeCommentDelete:
		sb.WriteString(m.editContent())
	case modeCreate:
		sb.WriteString(m.createContent())
	case modeHelp:
		sb.WriteString(helpText())
	}
	return strings.TrimRight(sb.String(), "\n")
}

func helpText() string {
	body := i18n.T(`
 Global   1 liste · 2 kanban · 3 détail · tab changer d'onglet
          n nouveau ticket · s changer le statut · o ouvrir dans Jira
          b choisir un board
          ? aide · q quitter
 Liste    ↑/↓ naviguer · enter ouvrir · / filtrer (texte) · r rafraîchir
          m assigné à moi/tous · O ouverts/tous · ctrl+j requête JQL
          f filtres de la config
 Kanban   ←/→ colonne · ↑/↓ carte · H/L déplacer le ticket
 Auto.    x automatisations du ticket (liste, kanban, détail) : choisir, confirmer
 Détail   ↑/↓ défiler · c commenter (tab client/interne, ctrl+s envoie) · t ajouter du temps (Tempo)
          e modifier (titre, description, priorité, étiquettes, assigné, commentaires)
          a m'assigner
          u désassigner · r recharger · esc retour
`, `
 Global   1 list · 2 kanban · 3 detail · tab switch tab
          n new ticket · s change status · o open in Jira
          b choose a board
          ? help · q quit
 List     ↑/↓ navigate · enter open · / filter (text) · r refresh
          m assigned to me/all · O open/all · ctrl+j JQL query
          f config filters
 Kanban   ←/→ column · ↑/↓ card · H/L move the ticket
 Auto.    x ticket automations (list, kanban, detail): choose, confirm
 Detail   ↑/↓ scroll · c comment (tab customer/internal, ctrl+s sends) · t add time (Tempo)
          e edit (title, description, priority, labels, assignee, comments)
          a assign to me
          u unassign · r reload · esc back
`)
	return bold(colLav, i18n.T("Raccourcis", "Shortcuts")) + "\n" + body + "\n " + txt(colSub, i18n.T("(toute touche pour fermer)", "(any key to close)"))
}
