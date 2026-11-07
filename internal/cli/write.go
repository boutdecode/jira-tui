package cli

import (
	"errors"
	"flag"
	"fmt"
	"slices"
	"strings"

	"github.com/kevin/jira-tui/internal/i18n"
	"github.com/kevin/jira-tui/internal/jira"
)

func (r *runner) unexpected(rest []string) int {
	return r.usageErr(i18n.F("argument inattendu : %s", "unexpected argument: %s", strings.Join(rest, " ")))
}

func visibility(internal bool) string {
	if internal {
		return "internal"
	}
	return "client"
}

func visibilityLabel(internal bool) string {
	if internal {
		return i18n.T("interne", "internal")
	}
	return i18n.T("visible du client", "visible to the customer")
}

// converterFor resolves the --mention options and returns the converter that writes them.
func converterFor(d *Deps, mentions []string, find func(query string) ([]jira.User, error)) (jira.Converter, error) {
	table, err := mentionTable(mentions, find)
	if err != nil {
		return jira.Converter{}, err
	}
	return jira.Converter{BrowseURL: d.Svc.BrowseURL, Mentions: table}, nil
}

func commentCmd() spec {
	return spec{
		name: "comment", args: "TICKET [TEXT|-]", write: true,
		brief: func() string { return i18n.T("Ajoute un commentaire", "Add a comment") },
		summary: func() string {
			return i18n.T("Ajoute un commentaire (Markdown ; « - » ou l'entrée standard pour un long texte). ATTENTION : visible du client par défaut, utilisez --internal pour le réserver à l'équipe.",
				"Add a comment (Markdown; “-” or standard input for a long text). WARNING: visible to the customer by default, use --internal to keep it to the team.")
		},
		setup: func(fs *flag.FlagSet) func(r *runner, pos []string) int {
			internal := fs.Bool("internal", false, i18n.T("commentaire interne (non visible du client)", "internal comment (not visible to the customer)"))
			file := fs.String("file", "", i18n.T("lit le texte dans ce fichier", "read the text from this file"))
			var mentions multi
			fs.Var(&mentions, "mention", i18n.T("personne à mentionner (répétable) : « @Nom » dans le texte devient une mention", "person to mention (repeatable): “@Name” in the text becomes a mention"))
			return func(r *runner, pos []string) int {
				key, rest, code, ok := r.ticketArg(pos)
				if !ok {
					return code
				}
				d, code, ok := r.need()
				if !ok {
					return code
				}
				text, found, err := r.readText(strings.Join(rest, " "), len(rest) > 0, *file)
				if err != nil {
					return r.fail(err)
				}
				if !found || strings.TrimSpace(text) == "" {
					return r.usageErr(i18n.T("le texte du commentaire est vide", "the comment text is empty"))
				}
				conv, err := converterFor(d, mentions, func(q string) ([]jira.User, error) { return d.Svc.SearchMentionable(key, q) })
				if err != nil {
					return r.fail(err)
				}
				body := conv.ToADF(strings.TrimSpace(text))
				if r.dryRun {
					return r.json(map[string]any{"action": "comment", "key": key, "visibility": visibility(*internal), "body": body})
				}
				if err := d.Svc.AddComment(key, body, *internal); err != nil {
					return r.fail(err)
				}
				if r.asJSON {
					return r.json(map[string]string{"key": key, "url": d.Svc.BrowseURL(key), "visibility": visibility(*internal)})
				}
				r.out("%s", i18n.F("%s : commentaire ajouté (%s)", "%s: comment added (%s)", key, visibilityLabel(*internal)))
				return 0
			}
		},
	}
}

func transitionCmd() spec {
	return spec{
		name: "transition", args: "TICKET [STATUS]", write: true,
		brief: func() string { return i18n.T("Change le statut d'un ticket", "Change the status of an issue") },
		summary: func() string {
			return i18n.T("Change le statut : nom de la transition ou du statut cible (préfixe accepté s'il est unique). Sans statut, ou avec --list, affiche les transitions possibles.",
				"Change the status: name of the transition or of the target status (a prefix is accepted when unique). Without a status, or with --list, show the possible transitions.")
		},
		setup: func(fs *flag.FlagSet) func(r *runner, pos []string) int {
			list := fs.Bool("list", false, i18n.T("affiche les transitions possibles", "show the possible transitions"))
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
				ts, err := d.Svc.Transitions(key)
				if err != nil {
					return r.fail(err)
				}
				if *list || len(rest) == 0 {
					type row struct {
						ID   string `json:"id"`
						Name string `json:"name"`
						To   string `json:"to"`
					}
					rows := make([]row, 0, len(ts))
					for _, t := range ts {
						rows = append(rows, row{t.ID, t.Name, t.To.Name})
					}
					if r.asJSON {
						return r.json(rows)
					}
					for _, t := range rows {
						r.out("%s\t%s\t%s", t.ID, t.Name, t.To)
					}
					return 0
				}
				t, err := pickTransition(rest[0], ts)
				if err != nil {
					return r.fail(err)
				}
				if r.dryRun {
					return r.json(map[string]any{"action": "transition", "key": key, "transition": map[string]string{"id": t.ID, "name": t.Name, "to": t.To.Name}})
				}
				if err := d.Svc.DoTransition(key, t.ID); err != nil {
					return r.fail(err)
				}
				if r.asJSON {
					return r.json(map[string]string{"key": key, "url": d.Svc.BrowseURL(key), "status": t.To.Name})
				}
				r.out("%s → %s", key, t.To.Name)
				return 0
			}
		},
	}
}

// pickTransition finds the transition designated by query, by its own name or by the name of the
// status it leads to: an exact match first, then a prefix; ambiguity is an error.
func pickTransition(query string, ts []jira.Transition) (jira.Transition, error) {
	q := strings.ToLower(strings.TrimSpace(query))
	match := func(ok func(name string) bool) []int {
		var out []int
		for i, t := range ts {
			if ok(strings.ToLower(t.Name)) || ok(strings.ToLower(t.To.Name)) {
				out = append(out, i)
			}
		}
		return out
	}
	for _, found := range [][]int{
		match(func(n string) bool { return n == q }),
		match(func(n string) bool { return q != "" && strings.HasPrefix(n, q) }),
	} {
		switch len(found) {
		case 0:
		case 1:
			return ts[found[0]], nil
		default:
			var names []string
			for _, i := range found {
				names = append(names, ts[i].Name+" → "+ts[i].To.Name)
			}
			return jira.Transition{}, fmt.Errorf(i18n.T("« %s » est ambigu (%s)", "“%s” is ambiguous (%s)"), query, strings.Join(names, ", "))
		}
	}
	var avail []string
	for _, t := range ts {
		avail = append(avail, t.To.Name)
	}
	return jira.Transition{}, fmt.Errorf(i18n.T("« %s » ne correspond à aucune transition possible (choix : %s)", "“%s” matches no possible transition (choices: %s)"), query, strings.Join(avail, ", "))
}

func assignCmd() spec {
	return spec{
		name: "assign", args: "TICKET WHO", write: true,
		brief: func() string { return i18n.T("Assigne un ticket", "Assign an issue") },
		summary: func() string {
			return i18n.T("Assigne le ticket : « me », « none » (désassigner) ou un nom (la personne doit être assignable ; refus si le nom est ambigu).",
				"Assign the issue: “me”, “none” (unassign) or a name (the person must be assignable; refused if the name is ambiguous).")
		},
		setup: func(fs *flag.FlagSet) func(r *runner, pos []string) int {
			return func(r *runner, pos []string) int {
				key, rest, code, ok := r.ticketArg(pos)
				if !ok {
					return code
				}
				if len(rest) != 1 {
					return r.usageErr(i18n.T("indiquez à qui assigner : me, none ou un nom", "say who to assign to: me, none or a name"))
				}
				d, code, ok := r.need()
				if !ok {
					return code
				}
				who := strings.TrimSpace(rest[0])
				var target *jira.User // nil = unassign
				me := false
				switch strings.ToLower(who) {
				case "me", "moi":
					me = true
					u, err := d.Svc.Myself()
					if err != nil {
						return r.fail(err)
					}
					target = &u
				case "none", "unassigned", "aucun", "personne":
				default:
					us, err := d.Svc.SearchAssignable(key, who)
					if err != nil {
						return r.fail(err)
					}
					u, err := pickUser(who, us)
					if err != nil {
						return r.fail(err)
					}
					target = &u
				}
				name := ""
				if target != nil {
					name = target.DisplayName
				}
				if r.dryRun {
					return r.json(map[string]any{"action": "assign", "key": key, "assignee": nilIfEmpty(name), "account_id": accountOf(target)})
				}
				var err error
				switch {
				case target == nil:
					err = d.Svc.Unassign(key)
				case me:
					err = d.Svc.AssignToMe(key)
				default:
					err = d.Svc.AssignTo(key, target.AccountID)
				}
				if err != nil {
					return r.fail(err)
				}
				if r.asJSON {
					return r.json(map[string]any{"key": key, "url": d.Svc.BrowseURL(key), "assignee": nilIfEmpty(name)})
				}
				if target == nil {
					r.out("%s", i18n.F("%s désassigné", "%s unassigned", key))
				} else {
					r.out("%s", i18n.F("%s assigné à %s", "%s assigned to %s", key, name))
				}
				return 0
			}
		},
	}
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func accountOf(u *jira.User) any {
	if u == nil {
		return nil
	}
	return u.AccountID
}

func editCmd() spec {
	return spec{
		name: "edit", args: "TICKET", write: true,
		brief: func() string { return i18n.T("Modifie un ticket", "Edit an issue") },
		summary: func() string {
			return i18n.T("Modifie le titre, la description (Markdown), la priorité ou les étiquettes. Une description que l'app ne sait pas réécrire sans perte (images, tableaux…) n'est remplacée qu'avec --force.",
				"Change the title, the description (Markdown), the priority or the labels. A description the app cannot rewrite without loss (images, tables…) is only replaced with --force.")
		},
		setup: func(fs *flag.FlagSet) func(r *runner, pos []string) int {
			title := fs.String("title", "", i18n.T("nouveau titre", "new title"))
			desc := fs.String("description", "", i18n.T("nouvelle description (« - » = entrée standard ; vide = l'effacer)", "new description (“-” = standard input; empty = clear it)"))
			file := fs.String("file", "", i18n.T("lit la nouvelle description dans ce fichier", "read the new description from this file"))
			prio := fs.String("priority", "", i18n.T("nouvelle priorité (par son nom)", "new priority (by name)"))
			var labels, add, remove, mentions multi
			fs.Var(&labels, "label", i18n.T("remplace les étiquettes (répétable)", "replace the labels (repeatable)"))
			fs.Var(&add, "add-label", i18n.T("ajoute une étiquette (répétable)", "add a label (repeatable)"))
			fs.Var(&remove, "remove-label", i18n.T("retire une étiquette (répétable)", "remove a label (repeatable)"))
			fs.Var(&mentions, "mention", i18n.T("personne à mentionner dans la description (répétable)", "person to mention in the description (repeatable)"))
			force := fs.Bool("force", false, i18n.T("remplace même une description que l'app ne sait pas relire sans perte", "replace even a description the app cannot read back without loss"))
			return func(r *runner, pos []string) int {
				key, rest, code, ok := r.ticketArg(pos)
				if !ok {
					return code
				}
				if len(rest) > 0 {
					return r.unexpected(rest)
				}
				set := map[string]bool{}
				fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
				hasDesc := set["description"] || set["file"]
				switch {
				case !(set["title"] || hasDesc || set["priority"] || len(labels)+len(add)+len(remove) > 0):
					return r.usageErr(i18n.T("rien à modifier : indiquez --title, --description, --priority ou une option d'étiquettes", "nothing to change: give --title, --description, --priority or a labels option"))
				case len(labels) > 0 && len(add)+len(remove) > 0:
					return r.usageErr(i18n.T("--label ne se combine pas avec --add-label ni --remove-label", "--label cannot be combined with --add-label or --remove-label"))
				case set["title"] && strings.TrimSpace(*title) == "":
					return r.usageErr(i18n.T("le titre ne peut pas être vide", "the title cannot be empty"))
				}
				d, code, ok := r.need()
				if !ok {
					return code
				}
				var upd jira.FieldUpdate
				changed := map[string]any{}
				var names []string

				if set["title"] {
					t := strings.TrimSpace(*title)
					upd.Summary = &t
					changed["title"] = t
					names = append(names, i18n.T("titre", "title"))
				}
				var current *jira.Issue
				load := func() error {
					if current != nil {
						return nil
					}
					is, err := d.Svc.GetIssue(key)
					if err != nil {
						return err
					}
					current = &is
					return nil
				}
				if hasDesc {
					text, _, err := r.readText(*desc, set["description"], *file)
					if err != nil {
						return r.fail(err)
					}
					if err := load(); err != nil {
						return r.fail(err)
					}
					conv, err := converterFor(d, mentions, func(q string) ([]jira.User, error) { return d.Svc.SearchMentionable(key, q) })
					if err != nil {
						return r.fail(err)
					}
					if cur := current.Fields.Description; cur != nil && !*force {
						if _, _, ok := conv.ToMarkdown(cur); !ok {
							return r.fail(errors.New(i18n.T("la description actuelle contient des éléments que l'app ne sait pas réécrire sans perte (images, tableaux, panneaux…) ; ajoutez --force pour la remplacer quand même",
								"the current description has elements the app cannot rewrite without loss (images, tables, panels…); add --force to replace it anyway")))
						}
					}
					body := conv.ToADF(strings.TrimSpace(text))
					upd.Description = &body
					changed["description"] = body
					names = append(names, "description")
				}
				if set["priority"] {
					meta, err := d.Svc.EditMeta(key)
					if err != nil {
						return r.fail(err)
					}
					if !meta.Editable["priority"] || len(meta.Priorities) == 0 {
						return r.fail(errors.New(i18n.T("la priorité n'est pas modifiable pour ce ticket", "the priority cannot be edited on this issue")))
					}
					pn := make([]string, len(meta.Priorities))
					for i, p := range meta.Priorities {
						pn[i] = p.Name
					}
					i, err := pick(*prio, pn)
					if err != nil {
						return r.fail(err)
					}
					upd.PriorityID = &meta.Priorities[i].ID
					changed["priority"] = map[string]string{"id": meta.Priorities[i].ID, "name": meta.Priorities[i].Name}
					names = append(names, i18n.T("priorité", "priority"))
				}
				if len(labels)+len(add)+len(remove) > 0 {
					var final []string
					if len(labels) > 0 {
						final = dedupe(labels)
					} else {
						if err := load(); err != nil {
							return r.fail(err)
						}
						final = slices.Clone(current.Fields.Labels)
						for _, a := range add {
							if !slices.Contains(final, a) {
								final = append(final, a)
							}
						}
						final = slices.DeleteFunc(final, func(l string) bool { return slices.Contains(remove, l) })
					}
					if final == nil {
						final = []string{}
					}
					upd.Labels = &final
					changed["labels"] = final
					names = append(names, i18n.T("étiquettes", "labels"))
				}
				if r.dryRun {
					return r.json(map[string]any{"action": "edit", "key": key, "fields": changed})
				}
				if err := d.Svc.UpdateIssue(key, upd); err != nil {
					return r.fail(err)
				}
				if r.asJSON {
					return r.json(map[string]any{"key": key, "url": d.Svc.BrowseURL(key), "updated": names})
				}
				r.out("%s", i18n.F("%s modifié : %s", "%s updated: %s", key, strings.Join(names, ", ")))
				return 0
			}
		},
	}
}

func dedupe(in []string) []string {
	var out []string
	for _, s := range in {
		if s = strings.TrimSpace(s); s != "" && !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	return out
}

func createCmd() spec {
	return spec{
		name: "create", write: true,
		brief: func() string { return i18n.T("Crée un ticket", "Create an issue") },
		summary: func() string {
			return i18n.T("Crée un ticket. Le type, la priorité et les champs obligatoires sont vérifiés avec les métadonnées de création de Jira avant tout envoi.",
				"Create an issue. The type, the priority and the required fields are checked against Jira's create metadata before anything is sent.")
		},
		setup: func(fs *flag.FlagSet) func(r *runner, pos []string) int {
			project := fs.String("project", "", i18n.T("clé du projet (obligatoire)", "project key (required)"))
			typ := fs.String("type", "", i18n.T("type de ticket par son nom (défaut : le premier qui n'est pas une sous-tâche)", "issue type by name (default: the first one that is not a sub-task)"))
			title := fs.String("title", "", i18n.T("titre (obligatoire)", "title (required)"))
			desc := fs.String("description", "", i18n.T("description en Markdown (« - » = entrée standard)", "description in Markdown (“-” = standard input)"))
			file := fs.String("file", "", i18n.T("lit la description dans ce fichier", "read the description from this file"))
			prio := fs.String("priority", "", i18n.T("priorité par son nom", "priority by name"))
			assign := fs.String("assign", "", i18n.T("« me » pour vous assigner (sinon : assigné par défaut du projet)", "“me” to assign to yourself (otherwise: the project's default assignee)"))
			parent := fs.String("parent", "", i18n.T("ticket parent (crée une sous-tâche)", "parent issue (creates a sub-task)"))
			var labels, mentions multi
			fs.Var(&labels, "label", i18n.T("étiquette (répétable)", "label (repeatable)"))
			fs.Var(&mentions, "mention", i18n.T("personne à mentionner dans la description (répétable)", "person to mention in the description (repeatable)"))
			return func(r *runner, pos []string) int {
				if len(pos) > 0 {
					return r.unexpected(pos)
				}
				set := map[string]bool{}
				fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
				switch {
				case strings.TrimSpace(*project) == "":
					return r.usageErr(i18n.T("--project est obligatoire", "--project is required"))
				case strings.TrimSpace(*title) == "":
					return r.usageErr(i18n.T("--title est obligatoire", "--title is required"))
				case set["assign"] && !strings.EqualFold(*assign, "me") && !strings.EqualFold(*assign, "moi"):
					return r.usageErr(i18n.T("--assign n'accepte que « me »", "--assign only accepts “me”"))
				}
				d, code, ok := r.need()
				if !ok {
					return code
				}
				pkey := strings.ToUpper(strings.TrimSpace(*project))
				pkey = strings.TrimSpace(pkey)
				ptype, perr := pickIssueType(d, pkey, *typ, strings.TrimSpace(*parent))
				if perr != nil {
					return r.fail(perr)
				}
				meta, err := d.Svc.NewIssueMeta(pkey, ptype.ID)
				if err != nil {
					return r.fail(err)
				}
				if len(meta.Unsupported) > 0 {
					return r.fail(fmt.Errorf(i18n.T("champ obligatoire non géré : %s — créez ce ticket dans Jira", "unsupported required field: %s — create this issue in Jira"), strings.Join(meta.Unsupported, ", ")))
				}
				in := jira.NewIssue{ProjectKey: pkey, TypeID: ptype.ID, Summary: strings.TrimSpace(*title)}
				if p := strings.TrimSpace(*parent); p != "" {
					in.ParentKey = strings.ToUpper(p)
				}
				if set["description"] || set["file"] {
					if !meta.HasDesc {
						return r.fail(errors.New(i18n.T("ce type de ticket n'a pas de champ description", "this issue type has no description field")))
					}
					text, _, err := r.readText(*desc, set["description"], *file)
					if err != nil {
						return r.fail(err)
					}
					conv, err := converterFor(d, mentions, func(q string) ([]jira.User, error) { return d.Svc.SearchProjectUsers(pkey, q) })
					if err != nil {
						return r.fail(err)
					}
					if body := conv.ToADF(strings.TrimSpace(text)); len(descContent(body)) > 0 {
						in.Description = &body
					}
				}
				if set["priority"] {
					if !meta.HasPriority || len(meta.Priorities) == 0 {
						return r.fail(errors.New(i18n.T("ce type de ticket n'a pas de champ priorité", "this issue type has no priority field")))
					}
					pn := make([]string, len(meta.Priorities))
					for i, p := range meta.Priorities {
						pn[i] = p.Name
					}
					i, err := pick(*prio, pn)
					if err != nil {
						return r.fail(err)
					}
					in.PriorityID = meta.Priorities[i].ID
				}
				if len(labels) > 0 {
					if !meta.HasLabels {
						return r.fail(errors.New(i18n.T("ce type de ticket n'a pas de champ étiquettes", "this issue type has no labels field")))
					}
					in.Labels = dedupe(labels)
				}
				if set["assign"] {
					if !meta.HasAssignee {
						return r.fail(errors.New(i18n.T("ce type de ticket n'a pas de champ assigné", "this issue type has no assignee field")))
					}
					in.AssignToMe = true
				}
				if in.Labels == nil {
					in.Labels = []string{}
				}
				if r.dryRun {
					return r.json(map[string]any{
						"action": "create", "project": in.ProjectKey, "type": map[string]string{"id": ptype.ID, "name": ptype.Name},
						"summary": in.Summary, "description": in.Description, "priority_id": nilIfEmpty(in.PriorityID),
						"labels": in.Labels, "assign_to_me": in.AssignToMe, "parent": nilIfEmpty(in.ParentKey),
					})
				}
				ref, err := d.Svc.CreateIssue(in)
				if err != nil {
					return r.fail(err)
				}
				if r.asJSON {
					return r.json(map[string]string{"key": ref.Key, "url": d.Svc.BrowseURL(ref.Key), "summary": ref.Summary})
				}
				r.out("%s\t%s", ref.Key, d.Svc.BrowseURL(ref.Key))
				return 0
			}
		},
	}
}

func descContent(d jira.Doc) []any { c, _ := d["content"].([]any); return c }

// pickIssueType chooses the type of the issue to create, checking it against the project.
func pickIssueType(d *Deps, project, name, parent string) (jira.IssueType, error) {
	types, err := d.Svc.IssueTypes(project)
	if err != nil {
		return jira.IssueType{}, err
	}
	if len(types) == 0 {
		return jira.IssueType{}, fmt.Errorf(i18n.T("aucun type de ticket disponible dans %s", "no issue type available in %s"), project)
	}
	if strings.TrimSpace(name) == "" {
		for _, t := range types {
			if t.Subtask == (parent != "") {
				return t, nil
			}
		}
		if parent != "" {
			return jira.IssueType{}, fmt.Errorf(i18n.T("le projet %s n'a pas de type sous-tâche", "project %s has no sub-task type"), project)
		}
		return types[0], nil
	}
	names := make([]string, len(types))
	for i, t := range types {
		names[i] = t.Name
	}
	for _, t := range types {
		if t.ID == name {
			return checkSubtask(t, parent)
		}
	}
	i, err := pick(name, names)
	if err != nil {
		return jira.IssueType{}, err
	}
	return checkSubtask(types[i], parent)
}

func checkSubtask(t jira.IssueType, parent string) (jira.IssueType, error) {
	switch {
	case t.Subtask && parent == "":
		return t, fmt.Errorf(i18n.T("« %s » est une sous-tâche : indiquez --parent", "“%s” is a sub-task: give --parent"), t.Name)
	case !t.Subtask && parent != "":
		return t, fmt.Errorf(i18n.T("« %s » n'est pas une sous-tâche : --parent n'a de sens que pour une sous-tâche", "“%s” is not a sub-task: --parent only makes sense for a sub-task"), t.Name)
	}
	return t, nil
}
