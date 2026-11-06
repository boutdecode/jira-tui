package automation

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kevin/jira-tui/internal/jira"
)

const cid = "182b9218-d56a-453d-9659-3f29ea2aa7eb"

type fake struct {
	srv        *httptest.Server
	tenantHits atomic.Int32
	searches   []map[string]any
	invoked    []map[string]any
	searchBody string // first page
	page2      string
	invokeBody string
	invokeCode int
}

func newFake(t *testing.T) *fake {
	f := &fake{invokeBody: `{"ari:cloud:jira:` + cid + `:issue/10042":"SUCCESS"}`, invokeCode: 200}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		base := "/gateway/api/automation/public/jira/" + cid + "/rest/v1/rule/manual/"
		switch {
		case r.URL.Path == "/_edge/tenant_info":
			f.tenantHits.Add(1)
			_, _ = w.Write([]byte(`{"cloudId":"` + cid + `"}`))
			return
		case strings.HasPrefix(r.URL.Path, "/gateway/"):
			u, p, ok := r.BasicAuth()
			if !ok || u != "me@x.io" || p != "tok" {
				w.WriteHeader(401)
				return
			}
		}
		switch {
		case r.URL.Path == base+"search" && r.Method == "POST":
			var b map[string]any
			_ = json.NewDecoder(r.Body).Decode(&b)
			f.searches = append(f.searches, b)
			_, _ = w.Write([]byte(f.searchBody))
		case r.URL.Path == base+"search" && r.Method == "GET":
			if r.URL.Query().Get("cursor") != "c2" {
				t.Errorf("cursor = %q", r.URL.Query().Get("cursor"))
			}
			_, _ = w.Write([]byte(f.page2))
		case strings.HasSuffix(r.URL.Path, "/invocation") && r.Method == "POST":
			b, _ := io.ReadAll(r.Body)
			var m map[string]any
			_ = json.Unmarshal(b, &m)
			m["_path"] = r.URL.Path
			f.invoked = append(f.invoked, m)
			w.WriteHeader(f.invokeCode)
			_, _ = w.Write([]byte(f.invokeBody))
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.String())
			w.WriteHeader(404)
		}
	}))
	return f
}

func (f *fake) client() *Client { return NewClient(f.srv.URL, "me@x.io", "tok", "", "") }

func TestListParsesBothShapesAndPages(t *testing.T) {
	f := newFake(t)
	defer f.srv.Close()
	f.searchBody = `{"links":{"next":"?cursor=c2&limit=50"},"data":[
		{"id":"r1","name":"Commenter"},
		{"rule":{"ruleId":"r2","title":"Escalader"},"inputs":{"motif":{"inputType":"TEXT","value":"urgent"},"n":{"inputType":"NUMBER","value":3}}},
		{"name":"sans identifiant"}]}`
	f.page2 = `{"links":{},"results":[{"uuid":"r3","ruleName":"Page 2","userInputs":[{"name":"qui","type":"user"}]},{"id":"r1","name":"doublon"}]}`

	rules, err := f.client().List("10042")
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 3 || rules[0].ID != "r1" || rules[1].Name != "Escalader" || rules[2].ID != "r3" || rules[2].Name != "Page 2" {
		t.Fatalf("rules = %+v", rules)
	}
	if got := rules[1].Inputs; len(got) != 2 || got[0].Name != "motif" || got[0].Type != "TEXT" || got[0].Default != "urgent" || got[1].Type != "NUMBER" || got[1].Default != "3" {
		t.Fatalf("inputs = %+v", got)
	}
	if in := rules[2].Unsupported(); in == nil || in.Type != "USER" || rules[1].Unsupported() != nil {
		t.Errorf("unsupported detection: %+v", rules[2].Inputs)
	}
	if objs, _ := f.searches[0]["objects"].([]any); len(objs) != 1 || objs[0] != "ari:cloud:jira:"+cid+":issue/10042" {
		t.Errorf("search body = %v", f.searches[0])
	}
	// the cloud id is detected once, then cached
	if _, err := f.client().List("1"); err != nil {
		t.Fatal(err)
	}
	c := f.client()
	f.tenantHits.Store(0)
	_, _ = c.List("1")
	_, _ = c.List("2")
	if f.tenantHits.Load() != 1 {
		t.Errorf("tenant_info called %d times", f.tenantHits.Load())
	}
}

func TestRunSendsTypedInputs(t *testing.T) {
	f := newFake(t)
	defer f.srv.Close()
	rule := Rule{ID: "r2", Name: "Escalader", Inputs: []Input{{Name: "motif", Type: "TEXT"}, {Name: "n", Type: "NUMBER", Default: "3"}, {Name: "x", Type: "NUMBER"}}}
	if err := f.client().Run(rule, "10042", map[string]string{"motif": "client en colère", "x": "2,5"}); err != nil {
		t.Fatal(err)
	}
	got := f.invoked[0]
	if got["_path"] != "/gateway/api/automation/public/jira/"+cid+"/rest/v1/rule/manual/r2/invocation" {
		t.Errorf("path = %v", got["_path"])
	}
	if objs, _ := got["objects"].([]any); len(objs) != 1 || objs[0] != "ari:cloud:jira:"+cid+":issue/10042" {
		t.Errorf("objects = %v", got["objects"])
	}
	in := got["userInputs"].(map[string]any)
	motif, n, x := in["motif"].(map[string]any), in["n"].(map[string]any), in["x"].(map[string]any)
	if motif["inputType"] != "TEXT" || motif["value"] != "client en colère" ||
		n["inputType"] != "NUMBER" || n["value"] != float64(3) || // default, sent as a JSON number
		x["value"] != 2.5 { // comma decimal accepted
		t.Errorf("userInputs = %v", in)
	}
	// a rule without inputs sends none
	f.invoked = nil
	if err := f.client().Run(Rule{ID: "r1"}, "10042", nil); err != nil {
		t.Fatal(err)
	}
	if _, has := f.invoked[0]["userInputs"]; has {
		t.Error("userInputs must be omitted when the rule has no inputs")
	}
}

func TestRunRefusesBadInputsWithoutCalling(t *testing.T) {
	f := newFake(t)
	defer f.srv.Close()
	c := f.client()
	if err := c.Run(Rule{ID: "r", Inputs: []Input{{Name: "n", Type: "NUMBER"}}}, "1", map[string]string{"n": "abc"}); err == nil || !strings.Contains(err.Error(), "nombre") {
		t.Errorf("err = %v", err)
	}
	if err := c.Run(Rule{ID: "r", Inputs: []Input{{Name: "u", Type: "USER"}}}, "1", nil); err == nil || !strings.Contains(err.Error(), "non gérée") {
		t.Errorf("err = %v", err)
	}
	if len(f.invoked) != 0 {
		t.Error("nothing must be sent on invalid input")
	}
}

func TestRunStatuses(t *testing.T) {
	f := newFake(t)
	defer f.srv.Close()
	ari := "ari:cloud:jira:" + cid + ":issue/10042"
	cases := []struct {
		body string
		want error // nil = ok
		sub  string
	}{
		{`{"` + ari + `":"SUCCESS"}`, nil, ""},
		{`{"` + ari + `":"INVALID_TARGET_OBJECT"}`, ErrNotApplicable, ""},
		{`{"other":"SUCCESS"}`, nil, ""}, // single entry: taken as ours
		{`{"` + ari + `":"WEIRD"}`, nil, "WEIRD"},
		{`{}`, nil, "inattendue"},
	}
	for _, tc := range cases {
		f.invokeBody = tc.body
		err := f.client().Run(Rule{ID: "r1"}, "10042", nil)
		switch {
		case tc.want != nil && !errors.Is(err, tc.want):
			t.Errorf("%s: err = %v", tc.body, err)
		case tc.want == nil && tc.sub == "" && err != nil:
			t.Errorf("%s: err = %v", tc.body, err)
		case tc.sub != "" && (err == nil || !strings.Contains(err.Error(), tc.sub)):
			t.Errorf("%s: err = %v", tc.body, err)
		}
	}
}

func TestAuthHintAndRetry(t *testing.T) {
	f := newFake(t)
	defer f.srv.Close()
	bad := NewClient(f.srv.URL, "me@x.io", "wrong", "", "")
	if _, err := bad.List("1"); err == nil || !strings.Contains(err.Error(), "token API classique") {
		t.Errorf("401 should explain the classic token: %v", err)
	}

	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(429)
			return
		}
		_, _ = w.Write([]byte(`{"x":"SUCCESS"}`))
	}))
	defer srv.Close()
	jira.Notify = func(time.Duration, int) {}
	defer func() { jira.Notify = nil }()
	if err := NewClient(srv.URL, "a", "b", cid, "").Run(Rule{ID: "r"}, "1", nil); err != nil || calls != 2 {
		t.Errorf("429 should be retried: err=%v calls=%d", err, calls)
	}
}

func TestNextQueryNeverLeavesTheHost(t *testing.T) {
	for in, want := range map[string]string{
		"?cursor=a&limit=2": "cursor=a&limit=2", "cursor=a": "cursor=a",
		"https://evil.example/x?cursor=a": "cursor=a", "": "",
	} {
		got := nextQuery(map[string]any{"links": map[string]any{"next": in}})
		if got != want {
			t.Errorf("nextQuery(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMerge(t *testing.T) {
	got := Merge([]Rule{{ID: "a", Name: "A"}}, []Rule{{ID: "a", Name: "dup"}, {ID: "b", Name: "B"}, {Name: "no id"}})
	if len(got) != 2 || got[0].Name != "A" || got[1].ID != "b" {
		t.Errorf("merge = %+v", got)
	}
}
