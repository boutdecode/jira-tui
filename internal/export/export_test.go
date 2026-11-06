package export

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/kevin/jira-tui/internal/mock"
	"github.com/kevin/jira-tui/internal/tempo"
)

func TestParseRef(t *testing.T) {
	ok := map[string]string{
		"PAY-142": "PAY-142", "pay-142": "PAY-142", "  PAY-142 ": "PAY-142", "AB2_C-7": "AB2_C-7",
		"https://x.atlassian.net/browse/PAY-142":                                          "PAY-142",
		"https://x.atlassian.net/browse/pay-142?focusedCommentId=5":                       "PAY-142",
		"https://x.atlassian.net/jira/software/projects/PAY/boards/1?selectedIssue=PAY-9": "PAY-9",
		"https://x.atlassian.net/jira/servicedesk/projects/PAY/queues/custom/1/PAY-33":    "PAY-33",
	}
	for in, want := range ok {
		if got, err := ParseRef(in); err != nil || got != want {
			t.Errorf("ParseRef(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "  ", "PAY", "142", "PAY-", "-142", "PAY-1 PAY-2", "https://x.atlassian.net/", "A-1", "PAY-12abc"} {
		if got, err := ParseRef(in); err == nil {
			t.Errorf("ParseRef(%q) = %q, want an error", in, got)
		}
	}
}

type failingTime struct{}

func (failingTime) Worklogs(string) ([]tempo.Worklog, error) { return nil, errors.New("401 Tempo") }

func TestBuild(t *testing.T) {
	tk, err := Build(mock.New(), mock.NewTime(), "PAY-142")
	if err != nil {
		t.Fatal(err)
	}
	if tk.Key != "PAY-142" || tk.Type != "Bug" || tk.Status.Name != "En cours" || tk.Status.Category != "indeterminate" ||
		tk.Assignee == nil || *tk.Assignee != "Camille R." || tk.Priority == nil || *tk.Priority != "High" ||
		!strings.HasSuffix(tk.URL, "/browse/PAY-142") || !strings.Contains(tk.Description, "3DS2") ||
		!strings.Contains(tk.Description, "1. Ajouter un article au panier") {
		t.Fatalf("fields: %+v", tk)
	}
	if len(tk.Labels) != 3 || len(tk.Components) != 1 || tk.OriginalEstimateSeconds == nil || *tk.OriginalEstimateSeconds != 28800 {
		t.Errorf("lists/estimate: %+v", tk)
	}
	// comment visibility: seed = interne, client, client
	if len(tk.Comments) != 3 || tk.Comments[0].Visibility != "internal" || tk.Comments[1].Visibility != "client" || tk.Comments[0].Body == "" {
		t.Errorf("comments: %+v", tk.Comments)
	}
	// Tempo: 3 h + 1 h 30 + 45 min
	if tk.TimeSpentSeconds == nil || *tk.TimeSpentSeconds != 3*3600+90*60+45*60 || len(tk.Worklogs) != 3 || tk.WorklogsError != "" {
		t.Errorf("tempo: %v %+v %q", tk.TimeSpentSeconds, tk.Worklogs, tk.WorklogsError)
	}
	if tk.Worklogs[0].Date < tk.Worklogs[2].Date {
		t.Error("worklogs should be newest first")
	}
}

func TestBuildWithoutTempoAndNulls(t *testing.T) {
	tk, err := Build(mock.New(), nil, "PAY-135") // unassigned
	if err != nil {
		t.Fatal(err)
	}
	if tk.TimeSpentSeconds != nil || tk.Worklogs != nil || tk.Assignee != nil || tk.OriginalEstimateSeconds != nil {
		t.Errorf("absent values must be null: %+v", tk)
	}
	raw, _ := json.Marshal(tk)
	for _, want := range []string{`"assignee":null`, `"time_spent_seconds":null`, `"worklogs":null`, `"original_estimate_seconds":null`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("JSON misses %s", want)
		}
	}
}

func TestBuildTempoFailureDoesNotFail(t *testing.T) {
	tk, err := Build(mock.New(), failingTime{}, "PAY-142")
	if err != nil || tk.WorklogsError != "401 Tempo" || tk.Key != "PAY-142" || tk.TimeSpentSeconds != nil {
		t.Fatalf("err=%v tk=%+v", err, tk)
	}
}

func TestBuildUnknownTicket(t *testing.T) {
	if _, err := Build(mock.New(), nil, "ZZZ-1"); err == nil || !strings.Contains(err.Error(), "ZZZ-1") {
		t.Fatalf("err = %v", err)
	}
}

func TestJSONRoundTrip(t *testing.T) {
	tk, _ := Build(mock.New(), mock.NewTime(), "PAY-142")
	tk.Summary = `A & B <c> "d"`
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(tk)
	if !strings.Contains(buf.String(), `A & B <c>`) {
		t.Error("HTML characters must not be escaped")
	}
	var back Ticket
	if err := json.Unmarshal(buf.Bytes(), &back); err != nil || back.Summary != tk.Summary {
		t.Fatalf("round trip: %v", err)
	}
}
