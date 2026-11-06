package main

import (
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/kevin/jira-tui/internal/export"
	"github.com/kevin/jira-tui/internal/i18n"
)

type options struct {
	config      string
	demo        bool
	transparent bool
	outputJSON  bool
	version     bool
	ref         string // normalized ticket key, "" if none
}

func newFlagSet(o *options) *flag.FlagSet {
	fs := flag.NewFlagSet("jira-tui", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&o.config, "config", "", i18n.T("chemin du fichier de config (défaut ~/.config/jira-tui/config.yaml)", "path of the config file (default ~/.config/jira-tui/config.yaml)"))
	fs.BoolVar(&o.demo, "demo", false, i18n.T("données factices, sans réseau", "fake data, no network"))
	fs.BoolVar(&o.transparent, "transparent", false, i18n.T("fond transparent (garde le fond du terminal)", "transparent background (keeps the terminal background)"))
	fs.BoolVar(&o.outputJSON, "output-json", false, i18n.T("affiche le ticket en JSON sur la sortie standard (sans interface) ; exige une référence", "print the ticket as JSON on standard output (no UI); requires a reference"))
	fs.BoolVar(&o.version, "version", false, i18n.T("affiche la version et quitte", "print the version and exit"))
	return fs
}

// parseArgs accepts options before and after the ticket reference
// (the flag package alone stops at the first positional argument).
func parseArgs(args []string) (options, error) {
	var o options
	fs := newFlagSet(&o)
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return o, err
		}
		if fs.NArg() == 0 {
			break
		}
		pos = append(pos, fs.Arg(0))
		args = fs.Args()[1:]
	}
	if o.version {
		return o, nil
	}
	switch {
	case len(pos) > 1:
		return o, fmt.Errorf(i18n.T("une seule référence de ticket attendue, reçu : %v", "a single ticket reference is expected, got: %v"), pos)
	case len(pos) == 1:
		key, err := export.ParseRef(pos[0])
		if err != nil {
			return o, err
		}
		o.ref = key
	case o.outputJSON:
		return o, errors.New(i18n.T("--output-json exige une référence de ticket (ex. jira-tui PAY-142 --output-json)", "--output-json requires a ticket reference (e.g. jira-tui PAY-142 --output-json)"))
	}
	return o, nil
}

func printUsage(w io.Writer) {
	var o options
	fs := newFlagSet(&o)
	fs.SetOutput(w)
	fmt.Fprintln(w, i18n.T("Usage : jira-tui [options] [TICKET|URL]", "Usage: jira-tui [options] [TICKET|URL]"))
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, i18n.T("  TICKET  clé (PAY-142) ou URL Jira : ouvre directement le détail du ticket", "  TICKET  key (PAY-142) or Jira URL: opens the ticket detail directly"))
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, i18n.T("Options :", "Options:"))
	fs.PrintDefaults()
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, i18n.T("Codes de sortie : 0 ok · 1 erreur (réseau, config, ticket introuvable) · 2 usage", "Exit codes: 0 ok · 1 error (network, config, ticket not found) · 2 usage"))
}
