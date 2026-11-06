package tempo

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kevin/jira-tui/internal/jira"
)

func TestParseDuration(t *testing.T) {
	ok := map[string]int{
		"1h30": 5400, "1h 30m": 5400, "1h30m": 5400, "90m": 5400, "90": 5400, "1.5h": 5400, "1,5h": 5400,
		"45m": 2700, "45min": 2700, "2d": 2 * 8 * 3600, "1d 2h": 10 * 3600, "1j": 8 * 3600, "1d2": 10 * 3600,
		" 2H ": 7200, "15": 900,
	}
	for in, want := range ok {
		if got, err := ParseDuration(in, 8); err != nil || got != want {
			t.Errorf("ParseDuration(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	if got, _ := ParseDuration("1d", 7); got != 7*3600 {
		t.Errorf("hoursPerDay ignored: %d", got)
	}
	for _, in := range []string{"", "abc", "0", "0h", "-1h", "1x", "25h", "h", "1h30 pause"} {
		if got, err := ParseDuration(in, 8); err == nil {
			t.Errorf("ParseDuration(%q) = %d, want an error", in, got)
		}
	}
}

func TestFormatDuration(t *testing.T) {
	for secs, want := range map[int]string{0: "0 min", 900: "15 min", 5400: "1 h 30 min", 7200: "2 h", 8 * 3600: "8 h", 2*8*3600 + 3*3600 + 900: "19 h 15 min"} {
		if got := FormatDuration(secs); got != want {
			t.Errorf("FormatDuration(%d) = %q, want %q", secs, got, want)
		}
	}
}

func TestParseDate(t *testing.T) {
	now := time.Date(2026, 11, 6, 15, 0, 0, 0, time.UTC)
	ok := map[string]string{"": "2026-11-06", "Aujourd'hui": "2026-11-06", "hier": "2026-11-05", "2026-11-04": "2026-11-04",
		"04/11/2026": "2026-11-04", "04/11": "2026-11-04", "4/11/2026": "2026-11-04", "06/11": "2026-11-06"}
	for in, want := range ok {
		if got, err := ParseDate(in, now); err != nil || got != want {
			t.Errorf("ParseDate(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"demain", "2026-11-07", "07/11", "32/01", "x"} {
		if got, err := ParseDate(in, now); err == nil {
			t.Errorf("ParseDate(%q) = %q, want an error", in, got)
		}
	}
}

func TestWorklogsPaginationAndAuth(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tt" {
			t.Errorf("auth = %q", r.Header.Get("Authorization"))
		}
		if !strings.HasPrefix(r.URL.Path, "/4/worklogs/issue/10042") {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.URL.Query().Get("page") == "" {
			_, _ = w.Write([]byte(`{"metadata":{"next":"` + srv.URL + `/4/worklogs/issue/10042?page=2"},"results":[
				{"tempoWorklogId":1,"timeSpentSeconds":3600,"startDate":"2026-11-03","description":"a","author":{"accountId":"u1"}}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"metadata":{},"results":[
			{"tempoWorklogId":2,"timeSpentSeconds":1800,"startDate":"2026-11-05","description":"b","author":{"accountId":"u2"}}]}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL+"/4/", "tt", func(id string) string { return "Nom " + id }, nil)
	ws, err := c.Worklogs("10042")
	if err != nil {
		t.Fatal(err)
	}
	if len(ws) != 2 || ws[0].ID != 2 || ws[0].Author != "Nom u2" || ws[1].Seconds != 3600 {
		t.Fatalf("got %+v", ws)
	}
	if Total(ws) != 5400 {
		t.Errorf("total = %d", Total(ws))
	}
}

func TestWorklogsNeverFollowsForeignHost(t *testing.T) {
	evil := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("token leaked to another host")
	}))
	defer evil.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"metadata":{"next":"` + evil.URL + `/x"},"results":[]}`))
	}))
	defer srv.Close()
	if _, err := NewClient(srv.URL, "tt", nil, nil).Worklogs("1"); err != nil {
		t.Fatal(err)
	}
}

func TestAddWorklog(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/4/worklogs" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		_, _ = w.Write([]byte(`{"tempoWorklogId":9}`))
	}))
	defer srv.Close()
	c := NewClient(srv.URL+"/4", "tt", nil, func() (string, error) { return "acc-1", nil })
	if err := c.AddWorklog("10042", 5400, "2026-11-04", "revue"); err != nil {
		t.Fatal(err)
	}
	if got["issueId"] != float64(10042) || got["timeSpentSeconds"] != float64(5400) || got["startDate"] != "2026-11-04" ||
		got["authorAccountId"] != "acc-1" || got["description"] != "revue" {
		t.Fatalf("payload = %v", got)
	}
	if err := c.AddWorklog("PAY-1", 60, "2026-11-04", ""); err == nil {
		t.Error("non numeric issue id must be refused")
	}
}

func TestErrorsAndRetry(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(429)
			return
		}
		w.WriteHeader(401)
	}))
	defer srv.Close()
	jira.Notify = func(time.Duration, int) {}
	defer func() { jira.Notify = nil }()
	_, err := NewClient(srv.URL, "bad", nil, nil).Worklogs("1")
	if err == nil || !strings.Contains(err.Error(), "401") || calls != 2 {
		t.Fatalf("err = %v, calls = %d", err, calls)
	}
}
