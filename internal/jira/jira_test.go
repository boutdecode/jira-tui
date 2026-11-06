package jira

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestADFRoundTrip(t *testing.T) {
	in := "Première ligne\nseconde\n\nAutre paragraphe"
	raw, _ := json.Marshal(TextToADF(in))
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	got := ADFToText(doc)
	if got != in {
		t.Fatalf("round trip: got %q want %q", got, in)
	}
}

func TestADFList(t *testing.T) {
	var doc any
	_ = json.Unmarshal([]byte(`{"type":"doc","content":[{"type":"bulletList","content":[
		{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"a"}]}]},
		{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"b"}]}]}]}]}`), &doc)
	if got := ADFToText(doc); got != "• a\n• b" {
		t.Fatalf("got %q", got)
	}
}

func TestSearchPagination(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || u != "me@x.io" || p != "tok" {
			t.Errorf("bad auth %v %q %q", ok, u, p)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		calls++
		if calls == 1 {
			if _, has := body["nextPageToken"]; has {
				t.Error("first call must not send a token")
			}
			_, _ = w.Write([]byte(`{"issues":[{"key":"A-1"}],"nextPageToken":"p2"}`))
			return
		}
		if body["nextPageToken"] != "p2" {
			t.Errorf("token not forwarded: %v", body["nextPageToken"])
		}
		_, _ = w.Write([]byte(`{"issues":[{"key":"A-2"}]}`))
	}))
	defer srv.Close()

	is, err := NewClient(srv.URL, "me@x.io", "tok").Search("project = A")
	if err != nil {
		t.Fatal(err)
	}
	if len(is) != 2 || is[0].Key != "A-1" || is[1].Key != "A-2" {
		t.Fatalf("got %+v", is)
	}
}

func TestErrors(t *testing.T) {
	for _, tc := range []struct {
		status int
		want   string
	}{{401, "401"}, {403, "403"}} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Retry-After", "7")
			w.WriteHeader(tc.status)
		}))
		_, err := NewClient(srv.URL, "a", "b").GetIssue("A-1")
		srv.Close()
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("status %d: got %v", tc.status, err)
		}
	}
}

func TestRetryOn429(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls < 3 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(429)
			return
		}
		b, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(b), `"id":"31"`) {
			t.Errorf("body lost on retry: %q", b)
		}
		w.WriteHeader(204)
	}))
	defer srv.Close()
	waits := 0
	Notify = func(time.Duration, int) { waits++ }
	defer func() { Notify = nil }()
	if err := NewClient(srv.URL, "a", "b").DoTransition("A-1", "31"); err != nil {
		t.Fatal(err)
	}
	if calls != 3 || waits != 2 {
		t.Fatalf("calls=%d waits=%d", calls, waits)
	}
}

func TestGiveUpOn429(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(429)
	}))
	defer srv.Close()
	err := NewClient(srv.URL, "a", "b").AddComment("A-1", TextToADF("x"), false)
	if err == nil || !strings.Contains(err.Error(), "429") || calls != maxRetries+1 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
}

func TestAddCommentVisibility(t *testing.T) {
	for _, internal := range []bool{true, false} {
		var got map[string]any
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "POST" || r.URL.Path != "/rest/api/3/issue/A-1/comment" {
				t.Errorf("%s %s", r.Method, r.URL.Path)
			}
			_ = json.NewDecoder(r.Body).Decode(&got)
			w.WriteHeader(201)
		}))
		if err := NewClient(srv.URL, "a", "b").AddComment("A-1", TextToADF("bonjour"), internal); err != nil {
			t.Fatal(err)
		}
		srv.Close()
		props, ok := got["properties"].([]any) // must be an array, or Jira silently ignores it
		if !ok || len(props) != 1 {
			t.Fatalf("internal=%v: properties = %v", internal, got["properties"])
		}
		p := props[0].(map[string]any)
		val := p["value"].(map[string]any)
		if p["key"] != "sd.public.comment" || val["internal"] != internal {
			t.Fatalf("internal=%v: property = %v", internal, p)
		}
		if got["body"] == nil {
			t.Error("ADF body missing")
		}
	}
}

func commentServer(t *testing.T, commentsStatus int, commentsBody string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/3/issue/A-1":
			if strings.Contains(r.URL.Query().Get("fields"), "comment") {
				t.Errorf("comments must come from the dedicated endpoint: %s", r.URL.Query().Get("fields"))
			}
			_, _ = w.Write([]byte(`{"id":"10","key":"A-1","fields":{"summary":"s"}}`))
		case "/rest/api/3/issue/A-1/comment":
			w.WriteHeader(commentsStatus)
			_, _ = w.Write([]byte(commentsBody))
		default:
			t.Errorf("unexpected %s", r.URL.Path)
		}
	}))
}

func TestGetIssueCommentVisibility(t *testing.T) {
	srv := commentServer(t, 200, `{"comments":[
		{"id":"1","author":{"displayName":"A"},"jsdPublic":true,"created":"2026-11-01T10:00:00.000+0100"},
		{"id":"2","author":{"displayName":"B"},"jsdPublic":false,"created":"2026-11-01T11:00:00.000+0100"},
		{"id":"3","author":{"displayName":"C"},"created":"2026-11-01T12:00:00.000+0100"}]}`)
	defer srv.Close()
	is, err := NewClient(srv.URL, "a", "b").GetIssue("A-1")
	if err != nil {
		t.Fatal(err)
	}
	cs := is.Fields.Comment.Comments
	if len(cs) != 3 || cs[0].Public == nil || !*cs[0].Public || cs[1].Public == nil || *cs[1].Public || cs[2].Public != nil {
		t.Fatalf("visibility not decoded: %+v", cs)
	}
	if is.ID != "10" || is.CommentsErr != "" {
		t.Errorf("id=%q commentsErr=%q", is.ID, is.CommentsErr)
	}
}

func TestGetIssueSurvivesCommentFailure(t *testing.T) {
	srv := commentServer(t, 403, `forbidden`)
	defer srv.Close()
	is, err := NewClient(srv.URL, "a", "b").GetIssue("A-1")
	if err != nil || is.Key != "A-1" {
		t.Fatalf("issue must still load: %v %+v", err, is)
	}
	if !strings.Contains(is.CommentsErr, "403") || len(is.Fields.Comment.Comments) != 0 {
		t.Errorf("commentsErr = %q", is.CommentsErr)
	}
}
