package cli

import (
	"flag"
	"fmt"
	"strings"

	"github.com/kevin/jira-tui/internal/export"
	"github.com/kevin/jira-tui/internal/i18n"
	"github.com/kevin/jira-tui/internal/tempo"
)

// issueRow is the stable JSON shape of one line of "list".
type issueRow struct {
	Key      string  `json:"key"`
	URL      string  `json:"url"`
	Type     string  `json:"type"`
	Status   string  `json:"status"`
	Priority *string `json:"priority"`
	Assignee *string `json:"assignee"`
	Summary  string  `json:"summary"`
	Updated  string  `json:"updated"`
}

func dash(p *string) string {
	if p == nil || *p == "" {
		return "-"
	}
	return *p
}

// oneLine keeps a text on one line so that a tab-separated row stays one row.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

func listCmd() spec {
	return spec{
		name: "list", args: "[JQL]",
		brief: func() string { return i18n.T("Liste des tickets", "List issues") },
		summary: func() string {
			return i18n.T("Liste des tickets (requête JQL, filtre de la config, ou --mine / --open).", "List issues (a JQL query, a filter of the config, or --mine / --open).")
		},
		setup: func(fs *flag.FlagSet) func(r *runner, pos []string) int {
			mine := fs.Bool("mine", false, i18n.T("seulement les tickets qui me sont assignés", "only the issues assigned to me"))
			open := fs.Bool("open", false, i18n.T("seulement les tickets non résolus", "only unresolved issues"))
			filter := fs.String("filter", "", i18n.T("nom d'un filtre du fichier de config", "name of a filter of the config file"))
			limit := fs.Int("limit", 50, i18n.T("nombre maximal de tickets", "maximum number of issues"))
			return func(r *runner, pos []string) int {
				d, code, ok := r.need()
				if !ok {
					return code
				}
				jql := strings.TrimSpace(strings.Join(pos, " "))
				switch {
				case jql != "" && (*filter != "" || *mine || *open):
					return r.usageErr(i18n.T("--mine, --open et --filter ne se combinent pas avec une requête JQL", "--mine, --open and --filter cannot be combined with a JQL query"))
				case *filter != "" && (*mine || *open):
					return r.usageErr(i18n.T("--filter ne se combine pas avec --mine ni --open", "--filter cannot be combined with --mine or --open"))
				case *limit < 1:
					return r.usageErr(i18n.T("--limit doit être un nombre positif", "--limit must be a positive number"))
				}
				switch {
				case jql != "":
				case *filter != "":
					names := make([]string, len(d.Config.Filters))
					for i, f := range d.Config.Filters {
						names[i] = f.Name
					}
					i, err := pick(*filter, names)
					if err != nil {
						return r.fail(err)
					}
					jql = d.Config.Filters[i].JQL
				case *mine || *open:
					var c []string
					if *mine {
						c = append(c, "assignee = currentUser()")
					}
					if *open {
						c = append(c, "resolution = Unresolved")
					}
					jql = strings.Join(c, " AND ") + " ORDER BY updated DESC"
				default:
					jql = d.Config.JQL
				}
				issues, err := d.Svc.Search(jql)
				if err != nil {
					return r.fail(err)
				}
				if len(issues) > *limit {
					issues = issues[:*limit]
				}
				rows := make([]issueRow, 0, len(issues))
				for _, is := range issues {
					f := is.Fields
					row := issueRow{Key: is.Key, URL: d.Svc.BrowseURL(is.Key), Type: f.IssueType.Name, Status: f.Status.Name, Summary: f.Summary, Updated: f.Updated}
					if f.Priority != nil {
						row.Priority = &f.Priority.Name
					}
					if f.Assignee != nil {
						row.Assignee = &f.Assignee.DisplayName
					}
					rows = append(rows, row)
				}
				if r.asJSON {
					return r.json(rows)
				}
				for _, row := range rows {
					r.out("%s\t%s\t%s\t%s\t%s", row.Key, row.Status, dash(row.Assignee), dash(row.Priority), oneLine(row.Summary))
				}
				return 0
			}
		},
	}
}

// ticketArg parses the first positional argument as a ticket reference.
func (r *runner) ticketArg(pos []string) (key string, rest []string, code int, ok bool) {
	if len(pos) == 0 {
		return "", nil, r.usageErr(i18n.T("une référence de ticket est attendue (ex. PAY-142)", "a ticket reference is expected (e.g. PAY-142)")), false
	}
	k, err := export.ParseRef(pos[0])
	if err != nil {
		return "", nil, r.usageErr(err.Error()), false
	}
	return k, pos[1:], 0, true
}

func showCmd() spec {
	return spec{
		name: "show", args: "TICKET",
		brief: func() string { return i18n.T("Affiche un ticket", "Show an issue") },
		summary: func() string {
			return i18n.T("Affiche un ticket (champs, description, commentaires, temps Tempo).", "Show an issue (fields, description, comments, Tempo time).")
		},
		setup: func(fs *flag.FlagSet) func(r *runner, pos []string) int {
			return func(r *runner, pos []string) int {
				key, rest, code, ok := r.ticketArg(pos)
				if !ok {
					return code
				}
				if len(rest) > 0 {
					return r.usageErr(i18n.F("argument inattendu : %s", "unexpected argument: %s", strings.Join(rest, " ")))
				}
				d, code, ok := r.need()
				if !ok {
					return code
				}
				var ts export.TimeService
				if d.Time != nil {
					ts = d.Time
				}
				t, err := export.Build(d.Svc, ts, key)
				if err != nil {
					return r.fail(err)
				}
				if t.WorklogsError != "" {
					r.warn("%s %s", i18n.T("avertissement: Tempo :", "warning: Tempo:"), t.WorklogsError)
				}
				if t.CommentsError != "" {
					r.warn("%s %s", i18n.T("avertissement: commentaires indisponibles :", "warning: comments unavailable:"), t.CommentsError)
				}
				if r.asJSON {
					return r.json(t)
				}
				printTicket(r, t)
				return 0
			}
		},
	}
}

func printTicket(r *runner, t export.Ticket) {
	r.out("%s  %s", t.Key, t.Summary)
	r.out("%s", t.URL)
	r.out("%s: %s   %s: %s   %s: %s", i18n.T("Type", "Type"), t.Type, i18n.T("Statut", "Status"), t.Status.Name, i18n.T("Priorité", "Priority"), dash(t.Priority))
	r.out("%s: %s   %s: %s", i18n.T("Assigné", "Assignee"), dash(t.Assignee), i18n.T("Rapporteur", "Reporter"), dash(t.Reporter))
	if len(t.Labels) > 0 {
		r.out("%s: %s", i18n.T("Étiquettes", "Labels"), strings.Join(t.Labels, ", "))
	}
	if len(t.Components) > 0 {
		r.out("%s: %s", i18n.T("Composants", "Components"), strings.Join(t.Components, ", "))
	}
	r.out("%s: %s   %s: %s", i18n.T("Créé", "Created"), t.Created, i18n.T("Mis à jour", "Updated"), t.Updated)
	if t.TimeSpentSeconds != nil {
		line := fmt.Sprintf("%s: %s", i18n.T("Temps passé", "Time spent"), tempo.FormatDuration(*t.TimeSpentSeconds))
		if t.OriginalEstimateSeconds != nil && *t.OriginalEstimateSeconds > 0 {
			line += fmt.Sprintf(" (%s %s)", i18n.T("estimé", "estimated"), tempo.FormatDuration(*t.OriginalEstimateSeconds))
		}
		r.out("%s", line)
	}
	if t.Description != "" {
		r.out("\n%s\n%s", i18n.T("Description", "Description"), t.Description)
	}
	if len(t.Comments) > 0 {
		r.out("\n%s (%d)", i18n.T("Commentaires", "Comments"), len(t.Comments))
		for _, c := range t.Comments {
			vis := map[string]string{
				"client":   i18n.T("client", "customer"),
				"internal": i18n.T("interne", "internal"),
			}[c.Visibility]
			head := fmt.Sprintf("[%s] %s", c.Created, c.Author)
			if vis != "" {
				head += " (" + vis + ")"
			}
			r.out("%s", head)
			for _, l := range strings.Split(c.Body, "\n") {
				r.out("  %s", l)
			}
		}
	}
}

func whoamiCmd() spec {
	return spec{
		name:  "whoami",
		brief: func() string { return i18n.T("Vérifie les identifiants", "Check the credentials") },
		summary: func() string {
			return i18n.T("Vérifie les identifiants et affiche l'utilisateur et le site.", "Check the credentials and show the user and the site.")
		},
		setup: func(fs *flag.FlagSet) func(r *runner, pos []string) int {
			return func(r *runner, pos []string) int {
				if len(pos) > 0 {
					return r.usageErr(i18n.F("argument inattendu : %s", "unexpected argument: %s", strings.Join(pos, " ")))
				}
				d, code, ok := r.need()
				if !ok {
					return code
				}
				u, err := d.Svc.Myself()
				if err != nil {
					return r.fail(err)
				}
				site := strings.TrimSuffix(d.Svc.BrowseURL(""), "/browse/")
				if r.asJSON {
					return r.json(map[string]string{"name": u.DisplayName, "account_id": u.AccountID, "site": site})
				}
				r.out("%s\t%s", u.DisplayName, site)
				return 0
			}
		},
	}
}
