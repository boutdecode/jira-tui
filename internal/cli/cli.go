// Package cli implements the non-interactive commands of jira-tui (list, show, comment,
// transition, assign, edit, create, log, auto, whoami, completion).
//
// They never prompt: everything comes from arguments, options or standard input. Results go
// to standard output (text, or JSON with --output-json), errors to standard error. Exit
// codes: 0 ok, 1 failure (network, authentication, not found, ambiguous choice…), 2 usage.
package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/kevin/jira-tui/internal/automation"
	"github.com/kevin/jira-tui/internal/config"
	"github.com/kevin/jira-tui/internal/i18n"
	"github.com/kevin/jira-tui/internal/jira"
	"github.com/kevin/jira-tui/internal/tempo"
)

// TimeService is the Tempo surface the commands need (nil in Deps = not configured).
type TimeService interface {
	Worklogs(issueID string) ([]tempo.Worklog, error)
	AddWorklog(issueID string, seconds int, date, description string) error
}

// AutomationService is the Jira Automation surface (nil in Deps = unavailable).
type AutomationService interface {
	List(issueID string) ([]automation.Rule, error)
	Run(rule automation.Rule, issueID string, values map[string]string) error
}

// Deps are the services a command works with.
type Deps struct {
	Svc    jira.Service
	Time   TimeService
	Auto   AutomationService
	Config config.Config
	Now    func() time.Time // time.Now when nil
}

// Builder creates the Deps once the common options are known (it loads the configuration, and
// returns an error when it is incomplete). Commands that fail on usage never call it.
type Builder func(configPath string, demo bool) (Deps, error)

// IO is the process environment.
type IO struct {
	Stdin      io.Reader
	StdinIsTTY bool
	Stdout     io.Writer
	Stderr     io.Writer
}

// runner carries what a running command needs.
type runner struct {
	io    IO
	build Builder
	name  string
	spec  *spec

	configPath string
	demo       bool
	asJSON     bool
	dryRun     bool
	deps       *Deps
}

// spec describes one command. setup registers its own options on fs and returns the function
// that runs it, with the positional arguments.
type spec struct {
	name    string
	args    string        // usage of the positional arguments
	brief   func() string // a few words, for the lists
	summary func() string // the help of the command
	write   bool          // changes something in Jira: gets --dry-run
	setup   func(fs *flag.FlagSet) func(r *runner, pos []string) int
}

// IsCommand reports whether name is one of the commands.
func IsCommand(name string) bool { return find(name) != nil }

// Summaries returns "name  summary" pairs of every command, for the main help.
func Summaries() [][2]string {
	var out [][2]string
	for _, c := range commands() {
		out = append(out, [2]string{c.name, c.brief()})
	}
	return out
}

func find(name string) *spec {
	for _, c := range commands() {
		if c.name == name {
			c := c
			return &c
		}
	}
	return nil
}

func errLabel() string { return i18n.T("erreur:", "error:") }

// Run executes args[0] (a command) with the remaining arguments and returns the exit code.
func Run(args []string, env IO, build Builder) int {
	if len(args) == 0 {
		return 2
	}
	sp := find(args[0])
	if sp == nil {
		fmt.Fprintln(env.Stderr, errLabel(), i18n.F("commande inconnue : %s", "unknown command: %s", args[0]))
		return 2
	}
	r := &runner{io: env, build: build, name: sp.name, spec: sp}
	fs := flag.NewFlagSet("jira-tui "+sp.name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&r.configPath, "config", "", i18n.T("chemin du fichier de config (défaut ~/.config/jira-tui/config.yaml)", "path of the config file (default ~/.config/jira-tui/config.yaml)"))
	fs.BoolVar(&r.demo, "demo", false, i18n.T("données factices, sans réseau", "fake data, no network"))
	fs.BoolVar(&r.asJSON, "output-json", false, i18n.T("résultat en JSON sur la sortie standard", "result as JSON on standard output"))
	if sp.write {
		fs.BoolVar(&r.dryRun, "dry-run", false, i18n.T("affiche ce qui serait envoyé à Jira sans rien écrire", "print what would be sent to Jira without writing anything"))
	}
	run := sp.setup(fs)

	pos, err := parseArgs(fs, args[1:])
	switch {
	case errors.Is(err, flag.ErrHelp):
		r.printHelp(fs)
		return 0
	case err != nil:
		return r.usageErr(err.Error())
	}
	return run(r, pos)
}

// parseArgs accepts options before and after the positional arguments; "--" ends the options.
func parseArgs(fs *flag.FlagSet, args []string) ([]string, error) {
	var tail []string
	for i, a := range args {
		if a == "--" {
			args, tail = args[:i], args[i+1:]
			break
		}
	}
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		if fs.NArg() == 0 {
			break
		}
		pos = append(pos, fs.Arg(0))
		args = fs.Args()[1:]
	}
	return append(pos, tail...), nil
}

func (r *runner) printHelp(fs *flag.FlagSet) {
	w := r.io.Stdout
	fmt.Fprintf(w, "%s jira-tui %s [options]", i18n.T("Usage :", "Usage:"), r.name)
	if r.spec.args != "" {
		fmt.Fprintf(w, " %s", r.spec.args)
	}
	fmt.Fprintf(w, "\n\n%s\n\n%s\n", r.spec.summary(), i18n.T("Options :", "Options:"))
	fs.SetOutput(w)
	fs.PrintDefaults()
	fs.SetOutput(io.Discard)
}

// --- results ---

func (r *runner) usageErr(msg string) int {
	fmt.Fprintln(r.io.Stderr, errLabel(), msg)
	fmt.Fprintf(r.io.Stderr, "%s jira-tui %s -h\n", i18n.T("Aide :", "Help:"), r.name)
	return 2
}

func (r *runner) fail(err error) int {
	fmt.Fprintln(r.io.Stderr, errLabel(), err)
	return 1
}

func (r *runner) warn(format string, args ...any) {
	fmt.Fprintf(r.io.Stderr, format+"\n", args...)
}

func (r *runner) out(format string, args ...any) { fmt.Fprintf(r.io.Stdout, format+"\n", args...) }

func (r *runner) json(v any) int {
	enc := json.NewEncoder(r.io.Stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return r.fail(err)
	}
	return 0
}

// need builds the services once; ok is false (with the exit code) when that fails.
func (r *runner) need() (*Deps, int, bool) {
	if r.deps != nil {
		return r.deps, 0, true
	}
	d, err := r.build(r.configPath, r.demo)
	if err != nil {
		return nil, r.fail(err), false
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	r.deps = &d
	return r.deps, 0, true
}

// --- the registry ---

func commands() []spec {
	cs := []spec{
		listCmd(), showCmd(), whoamiCmd(),
		commentCmd(), transitionCmd(), assignCmd(), editCmd(), createCmd(),
		logCmd(), autoCmd(), completionCmd(),
	}
	return cs
}

func commandNames() []string {
	var ns []string
	for _, c := range commands() {
		ns = append(ns, c.name)
	}
	sort.Strings(ns)
	return ns
}

// flagNames lists the options of a command (for the completion scripts).
func flagNames(sp spec) []string {
	fs := flag.NewFlagSet(sp.name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	for _, n := range []string{"config", "demo", "output-json"} {
		fs.String(n, "", "")
	}
	if sp.write {
		fs.Bool("dry-run", false, "")
	}
	sp.setup(fs)
	var out []string
	fs.VisitAll(func(f *flag.Flag) { out = append(out, "--"+f.Name) })
	sort.Strings(out)
	return out
}
