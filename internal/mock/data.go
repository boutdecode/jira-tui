// Package mock provides an in-memory jira.Service for --demo and tests.
package mock

import (
	"fmt"
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
			ID: fmt.Sprint(len(s.comments["PAY-142"]) + 1), Author: jira.User{DisplayName: c.who},
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

func (s *Service) AddComment(key, text string, internal bool) error {
	public := !internal
	s.comments[key] = append(s.comments[key], jira.Comment{
		ID: fmt.Sprint(len(s.comments[key]) + 1), Author: jira.User{DisplayName: "Kevin"},
		Body: jira.TextToADF(text), RawDate: time.Now().Format(layout), Public: &public,
	})
	return nil
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
