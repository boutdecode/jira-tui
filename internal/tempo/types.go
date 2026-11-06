// Package tempo is a small client for the Tempo Cloud REST API v4 (worklogs).
package tempo

// Worklog is one time entry.
type Worklog struct {
	ID          int
	AuthorID    string
	Author      string // display name (resolved through Jira)
	Seconds     int
	Date        string // YYYY-MM-DD
	Description string
}

// Total returns the sum of the worklogs, in seconds.
func Total(ws []Worklog) int {
	t := 0
	for _, w := range ws {
		t += w.Seconds
	}
	return t
}
