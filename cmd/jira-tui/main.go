package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/kevin/jira-tui/internal/automation"
	"github.com/kevin/jira-tui/internal/config"
	"github.com/kevin/jira-tui/internal/export"
	"github.com/kevin/jira-tui/internal/i18n"
	"github.com/kevin/jira-tui/internal/jira"
	"github.com/kevin/jira-tui/internal/mock"
	"github.com/kevin/jira-tui/internal/tempo"
	"github.com/kevin/jira-tui/internal/ui"
)

func main() {
	i18n.Set(i18n.Detect(os.Getenv))
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func errLabel() string { return i18n.T("erreur:", "error:") }

// run returns the exit code: 0 ok, 1 error, 2 usage.
func run(args []string, stdout, stderr io.Writer) int {
	o, err := parseArgs(args)
	if errors.Is(err, flag.ErrHelp) {
		printUsage(stdout)
		return 0
	}
	if err != nil {
		fmt.Fprintln(stderr, errLabel(), err)
		fmt.Fprintln(stderr, i18n.T("Aide : jira-tui -h", "Help: jira-tui -h"))
		return 2
	}
	cfg, err := config.Load(o.config)
	if err != nil {
		fmt.Fprintln(stderr, errLabel(), err)
		return 1
	}
	if o.outputJSON {
		return runJSON(o, cfg, stdout, stderr)
	}
	return runTUI(o, cfg, stderr)
}

// services builds the Jira, Tempo (nil unless configured) and Automation clients.
func services(o options, cfg config.Config) (svc jira.Service, tsvc ui.TimeService, asvc ui.AutomationService) {
	if o.demo {
		ms := mock.New()
		return ms, mock.NewTime(), mock.NewAutomations(ms)
	}
	jc := jira.NewClient(cfg.URL, cfg.Email, cfg.Token)
	if cfg.TempoToken != "" {
		tsvc = tempo.NewClient(cfg.TempoURL, cfg.TempoToken, jc.UserName, jc.AccountID)
	}
	asvc = automation.NewClient(cfg.URL, cfg.Email, cfg.Token, cfg.CloudID, cfg.AutomationURL)
	return jc, tsvc, asvc
}

// runJSON never prompts: it prints the ticket as JSON on stdout, everything else goes to stderr.
func runJSON(o options, cfg config.Config, stdout, stderr io.Writer) int {
	if !o.demo {
		if err := cfg.Validate(); err != nil {
			fmt.Fprintln(stderr, errLabel(), err)
			return 1
		}
	}
	jira.Notify = func(wait time.Duration, attempt int) {
		fmt.Fprintf(stderr, i18n.T("limite de Jira atteinte — nouvelle tentative dans %s (%d/3)\n", "Jira rate limit reached — retrying in %s (%d/3)\n"), wait.Round(time.Second), attempt)
	}
	svc, tsvc, _ := services(o, cfg)
	var ts export.TimeService
	if tsvc != nil {
		ts = tsvc
	}
	t, err := export.Build(svc, ts, o.ref)
	if err != nil {
		fmt.Fprintln(stderr, errLabel(), err)
		return 1
	}
	if t.WorklogsError != "" {
		fmt.Fprintln(stderr, i18n.T("avertissement: Tempo :", "warning: Tempo:"), t.WorklogsError)
	}
	if t.CommentsError != "" {
		fmt.Fprintln(stderr, i18n.T("avertissement: commentaires indisponibles :", "warning: comments unavailable:"), t.CommentsError)
	}
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(t); err != nil {
		fmt.Fprintln(stderr, errLabel(), err)
		return 1
	}
	return 0
}

func runTUI(o options, cfg config.Config, stderr io.Writer) int {
	if !o.demo {
		if err := cfg.Validate(); err != nil {
			path := o.config
			if path == "" {
				path = config.DefaultPath()
			}
			if fi, e := os.Stdin.Stat(); e != nil || fi.Mode()&os.ModeCharDevice == 0 {
				fmt.Fprintln(stderr, errLabel(), err) // not interactive: cannot ask
				return 1
			}
			res, e := ui.RunSetup(cfg, path, ui.JiraCheck)
			if e != nil {
				fmt.Fprintln(stderr, errLabel(), e)
				return 1
			}
			if res == nil {
				fmt.Fprintln(stderr, i18n.T("configuration annulée", "configuration cancelled"))
				return 1
			}
			// persist only what the file already had + the verified credentials: never env-provided secrets
			toSave, _ := config.LoadFile(path)
			toSave.URL, toSave.Email, toSave.Token = res.URL, res.Email, res.Token
			if e := config.Save(path, toSave); e != nil {
				fmt.Fprintln(stderr, errLabel(), e)
				return 1
			}
			fmt.Fprintln(stderr, i18n.T("configuration enregistrée dans", "configuration saved in"), path)
			cfg = *res
		}
	}
	svc, tsvc, asvc := services(o, cfg)

	m := ui.New(svc, cfg).Transparent(o.transparent).WithTime(tsvc, cfg.HoursPerDay).WithAutomations(asvc, cfg.Automations)
	if o.ref != "" {
		m = m.OpenIssue(o.ref)
	}
	p := tea.NewProgram(m)
	jira.Notify = func(wait time.Duration, attempt int) { p.Send(ui.RetryMsg{Wait: wait, Attempt: attempt}) }
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(stderr, errLabel(), err)
		return 1
	}
	return 0
}
