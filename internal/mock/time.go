package mock

import (
	"fmt"
	"time"

	"github.com/kevin/jira-tui/internal/i18n"
	"github.com/kevin/jira-tui/internal/tempo"
)

// Time is an in-memory Tempo for --demo and tests.
type Time struct{ logs map[string][]tempo.Worklog }

func NewTime() *Time {
	day := func(back int) string { return time.Now().AddDate(0, 0, -back).Format("2006-01-02") }
	return &Time{logs: map[string][]tempo.Worklog{
		fmt.Sprint(IssueIDBase): { // PAY-142
			{ID: 3, Author: "Camille R.", Seconds: 3 * 3600, Date: day(2), Description: i18n.T("Reproduction du bug en local", "Reproducing the bug locally")},
			{ID: 2, Author: "Sofia M.", Seconds: 90 * 60, Date: day(1), Description: i18n.T("Tests en préprod (Visa, Mastercard)", "Staging tests (Visa, Mastercard)")},
			{ID: 1, Author: "Kevin", Seconds: 45 * 60, Date: day(0), Description: i18n.T("Analyse du return_url", "return_url analysis")},
		},
	}}
}

func (t *Time) Worklogs(issueID string) ([]tempo.Worklog, error) {
	return append([]tempo.Worklog(nil), t.logs[issueID]...), nil
}

func (t *Time) AddWorklog(issueID string, seconds int, date, description string) error {
	ws := t.logs[issueID]
	next := 1
	for _, w := range ws {
		next = max(next, w.ID+1)
	}
	t.logs[issueID] = append([]tempo.Worklog{{ID: next, Author: "Kevin", Seconds: seconds, Date: date, Description: description}}, ws...)
	return nil
}
