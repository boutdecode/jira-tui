package export

import (
	"fmt"

	"github.com/kevin/jira-tui/internal/jira"
	"github.com/kevin/jira-tui/internal/tempo"
)

// TimeService is the Tempo surface needed to export worklogs (nil = Tempo not configured).
type TimeService interface {
	Worklogs(issueID string) ([]tempo.Worklog, error)
}

type Status struct {
	Name     string `json:"name"`
	Category string `json:"category"`
}

type Comment struct {
	Author     string `json:"author"`
	Created    string `json:"created"`
	Visibility string `json:"visibility"` // client | internal | unknown
	Body       string `json:"body"`
}

type Worklog struct {
	Author      string `json:"author"`
	Date        string `json:"date"`
	Seconds     int    `json:"seconds"`
	Description string `json:"description"`
}

// Ticket is the stable JSON shape (not Jira's raw payload).
type Ticket struct {
	Key         string   `json:"key"`
	ID          string   `json:"id"`
	URL         string   `json:"url"`
	Summary     string   `json:"summary"`
	Description string   `json:"description"`
	Type        string   `json:"type"`
	Status      Status   `json:"status"`
	Priority    *string  `json:"priority"`
	Assignee    *string  `json:"assignee"`
	Reporter    *string  `json:"reporter"`
	Labels      []string `json:"labels"`
	Components  []string `json:"components"`
	Created     string   `json:"created"`
	Updated     string   `json:"updated"`

	OriginalEstimateSeconds *int      `json:"original_estimate_seconds"`
	Comments                []Comment `json:"comments"`
	CommentsError           string    `json:"comments_error,omitempty"`

	// Tempo: null when Tempo is not configured.
	TimeSpentSeconds *int      `json:"time_spent_seconds"`
	Worklogs         []Worklog `json:"worklogs"`
	WorklogsError    string    `json:"worklogs_error,omitempty"`
}

func userName(u *jira.User) *string {
	if u == nil {
		return nil
	}
	return &u.DisplayName
}

// Build loads a ticket (with its comments and, when tsvc is set, its Tempo worklogs).
// A Tempo failure does not fail the export: it is reported in WorklogsError.
func Build(svc jira.Service, tsvc TimeService, key string) (Ticket, error) {
	is, err := svc.GetIssue(key)
	if err != nil {
		return Ticket{}, fmt.Errorf("%s : %w", key, err)
	}
	f := is.Fields
	t := Ticket{
		Key: is.Key, ID: is.ID, URL: svc.BrowseURL(is.Key),
		Summary: f.Summary, Description: jira.ADFToText(f.Description),
		Type:     f.IssueType.Name,
		Status:   Status{Name: f.Status.Name, Category: f.Status.Category.Key},
		Assignee: userName(f.Assignee), Reporter: userName(f.Reporter),
		Labels: []string{}, Components: []string{}, Comments: []Comment{},
		Created: f.Created, Updated: f.Updated,
		OriginalEstimateSeconds: f.OriginalEstimate,
		CommentsError:           is.CommentsErr,
	}
	if f.Priority != nil {
		t.Priority = &f.Priority.Name
	}
	if f.Labels != nil {
		t.Labels = f.Labels
	}
	for _, c := range f.Components {
		t.Components = append(t.Components, c.Name)
	}
	for _, c := range f.Comment.Comments {
		vis := "unknown"
		if c.Public != nil {
			vis = map[bool]string{true: "client", false: "internal"}[*c.Public]
		}
		t.Comments = append(t.Comments, Comment{
			Author: c.Author.DisplayName, Created: c.RawDate, Visibility: vis, Body: jira.ADFToText(c.Body),
		})
	}

	if tsvc != nil {
		ws, err := tsvc.Worklogs(is.ID)
		if err != nil {
			t.WorklogsError = err.Error()
		} else {
			tempo.SortNewestFirst(ws)
			total := tempo.Total(ws)
			t.TimeSpentSeconds = &total
			t.Worklogs = make([]Worklog, 0, len(ws))
			for _, w := range ws {
				t.Worklogs = append(t.Worklogs, Worklog{Author: w.Author, Date: w.Date, Seconds: w.Seconds, Description: w.Description})
			}
		}
	}
	return t, nil
}
