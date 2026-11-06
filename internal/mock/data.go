// Package mock provides an in-memory jira.Service for --demo and tests.
package mock

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/kevin/jira-tui/internal/i18n"
	"github.com/kevin/jira-tui/internal/jira"
)

type Service struct {
	statuses []jira.Status
	issues   []jira.Issue
	comments map[string][]jira.Comment
}

// Indexes of the demo statuses (see newStatuses).
const (
	sTodo = iota
	sDoing
	sReview
	sDone
)

// newStatuses builds the workflow in the current language (names are demo data).
func newStatuses() []jira.Status {
	return []jira.Status{
		{ID: "1", Name: i18n.T("À faire", "To Do"), Category: jira.StatusCategory{Key: "new"}},
		{ID: "2", Name: i18n.T("En cours", "In Progress"), Category: jira.StatusCategory{Key: "indeterminate"}},
		{ID: "3", Name: i18n.T("En revue", "In Review"), Category: jira.StatusCategory{Key: "indeterminate"}},
		{ID: "4", Name: i18n.T("Terminé", "Done"), Category: jira.StatusCategory{Key: "done"}},
	}
}

// IssueIDBase is the numeric id of the first mock issue (PAY-142).
const IssueIDBase = 10000

const layout = "2006-01-02T15:04:05.000-0700"

type seed struct {
	key, typ, summary string
	status            int
	prio, who         string
	age               time.Duration
}

var seeds = []seed{
	{"PAY-142", "Bug", "Le paiement échoue avec une carte 3DS2", sDoing, "High", "Camille R.", 12 * time.Minute},
	{"PAY-141", "Tâche", "Migrer les webhooks vers l’API v2", sTodo, "Medium", "Kevin", time.Hour},
	{"PAY-139", "Story", "Ajouter Apple Pay au checkout", sDoing, "High", "Kevin", 2 * time.Hour},
	{"PAY-137", "Bug", "Montant TVA arrondi de travers sur les devis", sReview, "Medium", "Sofia M.", 3 * time.Hour},
	{"PAY-135", "Story", "Page « historique des paiements »", sTodo, "Low", "", 26 * time.Hour},
	{"PAY-133", "Tâche", "Documenter le flux de remboursement", sReview, "Low", "Léo B.", 28 * time.Hour},
	{"PAY-131", "Bug", "Timeout sur la création de facture > 200 lignes", sDoing, "High", "Camille R.", 50 * time.Hour},
	{"PAY-128", "Story", "Notifications e-mail en cas d’échec de prélèvement", sTodo, "Medium", "Sofia M.", 52 * time.Hour},
	{"PAY-124", "Tâche", "Mettre à jour la lib de signature PDF", sDone, "Low", "Léo B.", 74 * time.Hour},
	{"PAY-120", "Bug", "Doublon de reçu après un retry réseau", sDone, "Medium", "Kevin", 98 * time.Hour},
	{"PAY-146", "Tâche", "Ajouter un test e2e du checkout", sTodo, "Low", "Léo B.", 5 * 24 * time.Hour},
	{"PAY-144", "Story", "Limiter le débit de l’API de paiement", sTodo, "Medium", "Kevin", 6 * 24 * time.Hour},
	{"PAY-143", "Story", "Afficher le logo de la carte saisie", sTodo, "Low", "Sofia M.", 6 * 24 * time.Hour},
	{"PAY-118", "Story", "Export CSV des transactions", sDone, "Medium", "Sofia M.", 8 * 24 * time.Hour},
	{"PAY-116", "Bug", "Corriger les libellés du reçu", sDone, "Low", "Léo B.", 9 * 24 * time.Hour},
	{"PAY-114", "Tâche", "Passer le service de paiement à Go 1.27", sDone, "Medium", "Kevin", 10 * 24 * time.Hour},
	{"PAY-112", "Tâche", "Journaliser les erreurs 3DS", sDone, "Low", "Camille R.", 11 * 24 * time.Hour},
	{"PAY-110", "Story", "Ajouter les tests de paiement SEPA", sDone, "Medium", "Léo B.", 12 * 24 * time.Hour},
	{"PAY-108", "Tâche", "Mettre à jour la page des CGV", sDone, "Low", "Sofia M.", 13 * 24 * time.Hour},
}

// enSummary holds the English summaries of the demo tickets.
var enSummary = map[string]string{
	"PAY-142": "Payment fails with a 3DS2 card",
	"PAY-141": "Migrate webhooks to the v2 API",
	"PAY-139": "Add Apple Pay to the checkout",
	"PAY-137": "VAT amount wrongly rounded on quotes",
	"PAY-135": "“Payment history” page",
	"PAY-133": "Document the refund flow",
	"PAY-131": "Timeout when creating an invoice with > 200 lines",
	"PAY-128": "Email notifications when a direct debit fails",
	"PAY-124": "Update the PDF signature library",
	"PAY-120": "Duplicate receipt after a network retry",
	"PAY-146": "Add an e2e test for the checkout",
	"PAY-144": "Rate-limit the payment API",
	"PAY-143": "Show the logo of the entered card",
	"PAY-118": "CSV export of transactions",
	"PAY-116": "Fix the receipt labels",
	"PAY-114": "Move the payment service to Go 1.27",
	"PAY-112": "Log 3DS errors",
	"PAY-110": "Add SEPA payment tests",
	"PAY-108": "Update the T&C page",
}

// issueType translates the demo issue types ("Tâche" is the French name of "Task").
func issueType(t string) string {
	if t == "Tâche" {
		return i18n.T("Tâche", "Task")
	}
	return t
}

func (sd seed) title() string {
	if i18n.Current() == i18n.EN {
		return enSummary[sd.key]
	}
	return sd.summary
}

func user(name string) *jira.User {
	if name == "" {
		return nil
	}
	return &jira.User{AccountID: name, DisplayName: name}
}

func New() *Service {
	now := time.Now()
	s := &Service{comments: map[string][]jira.Comment{}, statuses: newStatuses()}
	for i, sd := range seeds {
		f := jira.Fields{
			Summary:     sd.title(),
			Status:      s.statuses[sd.status],
			IssueType:   jira.Named{Name: issueType(sd.typ)},
			Priority:    &jira.Named{Name: sd.prio},
			Assignee:    user(sd.who),
			Reporter:    user("Sofia M."),
			Updated:     now.Add(-sd.age).Format(layout),
			Created:     now.Add(-sd.age - 48*time.Hour).Format(layout),
			Components:  []jira.Named{{Name: "Checkout"}},
			Labels:      []string{i18n.T("paiement", "payment")},
			Description: jira.TextToADF(i18n.T("Description à compléter pour : ", "Description to be completed for: ") + sd.title()),
		}
		if sd.key == "PAY-142" {
			f.Labels = []string{i18n.T("paiement", "payment"), "3ds", "prod"}
			f.Description = jira.TextToADF(i18n.T(
				"Lors du paiement avec une carte nécessitant une authentification 3DS2, la redirection vers la banque aboutit à une erreur 502 et la commande reste « en attente ».\n\n"+
					"1. Ajouter un article au panier\n2. Payer avec la carte de test 4000 0027 6000 3184\n3. Valider le challenge dans la fenêtre de la banque",
				"When paying with a card that requires 3DS2 authentication, the redirect to the bank ends in a 502 error and the order stays “pending”.\n\n"+
					"1. Add an item to the cart\n2. Pay with the test card 4000 0027 6000 3184\n3. Complete the challenge in the bank window"))
		}
		if sd.key == "PAY-137" {
			// a mention and a status lozenge: no Markdown can hold them, so the app refuses to edit it
			f.Description = unsupportedDescription()
		}
		if sd.key == "PAY-135" {
			// a description with a link and a list: the app must refuse to rewrite it as plain text
			f.Description = richDescription()
		}
		if sd.key == "PAY-142" {
			est := 8 * 3600
			f.OriginalEstimate = &est
		}
		s.issues = append(s.issues, jira.Issue{ID: fmt.Sprint(IssueIDBase + i), Key: sd.key, Fields: f})
	}
	for _, c := range []struct {
		who, text string
		age       time.Duration
		internal  bool
	}{
		{"Camille R.", i18n.T("Je regarde côté Stripe, les logs montrent un échec à la confirmation.", "I'm looking at the Stripe side, the logs show a failure at confirmation."), 3 * time.Hour, true},
		{"Sofia M.", i18n.T("Même erreur en préprod avec Visa et Mastercard. Pas de souci en 3DS1.", "Same error in staging with Visa and Mastercard. No issue with 3DS1."), 2 * time.Hour, false},
		{"Kevin", i18n.T("Piste : le paramètre return_url est tronqué après le « ? ». À confirmer.", "Lead: the return_url parameter is truncated after the “?”. To be confirmed."), 12 * time.Minute, false},
	} {
		public := !c.internal
		s.comments["PAY-142"] = append(s.comments["PAY-142"], jira.Comment{
			ID: fmt.Sprint(len(s.comments["PAY-142"]) + 1), Author: jira.User{AccountID: c.who, DisplayName: c.who},
			Body: jira.TextToADF(c.text), RawDate: now.Add(-c.age).Format(layout), Public: &public,
		})
	}
	return s
}

func (s *Service) find(key string) *jira.Issue {
	for i := range s.issues {
		if s.issues[i].Key == key {
			return &s.issues[i]
		}
	}
	return nil
}

// Search honours "resolution = Unresolved" so the "ouverts" chip has a visible effect.
func (s *Service) Search(jql string) ([]jira.Issue, error) {
	onlyOpen := strings.Contains(jql, "resolution = Unresolved")
	var out []jira.Issue
	for _, is := range s.issues {
		if onlyOpen && is.Fields.Status.Category.Key == "done" {
			continue
		}
		out = append(out, is)
	}
	return out, nil
}

func (s *Service) GetIssue(key string) (jira.Issue, error) {
	is := s.find(key)
	if is == nil {
		return jira.Issue{}, fmt.Errorf(i18n.T("ticket %s introuvable", "ticket %s not found"), key)
	}
	out := *is
	out.Fields.Comment.Comments = s.comments[key]
	return out, nil
}

func (s *Service) Transitions(key string) ([]jira.Transition, error) {
	var ts []jira.Transition
	for _, st := range s.statuses {
		ts = append(ts, jira.Transition{ID: st.ID, Name: st.Name, To: st})
	}
	return ts, nil
}

func (s *Service) DoTransition(key, id string) error {
	is := s.find(key)
	if is == nil {
		return fmt.Errorf(i18n.T("ticket %s introuvable", "ticket %s not found"), key)
	}
	for _, st := range s.statuses {
		if st.ID == id {
			is.Fields.Status = st
			is.Fields.Updated = time.Now().Format(layout)
			return nil
		}
	}
	return fmt.Errorf(i18n.T("transition %s inconnue", "unknown transition %s"), id)
}

func (s *Service) AddComment(key string, body jira.Doc, internal bool) error {
	public := !internal
	s.comments[key] = append(s.comments[key], jira.Comment{
		ID: s.nextCommentID(key), Author: jira.User{AccountID: "Kevin", DisplayName: "Kevin"},
		Body: body, RawDate: time.Now().Format(layout), Public: &public,
	})
	return nil
}

func (s *Service) nextCommentID(key string) string {
	next := 1
	for _, c := range s.comments[key] {
		if n, err := strconv.Atoi(c.ID); err == nil {
			next = max(next, n+1)
		}
	}
	return fmt.Sprint(next)
}

// richDescription has a link and a list: editable, as Markdown.
func richDescription() map[string]any {
	text := func(t string, marks ...any) map[string]any {
		n := map[string]any{"type": "text", "text": t}
		if len(marks) > 0 {
			n["marks"] = marks
		}
		return n
	}
	item := func(t string) map[string]any {
		return map[string]any{"type": "listItem", "content": []any{map[string]any{"type": "paragraph", "content": []any{text(t)}}}}
	}
	return map[string]any{"type": "doc", "version": 1, "content": []any{
		map[string]any{"type": "paragraph", "content": []any{
			text(i18n.T("Maquette : ", "Mockup: ")),
			text("figma.com/payments", map[string]any{"type": "link", "attrs": map[string]any{"href": "https://figma.com/payments"}}),
		}},
		map[string]any{"type": "bulletList", "content": []any{
			item(i18n.T("Filtrer par date", "Filter by date")),
			item(i18n.T("Filtrer par moyen de paiement", "Filter by payment method")),
		}},
	}}
}

// unsupportedDescription is an ADF document that cannot be edited as Markdown.
func unsupportedDescription() map[string]any {
	return map[string]any{"type": "doc", "version": 1, "content": []any{
		map[string]any{"type": "paragraph", "content": []any{
			map[string]any{"type": "text", "text": i18n.T("À vérifier avec ", "To check with ")},
			map[string]any{"type": "mention", "attrs": map[string]any{"id": "Sofia M.", "text": "@Sofia M."}},
			map[string]any{"type": "text", "text": " "},
			map[string]any{"type": "status", "attrs": map[string]any{"text": "BLOQUÉ", "color": "red"}},
		}},
	}}
}

// users are the people that can be assigned in the demo.
var users = []string{"Kevin", "Camille R.", "Sofia M.", "Léo B."}

var priorities = []jira.Named{{ID: "1", Name: "High"}, {ID: "2", Name: "Medium"}, {ID: "3", Name: "Low"}}

func (s *Service) notFound(key string) error {
	return fmt.Errorf(i18n.T("ticket %s introuvable", "ticket %s not found"), key)
}

func (s *Service) AssignTo(key, accountID string) error {
	is := s.find(key)
	if is == nil {
		return s.notFound(key)
	}
	for _, u := range users {
		if u == accountID {
			is.Fields.Assignee = user(u)
			return nil
		}
	}
	return fmt.Errorf(i18n.T("utilisateur %s inconnu", "unknown user %s"), accountID)
}

func (s *Service) SearchAssignable(key, query string) ([]jira.User, error) {
	if s.find(key) == nil {
		return nil, s.notFound(key)
	}
	q := strings.ToLower(strings.TrimSpace(query))
	var out []jira.User
	for _, u := range users {
		if strings.Contains(strings.ToLower(u), q) {
			out = append(out, *user(u))
		}
	}
	return out, nil
}

// customers are the Jira Service Management customers of the demo: they can be mentioned
// but, not being Jira users, cannot be assigned.
var customers = []string{"Julie Martin", "Paul Durand"}

func (s *Service) SearchMentionable(key, query string) ([]jira.User, error) {
	out, err := s.SearchAssignable(key, query) // the same four people can see every ticket of the demo
	if err != nil {
		return nil, err
	}
	q := strings.ToLower(strings.TrimSpace(query))
	for _, c := range customers {
		if strings.Contains(strings.ToLower(c), q) {
			out = append(out, jira.User{AccountID: c, DisplayName: c, AccountType: "customer"})
		}
	}
	return out, nil
}

// SearchIssues filters on key and summary; without a query it returns the "recent" ones.
func (s *Service) SearchIssues(query string) ([]jira.IssueRef, error) {
	q := strings.ToLower(strings.TrimSpace(query))
	var out []jira.IssueRef
	for _, is := range s.issues {
		if q == "" || strings.Contains(strings.ToLower(is.Key+" "+is.Fields.Summary), q) {
			out = append(out, jira.IssueRef{Key: is.Key, Summary: is.Fields.Summary})
		}
		if len(out) == 8 {
			break
		}
	}
	return out, nil
}

func (s *Service) Myself() (jira.User, error) { return *user("Kevin"), nil }

func (s *Service) EditMeta(key string) (jira.EditMeta, error) {
	if s.find(key) == nil {
		return jira.EditMeta{}, s.notFound(key)
	}
	return jira.EditMeta{
		Priorities: append([]jira.Named(nil), priorities...),
		Editable:   map[string]bool{"summary": true, "description": true, "priority": true, "labels": true},
	}, nil
}

func (s *Service) UpdateIssue(key string, u jira.FieldUpdate) error {
	is := s.find(key)
	if is == nil {
		return s.notFound(key)
	}
	if u.Summary != nil {
		if strings.TrimSpace(*u.Summary) == "" {
			return errors.New(i18n.T("summary : le résumé est obligatoire", "summary: the summary is required"))
		}
		is.Fields.Summary = *u.Summary
	}
	if u.Description != nil {
		if c, _ := (*u.Description)["content"].([]any); len(c) == 0 {
			is.Fields.Description = nil
		} else {
			is.Fields.Description = *u.Description
		}
	}
	if u.PriorityID != nil {
		i := slices.IndexFunc(priorities, func(p jira.Named) bool { return p.ID == *u.PriorityID })
		if i < 0 {
			return fmt.Errorf(i18n.T("priorité %s inconnue", "unknown priority %s"), *u.PriorityID)
		}
		p := priorities[i]
		is.Fields.Priority = &p
	}
	if u.Labels != nil {
		is.Fields.Labels = append([]string(nil), *u.Labels...)
	}
	is.Fields.Updated = time.Now().Format(layout)
	return nil
}

func (s *Service) EditComment(key string, c jira.Comment, body jira.Doc) error {
	for i := range s.comments[key] {
		if s.comments[key][i].ID == c.ID {
			s.comments[key][i].Body = body
			return nil
		}
	}
	return fmt.Errorf(i18n.T("commentaire %s introuvable", "comment %s not found"), c.ID)
}

func (s *Service) DeleteComment(key, commentID string) error {
	cs := s.comments[key]
	for i := range cs {
		if cs[i].ID == commentID {
			s.comments[key] = slices.Delete(slices.Clone(cs), i, i+1)
			return nil
		}
	}
	return fmt.Errorf(i18n.T("commentaire %s introuvable", "comment %s not found"), commentID)
}

func (s *Service) AssignToMe(key string) error {
	if is := s.find(key); is != nil {
		is.Fields.Assignee = user("Kevin")
	}
	return nil
}

func (s *Service) Unassign(key string) error {
	if is := s.find(key); is != nil {
		is.Fields.Assignee = nil
	}
	return nil
}

func (s *Service) Boards() ([]jira.Board, error) {
	return []jira.Board{{ID: 1, Name: i18n.T("Tableau PAY", "PAY board"), Type: "kanban"}}, nil
}

func (s *Service) BoardColumns(int) ([]jira.Column, error) {
	var cols []jira.Column
	for _, st := range s.statuses {
		cols = append(cols, jira.Column{Name: st.Name, StatusID: []string{st.ID}})
	}
	return cols, nil
}

func (s *Service) BoardIssues(int) ([]jira.Issue, error) {
	return append([]jira.Issue(nil), s.issues...), nil
}

func (s *Service) BrowseURL(key string) string { return "https://example.atlassian.net/browse/" + key }

// --- creating issues ---

type deskType struct {
	id, fr, en string
	subtask    bool
}

type deskProject struct {
	id, key, fr, en string
	types           []deskType
	required        []string // a required field the app cannot fill (shows the blocking message)
}

var demoProjects = []deskProject{
	{id: "10001", key: "PAY", fr: "Paiements", en: "Payments", types: []deskType{
		{"10", "Tâche", "Task", false}, {"11", "Bug", "Bug", false}, {"12", "Story", "Story", false}, {"13", "Sous-tâche", "Sub-task", true}}},
	{id: "10002", key: "OPS", fr: "Exploitation", en: "Operations", types: []deskType{
		{"20", "Tâche", "Task", false}, {"21", "Incident", "Incident", false}}},
	{id: "10003", key: "WEB", fr: "Site web", en: "Website", types: []deskType{
		{"30", "Tâche", "Task", false}, {"31", "Bug", "Bug", false}},
		required: []string{"Environnement"}},
}

func (p deskProject) name() string { return i18n.T(p.fr, p.en) }

func findProject(key string) *deskProject {
	for i := range demoProjects {
		if demoProjects[i].key == key {
			return &demoProjects[i]
		}
	}
	return nil
}

func (s *Service) Projects(query string) ([]jira.Project, error) {
	q := strings.ToLower(strings.TrimSpace(query))
	var out []jira.Project
	for _, p := range demoProjects {
		if q == "" || strings.Contains(strings.ToLower(p.key+" "+p.name()), q) {
			out = append(out, jira.Project{ID: p.id, Key: p.key, Name: p.name(), Type: "software"})
		}
	}
	return out, nil
}

func (s *Service) IssueTypes(project string) ([]jira.IssueType, error) {
	p := findProject(project)
	if p == nil {
		return nil, fmt.Errorf(i18n.T("projet %s introuvable", "project %s not found"), project)
	}
	var out []jira.IssueType
	for _, t := range p.types {
		out = append(out, jira.IssueType{ID: t.id, Name: i18n.T(t.fr, t.en), Subtask: t.subtask})
	}
	return out, nil
}

func (s *Service) NewIssueMeta(project, typeID string) (jira.NewIssueMeta, error) {
	p := findProject(project)
	if p == nil {
		return jira.NewIssueMeta{}, fmt.Errorf(i18n.T("projet %s introuvable", "project %s not found"), project)
	}
	meta := jira.NewIssueMeta{
		HasPriority: true, HasLabels: true, HasDesc: true, HasAssignee: true,
		Priorities: append([]jira.Named(nil), priorities...),
	}
	for _, f := range p.required {
		meta.Unsupported = append(meta.Unsupported, i18n.T(f, "Environment"))
	}
	return meta, nil
}

func (s *Service) SearchProjectUsers(project, query string) ([]jira.User, error) {
	q := strings.ToLower(strings.TrimSpace(query))
	var out []jira.User
	for _, u := range users {
		if strings.Contains(strings.ToLower(u), q) {
			out = append(out, *user(u))
		}
	}
	return out, nil
}

func (s *Service) CreateIssue(in jira.NewIssue) (jira.IssueRef, error) {
	p := findProject(in.ProjectKey)
	if p == nil {
		return jira.IssueRef{}, fmt.Errorf(i18n.T("projet %s introuvable", "project %s not found"), in.ProjectKey)
	}
	if strings.TrimSpace(in.Summary) == "" {
		return jira.IssueRef{}, errors.New(i18n.T("summary : le résumé est obligatoire", "summary: the summary is required"))
	}
	typeName := ""
	for _, t := range p.types {
		if t.id == in.TypeID {
			typeName = i18n.T(t.fr, t.en)
			if t.subtask && in.ParentKey == "" {
				return jira.IssueRef{}, errors.New(i18n.T("parent : une sous-tâche a besoin d'un parent", "parent: a sub-task needs a parent"))
			}
		}
	}
	if typeName == "" {
		return jira.IssueRef{}, fmt.Errorf(i18n.T("type %s inconnu", "unknown issue type %s"), in.TypeID)
	}
	if len(p.required) > 0 {
		return jira.IssueRef{}, fmt.Errorf("customfield_10050: %s", i18n.T("le champ est obligatoire", "the field is required"))
	}
	next := 0
	for _, is := range s.issues {
		if k, n, ok := strings.Cut(is.Key, "-"); ok && k == p.key {
			if v, err := strconv.Atoi(n); err == nil {
				next = max(next, v)
			}
		}
	}
	key := fmt.Sprintf("%s-%d", p.key, next+1)
	now := time.Now().Format(layout)
	f := jira.Fields{
		Summary: in.Summary, Status: s.statuses[sTodo], IssueType: jira.Named{Name: typeName},
		Reporter: user("Kevin"), Created: now, Updated: now, Labels: append([]string(nil), in.Labels...),
	}
	if in.Description != nil && len(docContent(*in.Description)) > 0 {
		f.Description = *in.Description
	}
	if in.PriorityID != "" {
		i := slices.IndexFunc(priorities, func(p jira.Named) bool { return p.ID == in.PriorityID })
		if i < 0 {
			return jira.IssueRef{}, fmt.Errorf(i18n.T("priorité %s inconnue", "unknown priority %s"), in.PriorityID)
		}
		pr := priorities[i]
		f.Priority = &pr
	}
	if in.AssignToMe {
		f.Assignee = user("Kevin")
	}
	s.issues = append([]jira.Issue{{ID: fmt.Sprint(IssueIDBase + len(s.issues)), Key: key, Fields: f}}, s.issues...)
	return jira.IssueRef{Key: key, Summary: in.Summary}, nil
}

func docContent(d jira.Doc) []any { c, _ := d["content"].([]any); return c }
