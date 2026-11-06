package mock

import (
	"errors"

	"github.com/kevin/jira-tui/internal/automation"
	"github.com/kevin/jira-tui/internal/i18n"
)

// Run records one automation execution.
type Run struct {
	RuleID, IssueID string
	Values          map[string]string
}

// Automations is an in-memory Jira Automation for --demo and tests.
type Automations struct {
	svc  *Service
	Runs []Run
}

func NewAutomations(svc *Service) *Automations { return &Automations{svc: svc} }

func (a *Automations) List(issueID string) ([]automation.Rule, error) {
	return []automation.Rule{
		{ID: "rule-comment", Name: i18n.T("Ajouter un commentaire interne de suivi", "Add an internal follow-up comment")},
		{ID: "rule-escalate", Name: i18n.T("Escalader au support N2", "Escalate to L2 support"), Inputs: []automation.Input{
			{Name: i18n.T("Motif", "Reason"), Type: "TEXT"},
			{Name: i18n.T("Urgence (1-5)", "Urgency (1-5)"), Type: "NUMBER", Default: "3"},
		}},
		{ID: "rule-assign", Name: i18n.T("Assigner à un utilisateur", "Assign to a user"), Inputs: []automation.Input{{Name: i18n.T("Utilisateur", "User"), Type: "USER"}}},
	}, nil
}

func (a *Automations) Run(rule automation.Rule, issueID string, values map[string]string) error {
	if issueID == "" {
		return errors.New(i18n.T("identifiant de ticket manquant", "missing ticket id"))
	}
	for _, in := range rule.Inputs {
		if v, ok := values[in.Name]; ok {
			if err := automation.CheckValue(in, v); err != nil {
				return err
			}
		}
	}
	a.Runs = append(a.Runs, Run{RuleID: rule.ID, IssueID: issueID, Values: values})
	// make the effect visible in the demo: the escalation moves the ticket to "En revue"
	if rule.ID == "rule-escalate" {
		for i := range a.svc.issues {
			if a.svc.issues[i].ID == issueID {
				a.svc.issues[i].Fields.Status = a.svc.statuses[sReview]
			}
		}
	}
	return nil
}
