package ui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/kevin/jira-tui/internal/config"
	"github.com/kevin/jira-tui/internal/i18n"
	"github.com/kevin/jira-tui/internal/jira"
)

// CheckFunc verifies Jira credentials; it returns the account's display name.
type CheckFunc func(url, email, token string) (string, error)

// JiraCheck is the production CheckFunc.
func JiraCheck(url, email, token string) (string, error) {
	u, err := jira.NewClient(url, email, token).Check()
	return u.DisplayName, err
}

type checkedMsg struct {
	who string
	err error
}

// Setup is the first-run form that collects the Jira credentials.
type Setup struct {
	cfg       config.Config
	path      string
	check     CheckFunc
	inputs    [3]textinput.Model
	focus     int
	busy      bool
	errMsg    string
	spin      spinner.Model
	w, h      int
	Result    *config.Config // set once the credentials were verified
	Cancelled bool
}

func NewSetup(cfg config.Config, path string, check CheckFunc) Setup {
	mk := func(placeholder, value string, secret bool) textinput.Model {
		ti := textinput.New()
		ti.Prompt = ""
		ti.Placeholder = placeholder
		ti.SetValue(value)
		ti.SetVirtualCursor(true)
		ti.SetWidth(46)
		ti.SetStyles(inputStyles(ti.Styles()))
		if secret {
			ti.EchoMode = textinput.EchoPassword
		}
		return ti
	}
	s := Setup{cfg: cfg, path: path, check: check}
	s.inputs[0] = mk(i18n.T("https://votre-site.atlassian.net", "https://your-site.atlassian.net"), cfg.URL, false)
	s.inputs[1] = mk(i18n.T("vous@exemple.fr", "you@example.com"), cfg.Email, false)
	s.inputs[2] = mk(i18n.T("token d'API Atlassian", "Atlassian API token"), cfg.Token, true)
	// start on the first empty field
	for i := range s.inputs {
		if s.inputs[i].Value() == "" {
			s.focus = i
			break
		}
	}
	_ = s.inputs[s.focus].Focus() // set on the real model, not on a copy (Init has a value receiver)
	s.spin = spinner.New(spinner.WithSpinner(spinner.MiniDot), spinner.WithStyle(lipgloss.NewStyle().Foreground(colLav)))
	return s
}

// RunSetup shows the form and returns the verified config (nil if cancelled).
func RunSetup(cfg config.Config, path string, check CheckFunc) (*config.Config, error) {
	final, err := tea.NewProgram(NewSetup(cfg, path, check)).Run()
	if err != nil {
		return nil, err
	}
	return final.(Setup).Result, nil
}

func (s *Setup) setFocus(i int) tea.Cmd {
	s.focus = (i + len(s.inputs)) % len(s.inputs)
	var cmd tea.Cmd
	for j := range s.inputs {
		if j == s.focus {
			cmd = s.inputs[j].Focus()
		} else {
			s.inputs[j].Blur()
		}
	}
	return cmd
}

func (s Setup) Init() tea.Cmd {
	return s.spin.Tick
}

func normalizeURL(u string) string {
	u = strings.TrimSpace(u)
	if u != "" && !strings.Contains(u, "://") {
		u = "https://" + u
	}
	return strings.TrimRight(u, "/")
}

func (s Setup) submit() (tea.Model, tea.Cmd) {
	url := normalizeURL(s.inputs[0].Value())
	email := strings.TrimSpace(s.inputs[1].Value())
	token := strings.TrimSpace(s.inputs[2].Value())
	for i, v := range []string{url, email, token} {
		if v == "" {
			s.errMsg = i18n.T("tous les champs sont obligatoires", "all fields are required")
			return s, s.setFocus(i)
		}
	}
	s.inputs[0].SetValue(url)
	s.busy, s.errMsg = true, ""
	check := s.check
	return s, func() tea.Msg {
		who, err := check(url, email, token)
		return checkedMsg{who, err}
	}
}

func (s Setup) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		s.w, s.h = msg.Width, msg.Height
		return s, nil
	case spinner.TickMsg:
		var cmd tea.Cmd
		s.spin, cmd = s.spin.Update(msg)
		return s, cmd
	case checkedMsg:
		s.busy = false
		if msg.err != nil {
			s.errMsg = msg.err.Error()
			return s, nil
		}
		c := s.cfg
		c.URL = normalizeURL(s.inputs[0].Value())
		c.Email = strings.TrimSpace(s.inputs[1].Value())
		c.Token = strings.TrimSpace(s.inputs[2].Value())
		s.Result = &c
		return s, tea.Quit
	case tea.KeyPressMsg:
		if s.busy {
			if msg.String() == "ctrl+c" {
				s.Cancelled = true
				return s, tea.Quit
			}
			return s, nil
		}
		switch msg.String() {
		case "ctrl+c", "esc":
			s.Cancelled = true
			return s, tea.Quit
		case "tab", "down":
			return s, s.setFocus(s.focus + 1)
		case "shift+tab", "up":
			return s, s.setFocus(s.focus - 1)
		case "enter":
			if s.focus < len(s.inputs)-1 {
				return s, s.setFocus(s.focus + 1)
			}
			return s.submit()
		}
	}
	var cmd tea.Cmd
	s.inputs[s.focus], cmd = s.inputs[s.focus].Update(msg)
	return s, cmd
}

func (s Setup) View() tea.View {
	labels := []string{i18n.T("URL du site Jira", "Jira site URL"), i18n.T("Email du compte Atlassian", "Atlassian account email"), i18n.T("Token d'API", "API token")}
	var sb strings.Builder
	sb.WriteString(lipgloss.NewStyle().Background(colPurple).Foreground(colWhite).Bold(true).Padding(0, 1).Render("JIRA") +
		"  " + bold(colWhite, i18n.T("Première configuration", "First-time setup")) + "\n\n")
	sb.WriteString(txt(colSub, i18n.T("Aucune configuration trouvée. Renseignez vos identifiants,\nils seront enregistrés dans :", "No configuration found. Enter your credentials,\nthey will be saved in:")) + "\n")
	sb.WriteString(txt(colFg, s.path) + "\n\n")
	for i, in := range s.inputs {
		border := colBorder
		if i == s.focus {
			border = colPurple
		}
		sb.WriteString(bold(colLav, labels[i]) + "\n")
		sb.WriteString(boxStyle(border).Width(50).Render(in.View()) + "\n")
		if i == 2 {
			sb.WriteString(txt(colSub, i18n.T("À créer sur ", "Create one at ")+"id.atlassian.com/manage-profile/security/api-tokens") + "\n")
		}
	}
	sb.WriteString("\n")
	switch {
	case s.busy:
		sb.WriteString(s.spin.View() + " " + txt(colSub, i18n.T("vérification auprès de Jira…", "checking with Jira…")))
	case s.errMsg != "":
		sb.WriteString(txt(colPink, "✗ "+truncate(s.errMsg, 60)))
	default:
		sb.WriteString(" ")
	}
	sb.WriteString("\n" + hint("tab", i18n.T("champ suivant", "next field")) + txt(colSub, " · ") + hint("enter", i18n.T("valider", "confirm")) + txt(colSub, " · ") + hint("esc", i18n.T("annuler", "cancel")))

	out := lipgloss.NewStyle().Width(66).Render(sb.String()) // pad lines so Place centers the block, not each line
	if s.w > 0 {
		out = lipgloss.Place(s.w, s.h, lipgloss.Center, lipgloss.Center, out)
	}
	v := tea.NewView(fmt.Sprint(out))
	v.AltScreen = true
	v.BackgroundColor = colBg
	v.ForegroundColor = colFg
	return v
}
