package jira

import (
	"reflect"
	"strings"
	"testing"
)

func TestProjectsForCreation(t *testing.T) {
	c, rec := serve(t, 200, `{"values":[{"id":"10001","key":"PAY","name":"Paiements","projectTypeKey":"software"},{"id":"10002","key":"SUP","name":"Support","projectTypeKey":"service_desk"}],"isLast":true}`)
	ps, err := c.Projects("pa")
	if err != nil {
		t.Fatal(err)
	}
	if rec.path != "/rest/api/3/project/search" || !strings.Contains(rec.query, "action=create") || !strings.Contains(rec.query, "query=pa") {
		t.Errorf("request %s ? %s (only projects where the user can create)", rec.path, rec.query)
	}
	if len(ps) != 2 || ps[0].Key != "PAY" || ps[1].Type != "service_desk" || ps[0].Name != "Paiements" {
		t.Errorf("projects = %+v", ps)
	}
}

func TestIssueTypes(t *testing.T) {
	for _, body := range []string{
		`{"issueTypes":[{"id":"10","name":"Task","subtask":false},{"id":"13","name":"Sub-task","subtask":true}]}`,
		`{"values":[{"id":"10","name":"Task","subtask":false},{"id":"13","name":"Sub-task","subtask":true}]}`, // older shape
	} {
		c, rec := serve(t, 200, body)
		ts, err := c.IssueTypes("PAY")
		if err != nil {
			t.Fatal(err)
		}
		if rec.path != "/rest/api/3/issue/createmeta/PAY/issuetypes" {
			t.Errorf("path %s", rec.path)
		}
		if len(ts) != 2 || ts[0].Subtask || !ts[1].Subtask || ts[1].ID != "13" {
			t.Errorf("types = %+v", ts)
		}
	}
}

func TestNewIssueMeta(t *testing.T) {
	c, rec := serve(t, 200, `{"fields":[
		{"fieldId":"summary","name":"Summary","required":true,"hasDefaultValue":false},
		{"fieldId":"issuetype","name":"Issue Type","required":true},
		{"fieldId":"project","name":"Project","required":true},
		{"fieldId":"reporter","name":"Reporter","required":true,"hasDefaultValue":false},
		{"fieldId":"description","name":"Description","required":false},
		{"fieldId":"priority","name":"Priority","required":false,"hasDefaultValue":true,"allowedValues":[{"id":"1","name":"High"},{"id":"3","name":"Low"}]},
		{"fieldId":"labels","name":"Labels","required":false},
		{"fieldId":"assignee","name":"Assignee","required":false},
		{"fieldId":"customfield_10050","name":"Environnement","required":true,"hasDefaultValue":false},
		{"fieldId":"customfield_10051","name":"Équipe","required":true,"hasDefaultValue":true},
		{"fieldId":"customfield_10052","name":"Optionnel","required":false},
		{"fieldId":"components","name":"","required":true}]}`)
	meta, err := c.NewIssueMeta("PAY", "10")
	if err != nil {
		t.Fatal(err)
	}
	if rec.path != "/rest/api/3/issue/createmeta/PAY/issuetypes/10" {
		t.Errorf("path %s", rec.path)
	}
	if !meta.HasPriority || !meta.HasLabels || !meta.HasDesc || !meta.HasAssignee {
		t.Errorf("meta = %+v", meta)
	}
	if len(meta.Priorities) != 2 || meta.Priorities[0].ID != "1" || meta.Priorities[1].Name != "Low" {
		t.Errorf("priorities = %+v", meta.Priorities)
	}
	// summary, issue type, project and reporter are handled; a required field with a default
	// value is fine; an optional custom field is ignored; the others block creation
	if got := strings.Join(meta.Unsupported, "|"); got != "Environnement|components" {
		t.Errorf("unsupported = %q", got)
	}
	// a form without priority or labels
	c2, _ := serve(t, 200, `{"values":[{"fieldId":"summary","required":true}]}`)
	m2, err := c2.NewIssueMeta("PAY", "10")
	if err != nil || m2.HasPriority || m2.HasLabels || len(m2.Unsupported) != 0 {
		t.Errorf("minimal form: %+v %v", m2, err)
	}
}

func TestCreateIssueSendsOnlyWhatIsFilled(t *testing.T) {
	c, rec := serve(t, 201, `{"id":"10500","key":"PAY-150","self":"https://x/rest/api/3/issue/10500"}`)
	ref, err := c.CreateIssue(NewIssue{ProjectKey: "PAY", TypeID: "10", Summary: "Un titre"})
	if err != nil {
		t.Fatal(err)
	}
	if rec.method != "POST" || rec.path != "/rest/api/3/issue" {
		t.Fatalf("%s %s", rec.method, rec.path)
	}
	fields, _ := rec.body["fields"].(map[string]any)
	if len(fields) != 3 || fields["summary"] != "Un titre" ||
		!reflect.DeepEqual(fields["project"], map[string]any{"key": "PAY"}) ||
		!reflect.DeepEqual(fields["issuetype"], map[string]any{"id": "10"}) {
		t.Fatalf("minimal fields = %v", fields)
	}
	if ref.Key != "PAY-150" || ref.Summary != "Un titre" {
		t.Errorf("ref = %+v", ref)
	}
}

func TestCreateIssueWithEverything(t *testing.T) {
	cv := conv(map[string]string{"Sofia M.": "acc-sofia"})
	desc := cv.ToADF("# Contexte\n\nÀ voir avec @Sofia M. et #PAY-9")
	// "assign to me" needs the account id of the user: /myself answers it
	srv, rec2 := serveRoutes(t, map[string]string{
		"/rest/api/3/myself": `{"accountId":"me-1","displayName":"Kevin"}`,
		"/rest/api/3/issue":  `{"id":"2","key":"PAY-152"}`,
	})
	ref, err := srv.CreateIssue(NewIssue{
		ProjectKey: "PAY", TypeID: "13", Summary: "Sous-tâche", Description: &desc,
		PriorityID: "2", Labels: []string{"a", "b"}, AssignToMe: true, ParentKey: "PAY-9",
	})
	if err != nil {
		t.Fatal(err)
	}
	if ref.Key != "PAY-152" {
		t.Errorf("ref = %+v", ref)
	}
	fields, _ := rec2["/rest/api/3/issue"]["fields"].(map[string]any)
	want := map[string]any{
		"project":   map[string]any{"key": "PAY"},
		"issuetype": map[string]any{"id": "13"},
		"summary":   "Sous-tâche",
		"priority":  map[string]any{"id": "2"},
		"labels":    []any{"a", "b"},
		"assignee":  map[string]any{"accountId": "me-1"},
		"parent":    map[string]any{"key": "PAY-9"},
	}
	for k, v := range want {
		if !reflect.DeepEqual(fields[k], v) {
			t.Errorf("fields[%s] = %v, want %v", k, fields[k], v)
		}
	}
	// the description is the ADF built from the Markdown, mention and smart link included
	kinds := []string{}
	for _, b := range fields["description"].(map[string]any)["content"].([]any) {
		for _, n := range children(b.(map[string]any)) {
			kinds = append(kinds, n.(map[string]any)["type"].(string))
		}
	}
	if strings.Join(kinds, ",") != "text,text,mention,text,inlineCard" {
		t.Errorf("description inline nodes = %v", kinds)
	}
}

func TestCreateIssueErrorsAreReadable(t *testing.T) {
	c, _ := serve(t, 400, `{"errorMessages":[],"errors":{"summary":"Le résumé est obligatoire","customfield_10050":"Environnement est obligatoire"}}`)
	_, err := c.CreateIssue(NewIssue{ProjectKey: "PAY", TypeID: "10"})
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{"summary: Le résumé est obligatoire", "customfield_10050: Environnement est obligatoire"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%q misses %q", err.Error(), want)
		}
	}
}

func TestSearchProjectUsers(t *testing.T) {
	c, rec := serve(t, 200, `[{"accountId":"a1","displayName":"Sofia M.","accountType":"atlassian","active":true},{"accountId":"bot","displayName":"App","accountType":"app"}]`)
	us, err := c.SearchProjectUsers("PAY", "so")
	if err != nil {
		t.Fatal(err)
	}
	if rec.path != "/rest/api/3/user/assignable/search" || !strings.Contains(rec.query, "project=PAY") || !strings.Contains(rec.query, "query=so") {
		t.Errorf("request %s ? %s", rec.path, rec.query)
	}
	if len(us) != 1 || us[0].DisplayName != "Sofia M." {
		t.Errorf("users = %+v", us)
	}
}
