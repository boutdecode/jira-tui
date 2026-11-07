package cli

import (
	"errors"
	"flag"
	"fmt"
	"sort"
	"strings"

	"github.com/kevin/jira-tui/internal/automation"
	"github.com/kevin/jira-tui/internal/i18n"
	"github.com/kevin/jira-tui/internal/tempo"
)

func logCmd() spec {
	return spec{
		name: "log", args: "TICKET DURATION", write: true,
		brief: func() string { return i18n.T("Saisit du temps dans Tempo", "Log time in Tempo") },
		summary: func() string {
			return i18n.T("Saisit du temps passé dans Tempo (1h30, 45m, 2d, 1d 2h ; un nombre seul = minutes).",
				"Log time spent in Tempo (1h30, 45m, 2d, 1d 2h; a bare number = minutes).")
		},
		setup: func(fs *flag.FlagSet) func(r *runner, pos []string) int {
			date := fs.String("date", "", i18n.T("jour concerné : vide = aujourd'hui, hier, 04/11, 2026-11-04", "day: empty = today, yesterday, 04/11, 2026-11-04"))
			desc := fs.String("description", "", i18n.T("description du temps saisi", "description of the logged time"))
			return func(r *runner, pos []string) int {
				key, rest, code, ok := r.ticketArg(pos)
				if !ok {
					return code
				}
				if len(rest) == 0 {
					return r.usageErr(i18n.T("indiquez la durée (ex. 1h30)", "give the duration (e.g. 1h30)"))
				}
				if len(rest) > 1 {
					return r.unexpected(rest[1:])
				}
				d, code, ok := r.need()
				if !ok {
					return code
				}
				if d.Time == nil {
					return r.fail(errors.New(i18n.T("Tempo non configuré — ajoutez tempo_token dans config.yaml (ou TEMPO_TOKEN)", "Tempo not configured — add tempo_token to config.yaml (or TEMPO_TOKEN)")))
				}
				secs, err := tempo.ParseDuration(rest[0], d.Config.HoursPerDay)
				if err != nil {
					return r.usageErr(err.Error())
				}
				day, err := tempo.ParseDate(*date, d.Now())
				if err != nil {
					return r.usageErr(err.Error())
				}
				is, err := d.Svc.GetIssue(key)
				if err != nil {
					return r.fail(err)
				}
				if r.dryRun {
					return r.json(map[string]any{"action": "log", "key": key, "issue_id": is.ID, "seconds": secs, "date": day, "description": *desc})
				}
				if err := d.Time.AddWorklog(is.ID, secs, day, strings.TrimSpace(*desc)); err != nil {
					return r.fail(err)
				}
				if r.asJSON {
					return r.json(map[string]any{"key": key, "url": d.Svc.BrowseURL(key), "seconds": secs, "date": day})
				}
				r.out("%s", i18n.F("%s : %s ajoutés le %s", "%s: %s logged on %s", key, tempo.FormatDuration(secs), day))
				return 0
			}
		},
	}
}

func autoCmd() spec {
	return spec{
		name: "auto", args: "TICKET [RULE]", write: true,
		brief: func() string { return i18n.T("Lance une automatisation manuelle", "Run a manual automation") },
		summary: func() string {
			return i18n.T("Automatisations Jira à déclencheur manuel : sans règle (ou avec --list) les liste, sinon lance la règle choisie (par son nom ou son identifiant).",
				"Jira automations with a manual trigger: without a rule (or with --list) list them, otherwise run the chosen rule (by name or id).")
		},
		setup: func(fs *flag.FlagSet) func(r *runner, pos []string) int {
			list := fs.Bool("list", false, i18n.T("liste les automatisations du ticket", "list the automations of the issue"))
			var inputs multi
			fs.Var(&inputs, "input", i18n.T("valeur d'une saisie de la règle : nom=valeur (répétable)", "value of an input of the rule: name=value (repeatable)"))
			return func(r *runner, pos []string) int {
				key, rest, code, ok := r.ticketArg(pos)
				if !ok {
					return code
				}
				if len(rest) > 1 {
					return r.unexpected(rest[1:])
				}
				d, code, ok := r.need()
				if !ok {
					return code
				}
				if d.Auto == nil {
					return r.fail(errors.New(i18n.T("automatisations indisponibles", "automations unavailable")))
				}
				is, err := d.Svc.GetIssue(key)
				if err != nil {
					return r.fail(err)
				}
				var extra []automation.Rule
				for _, a := range d.Config.Automations {
					extra = append(extra, automation.Rule{ID: a.ID, Name: a.Name})
				}
				rules, lerr := d.Auto.List(is.ID)
				rules = automation.Merge(rules, extra)
				if lerr != nil {
					if len(rules) == 0 {
						return r.fail(fmt.Errorf(i18n.T("automatisations : %w", "automations: %w"), lerr))
					}
					r.warn("%s %v", i18n.T("recherche automatique indisponible :", "automatic search unavailable:"), lerr)
				}
				if *list || len(rest) == 0 {
					type row struct {
						ID     string   `json:"id"`
						Name   string   `json:"name"`
						Inputs []string `json:"inputs"`
					}
					rows := make([]row, 0, len(rules))
					for _, ru := range rules {
						in := []string{}
						for _, i := range ru.Inputs {
							in = append(in, i.Name+" ("+i.Type+")")
						}
						rows = append(rows, row{ru.ID, ru.Name, in})
					}
					if r.asJSON {
						return r.json(rows)
					}
					for _, ro := range rows {
						r.out("%s\t%s\t%s", ro.ID, ro.Name, strings.Join(ro.Inputs, ", "))
					}
					return 0
				}
				rule, err := pickRule(rest[0], rules)
				if err != nil {
					return r.fail(err)
				}
				if in := rule.Unsupported(); in != nil {
					return r.fail(fmt.Errorf(i18n.T("« %s » : saisie « %s » de type %s non gérée — lancez la règle depuis Jira", "“%s”: input “%s” of type %s is not supported — run the rule from Jira"), rule.Name, in.Name, in.Type))
				}
				values := map[string]string{}
				for _, in := range rule.Inputs {
					values[in.Name] = in.Default
				}
				for _, kv := range inputs {
					name, val, found := strings.Cut(kv, "=")
					if !found {
						return r.usageErr(i18n.F("--input attend nom=valeur, reçu : %s", "--input expects name=value, got: %s", kv))
					}
					if _, known := values[name]; !known {
						var names []string
						for _, in := range rule.Inputs {
							names = append(names, in.Name)
						}
						return r.usageErr(i18n.F("la règle « %s » n'a pas de saisie « %s » (saisies : %s)", "rule “%s” has no input “%s” (inputs: %s)", rule.Name, name, strings.Join(names, ", ")))
					}
					values[name] = val
				}
				for _, in := range rule.Inputs {
					if err := automation.CheckValue(in, values[in.Name]); err != nil {
						return r.usageErr(err.Error())
					}
				}
				if r.dryRun {
					return r.json(map[string]any{"action": "auto", "key": key, "issue_id": is.ID, "rule": map[string]string{"id": rule.ID, "name": rule.Name}, "inputs": values})
				}
				if err := d.Auto.Run(rule, is.ID, values); err != nil {
					return r.fail(fmt.Errorf("“%s”: %w", rule.Name, err))
				}
				if r.asJSON {
					return r.json(map[string]string{"key": key, "rule": rule.Name, "status": "started"})
				}
				r.out("%s", i18n.F("règle « %s » lancée sur %s (elle s'exécute dans Jira)", "rule “%s” started on %s (it runs in Jira)", rule.Name, key))
				return 0
			}
		},
	}
}

func pickRule(query string, rules []automation.Rule) (automation.Rule, error) {
	for _, ru := range rules {
		if ru.ID != "" && ru.ID == query {
			return ru, nil
		}
	}
	names := make([]string, len(rules))
	for i, ru := range rules {
		names[i] = ru.Name
	}
	i, err := pick(query, names)
	if err != nil {
		return automation.Rule{}, err
	}
	return rules[i], nil
}

func completionCmd() spec {
	return spec{
		name: "completion", args: "bash|zsh|fish",
		brief: func() string { return i18n.T("Script de complétion du shell", "Shell completion script") },
		summary: func() string {
			return i18n.T("Affiche le script de complétion du shell (ex. source <(jira-tui completion zsh)).", "Print the shell completion script (e.g. source <(jira-tui completion zsh)).")
		},
		setup: func(fs *flag.FlagSet) func(r *runner, pos []string) int {
			return func(r *runner, pos []string) int {
				if len(pos) != 1 {
					return r.usageErr(i18n.T("indiquez le shell : bash, zsh ou fish", "give the shell: bash, zsh or fish"))
				}
				script, ok := completionScript(pos[0])
				if !ok {
					return r.usageErr(i18n.F("shell inconnu : %s (bash, zsh ou fish)", "unknown shell: %s (bash, zsh or fish)", pos[0]))
				}
				fmt.Fprint(r.io.Stdout, script)
				return 0
			}
		},
	}
}

// firstSentence shortens a summary for a completion menu.
func firstSentence(s string) string {
	if i := strings.Index(s, ". "); i > 0 {
		s = s[:i]
	}
	s = strings.TrimSuffix(s, ".")
	return strings.NewReplacer("'", "", "\"", "", "`", "", ":", " -", "\\", "").Replace(s)
}

func completionScript(shell string) (string, bool) {
	cs := commands()
	sort.Slice(cs, func(i, j int) bool { return cs[i].name < cs[j].name })
	var sb strings.Builder
	switch shell {
	case "bash":
		var names []string
		for _, c := range cs {
			names = append(names, c.name)
		}
		fmt.Fprintf(&sb, "_jira_tui() {\n  local cur=\"${COMP_WORDS[COMP_CWORD]}\"\n  if [ \"$COMP_CWORD\" -eq 1 ]; then\n    COMPREPLY=( $(compgen -W \"%s --version --help --demo --transparent --output-json --config\" -- \"$cur\") )\n    return\n  fi\n  case \"${COMP_WORDS[1]}\" in\n", strings.Join(names, " "))
		for _, c := range cs {
			fmt.Fprintf(&sb, "    %s) COMPREPLY=( $(compgen -W \"%s --help\" -- \"$cur\") ) ;;\n", c.name, strings.Join(flagNames(c), " "))
		}
		sb.WriteString("  esac\n}\ncomplete -F _jira_tui jira-tui\n")
	case "zsh":
		sb.WriteString("#compdef jira-tui\n_jira_tui() {\n  local -a cmds\n  cmds=(\n")
		for _, c := range cs {
			fmt.Fprintf(&sb, "    '%s:%s'\n", c.name, firstSentence(c.brief()))
		}
		sb.WriteString("  )\n  if (( CURRENT == 2 )); then\n    _describe 'command' cmds\n    return\n  fi\n  case $words[2] in\n")
		for _, c := range cs {
			fmt.Fprintf(&sb, "    %s) _values 'options' %s ;;\n", c.name, strings.Join(flagNames(c), " "))
		}
		sb.WriteString("  esac\n}\n_jira_tui \"$@\"\n")
	case "fish":
		for _, c := range cs {
			fmt.Fprintf(&sb, "complete -c jira-tui -n '__fish_use_subcommand' -a %s -d '%s'\n", c.name, firstSentence(c.brief()))
			for _, f := range flagNames(c) {
				fmt.Fprintf(&sb, "complete -c jira-tui -n '__fish_seen_subcommand_from %s' -l %s\n", c.name, strings.TrimPrefix(f, "--"))
			}
		}
	default:
		return "", false
	}
	return sb.String(), true
}
