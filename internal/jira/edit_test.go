package jira

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

// recorder answers every request with status/body and remembers what it received.
type recorder struct {
	method, path, query string
	body                map[string]any
}

func serve(t *testing.T, status int, resp string) (*Client, *recorder) {
	t.Helper()
	rec := &recorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.method, rec.path, rec.query, rec.body = r.Method, r.URL.Path, r.URL.RawQuery, nil
		raw, _ := io.ReadAll(r.Body)
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &rec.body)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(resp))
	}))
	t.Cleanup(srv.Close)
	return NewClient(srv.URL, "me@x.io", "tok"), rec
}

func strp(s string) *string { return &s }

// docp is a description made of the given Markdown.
func docp(md string) *Doc { d := MarkdownToADF(md); return &d }

func TestUpdateIssueSendsOnlyGivenFields(t *testing.T) {
	c, rec := serve(t, 204, "")
	labels := []string{"a", "b"}
	err := c.UpdateIssue("PAY-1", FieldUpdate{Summary: strp("Nouveau titre"), PriorityID: strp("2"), Labels: &labels})
	if err != nil {
		t.Fatal(err)
	}
	if rec.method != "PUT" || rec.path != "/rest/api/3/issue/PAY-1" {
		t.Fatalf("%s %s", rec.method, rec.path)
	}
	fields, _ := rec.body["fields"].(map[string]any)
	if len(fields) != 3 || fields["summary"] != "Nouveau titre" ||
		!reflect.DeepEqual(fields["priority"], map[string]any{"id": "2"}) ||
		!reflect.DeepEqual(fields["labels"], []any{"a", "b"}) {
		t.Fatalf("fields = %v", fields)
	}
}

func TestUpdateIssueDescriptionAndClearing(t *testing.T) {
	c, rec := serve(t, 204, "")
	if err := c.UpdateIssue("PAY-1", FieldUpdate{Description: docp("ligne 1\nligne 2\n\npar 2")}); err != nil {
		t.Fatal(err)
	}
	fields, _ := rec.body["fields"].(map[string]any)
	if got := ADFToText(fields["description"]); got != "ligne 1\nligne 2\n\npar 2" {
		t.Errorf("description round trip: %q (%v)", got, fields["description"])
	}
	if _, ok := ADFToMarkdown(fields["description"]); !ok {
		t.Error("what the app writes must be editable again")
	}
	// an empty text clears the description (null), an empty label list clears the labels
	none := []string{}
	if err := c.UpdateIssue("PAY-1", FieldUpdate{Description: docp("  \n"), Labels: &none}); err != nil {
		t.Fatal(err)
	}
	fields, _ = rec.body["fields"].(map[string]any)
	if v, ok := fields["description"]; !ok || v != nil {
		t.Errorf("description should be null, got %v (present=%v)", v, ok)
	}
	if !reflect.DeepEqual(fields["labels"], []any{}) {
		t.Errorf("labels = %#v", fields["labels"])
	}
	// nothing to change: no request at all
	c2, rec2 := serve(t, 204, "")
	if err := c2.UpdateIssue("PAY-1", FieldUpdate{}); err != nil || rec2.method != "" {
		t.Errorf("empty update sent %q (%v)", rec2.method, err)
	}
}

func TestEditMeta(t *testing.T) {
	c, rec := serve(t, 200, `{"fields":{"summary":{},"priority":{"allowedValues":[{"id":"1","name":"High"},{"id":"3","name":"Low"}]},"labels":{}}}`)
	em, err := c.EditMeta("PAY-1")
	if err != nil {
		t.Fatal(err)
	}
	if rec.path != "/rest/api/3/issue/PAY-1/editmeta" {
		t.Errorf("path %s", rec.path)
	}
	if len(em.Priorities) != 2 || em.Priorities[1].Name != "Low" || em.Priorities[1].ID != "3" {
		t.Errorf("priorities %+v", em.Priorities)
	}
	if !em.Editable["summary"] || !em.Editable["labels"] || em.Editable["description"] {
		t.Errorf("editable %+v", em.Editable)
	}
}

func TestSearchAssignable(t *testing.T) {
	c, rec := serve(t, 200, `[{"accountId":"a1","displayName":"Camille R.","accountType":"atlassian","active":true},
		{"accountId":"bot","displayName":"Some App","accountType":"app","active":true},
		{"accountId":"gone","displayName":"Left","accountType":"atlassian","active":false},
		{"accountId":"a2","displayName":"Sofia M."}]`)
	us, err := c.SearchAssignable("PAY-1", "ca m")
	if err != nil {
		t.Fatal(err)
	}
	if rec.path != "/rest/api/3/user/assignable/search" || !strings.Contains(rec.query, "issueKey=PAY-1") || !strings.Contains(rec.query, "query=ca+m") {
		t.Errorf("request %s ? %s", rec.path, rec.query)
	}
	if len(us) != 2 || us[0].AccountID != "a1" || us[1].AccountID != "a2" {
		t.Errorf("apps and inactive users must be dropped: %+v", us)
	}
}

func TestAssignToAndMyself(t *testing.T) {
	c, rec := serve(t, 204, "")
	if err := c.AssignTo("PAY-1", "acc-9"); err != nil {
		t.Fatal(err)
	}
	if rec.method != "PUT" || rec.path != "/rest/api/3/issue/PAY-1/assignee" || rec.body["accountId"] != "acc-9" {
		t.Errorf("%s %s %v", rec.method, rec.path, rec.body)
	}
	c2, _ := serve(t, 200, `{"accountId":"me-1","displayName":"Kevin"}`)
	u, err := c2.Myself()
	if err != nil || u.AccountID != "me-1" || u.DisplayName != "Kevin" {
		t.Errorf("Myself = %+v, %v", u, err)
	}
}

func TestEditAndDeleteComment(t *testing.T) {
	c, rec := serve(t, 200, `{}`)
	internal := false // jsdPublic=false means internal
	if err := c.EditComment("PAY-1", Comment{ID: "77", Public: &internal}, TextToADF("texte corrigé")); err != nil {
		t.Fatal(err)
	}
	if rec.method != "PUT" || rec.path != "/rest/api/3/issue/PAY-1/comment/77" {
		t.Fatalf("%s %s", rec.method, rec.path)
	}
	if ADFToText(rec.body["body"]) != "texte corrigé" {
		t.Errorf("body %v", rec.body["body"])
	}
	props, _ := rec.body["properties"].([]any)
	if len(props) != 1 {
		t.Fatalf("visibility must be sent again: %v", rec.body)
	}
	p, _ := props[0].(map[string]any)
	if p["key"] != "sd.public.comment" || !reflect.DeepEqual(p["value"], map[string]any{"internal": true}) {
		t.Errorf("property %v", p)
	}
	// unknown visibility (not a JSM project): no property
	if err := c.EditComment("PAY-1", Comment{ID: "78"}, TextToADF("x")); err != nil {
		t.Fatal(err)
	}
	if _, has := rec.body["properties"]; has {
		t.Errorf("no property expected: %v", rec.body)
	}

	c2, rec2 := serve(t, 204, "")
	if err := c2.DeleteComment("PAY-1", "77"); err != nil {
		t.Fatal(err)
	}
	if rec2.method != "DELETE" || rec2.path != "/rest/api/3/issue/PAY-1/comment/77" {
		t.Errorf("%s %s", rec2.method, rec2.path)
	}
}

func TestReadableJiraErrors(t *testing.T) {
	c, _ := serve(t, 400, `{"errorMessages":["Le champ est invalide"],"errors":{"summary":"Le résumé est obligatoire","labels":"espace interdit"}}`)
	err := c.UpdateIssue("PAY-1", FieldUpdate{Summary: strp("")})
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{"Le champ est invalide", "summary: Le résumé est obligatoire", "labels: espace interdit"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%q misses %q", err.Error(), want)
		}
	}
	if strings.Contains(err.Error(), "errorMessages") {
		t.Errorf("raw JSON leaked: %q", err.Error())
	}
	// a body that is not Jira's JSON stays as it is
	c2, _ := serve(t, 403, "forbidden by proxy")
	if err := c2.DeleteComment("PAY-1", "1"); err == nil || !strings.Contains(err.Error(), "forbidden by proxy") {
		t.Errorf("err = %v", err)
	}
}
