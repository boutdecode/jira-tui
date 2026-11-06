// Package automation is a client for Jira Cloud's Automation REST API (manual rules only).
package automation

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/kevin/jira-tui/internal/i18n"
)

// Input is a value a manual rule asks for when it is run ("Get input from users").
type Input struct {
	Name    string
	Type    string // TEXT, NUMBER, … (upper case)
	Default string
}

// Rule is a manual rule that can be run on an issue.
type Rule struct {
	ID     string
	Name   string
	Inputs []Input
}

// Unsupported returns the first input whose type this client cannot send (only TEXT and NUMBER).
func (r Rule) Unsupported() *Input {
	for i, in := range r.Inputs {
		if t := in.Type; t != "TEXT" && t != "NUMBER" {
			return &r.Inputs[i]
		}
	}
	return nil
}

// CheckValue validates a user-typed value against the input's type.
func CheckValue(in Input, v string) error {
	if in.Type == "NUMBER" {
		if _, err := strconv.ParseFloat(strings.Replace(strings.TrimSpace(v), ",", ".", 1), 64); err != nil {
			return fmt.Errorf(i18n.T("« %s » doit être un nombre", "“%s” must be a number"), in.Name)
		}
	}
	return nil
}

// Merge appends the extra rules that are not already present (same ID).
func Merge(found, extra []Rule) []Rule {
	seen := map[string]bool{}
	for _, r := range found {
		seen[r.ID] = true
	}
	out := append([]Rule(nil), found...)
	for _, r := range extra {
		if r.ID != "" && !seen[r.ID] {
			seen[r.ID] = true
			out = append(out, r)
		}
	}
	return out
}

// IssueARI is the Atlassian Resource Identifier of a Jira issue.
func IssueARI(cloudID, issueID string) string {
	return "ari:cloud:jira:" + cloudID + ":issue/" + issueID
}

// ErrNotApplicable is returned when Jira says the rule does not apply to the issue.
var ErrNotApplicable error = notApplicable{}

type notApplicable struct{}

func (notApplicable) Error() string {
	return i18n.T("règle non applicable à ce ticket", "rule not applicable to this ticket")
}
