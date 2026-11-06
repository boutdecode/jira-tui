package jira

import (
	"net/url"
	"strings"
)

// Project is a Jira project the user can create issues in.
type Project struct {
	ID   string `json:"id"`
	Key  string `json:"key"`
	Name string `json:"name"`
	Type string `json:"projectTypeKey"` // software | business | service_desk
}

// IssueType is an issue type of a project; sub-tasks need a parent.
type IssueType struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Subtask bool   `json:"subtask"`
}

// NewIssueMeta says what the create form of a project and issue type holds.
type NewIssueMeta struct {
	Priorities  []Named  // allowed priorities (empty when the field is not on the form)
	HasPriority bool     // the priority field is on the form
	HasLabels   bool     // the labels field is on the form
	HasDesc     bool     // the description field is on the form
	HasAssignee bool     // the assignee field is on the form
	Unsupported []string // required fields without default that the app cannot fill (custom fields…)
}

// NewIssue is what to create. Zero fields are left out of the request.
type NewIssue struct {
	ProjectKey  string
	TypeID      string
	Summary     string
	Description *Doc
	PriorityID  string
	Labels      []string
	AssignToMe  bool   // false: the project's default assignee applies
	ParentKey   string // sub-tasks
}

// createSupported are the fields the app fills itself or Jira fills for it.
var createSupported = map[string]bool{
	"summary": true, "description": true, "priority": true, "labels": true, "assignee": true,
	"reporter": true, "project": true, "issuetype": true, "parent": true,
}

// Projects lists the projects the user can create issues in, filtered by query.
func (c *Client) Projects(query string) ([]Project, error) {
	var res struct {
		Values []Project `json:"values"`
	}
	q := url.Values{"action": {"create"}, "query": {query}, "maxResults": {"30"}, "orderBy": {"name"}}
	if err := c.do("GET", "/rest/api/3/project/search", q, nil, &res); err != nil {
		return nil, err
	}
	return res.Values, nil
}

// IssueTypes lists the issue types the user can create in the project.
func (c *Client) IssueTypes(project string) ([]IssueType, error) {
	var res struct {
		IssueTypes []IssueType `json:"issueTypes"`
		Values     []IssueType `json:"values"` // older shape of the same endpoint
	}
	q := url.Values{"maxResults": {"50"}}
	if err := c.do("GET", "/rest/api/3/issue/createmeta/"+url.PathEscape(project)+"/issuetypes", q, nil, &res); err != nil {
		return nil, err
	}
	if len(res.IssueTypes) > 0 {
		return res.IssueTypes, nil
	}
	return res.Values, nil
}

// NewIssueMeta reads the create form of a project and issue type.
func (c *Client) NewIssueMeta(project, typeID string) (NewIssueMeta, error) {
	type field struct {
		ID            string  `json:"fieldId"`
		Name          string  `json:"name"`
		Required      bool    `json:"required"`
		HasDefault    bool    `json:"hasDefaultValue"`
		AllowedValues []Named `json:"allowedValues"`
	}
	var res struct {
		Fields []field `json:"fields"`
		Values []field `json:"values"` // older shape of the same endpoint
	}
	q := url.Values{"maxResults": {"100"}}
	path := "/rest/api/3/issue/createmeta/" + url.PathEscape(project) + "/issuetypes/" + url.PathEscape(typeID)
	if err := c.do("GET", path, q, nil, &res); err != nil {
		return NewIssueMeta{}, err
	}
	fields := res.Fields
	if len(fields) == 0 {
		fields = res.Values
	}
	var meta NewIssueMeta
	for _, f := range fields {
		switch f.ID {
		case "priority":
			meta.HasPriority, meta.Priorities = true, f.AllowedValues
		case "labels":
			meta.HasLabels = true
		case "description":
			meta.HasDesc = true
		case "assignee":
			meta.HasAssignee = true
		}
		if f.Required && !f.HasDefault && !createSupported[f.ID] {
			name := f.Name
			if name == "" {
				name = f.ID
			}
			meta.Unsupported = append(meta.Unsupported, name)
		}
	}
	return meta, nil
}

// CreateIssue creates the issue and returns its key.
func (c *Client) CreateIssue(in NewIssue) (IssueRef, error) {
	fields := map[string]any{
		"project":   map[string]string{"key": in.ProjectKey},
		"issuetype": map[string]string{"id": in.TypeID},
		"summary":   in.Summary,
	}
	if in.Description != nil && len(children(*in.Description)) > 0 {
		fields["description"] = *in.Description
	}
	if in.PriorityID != "" {
		fields["priority"] = map[string]string{"id": in.PriorityID}
	}
	if len(in.Labels) > 0 {
		fields["labels"] = in.Labels
	}
	if in.AssignToMe {
		id, err := c.myself()
		if err != nil {
			return IssueRef{}, err
		}
		fields["assignee"] = map[string]string{"accountId": id}
	}
	if in.ParentKey != "" {
		fields["parent"] = map[string]string{"key": in.ParentKey}
	}
	var res struct {
		Key string `json:"key"`
	}
	if err := c.do("POST", "/rest/api/3/issue", nil, map[string]any{"fields": fields}, &res); err != nil {
		return IssueRef{}, err
	}
	return IssueRef{Key: res.Key, Summary: in.Summary}, nil
}

// SearchProjectUsers lists the people who can be assigned issues of the project: the ones a
// description being written for a not yet existing issue can mention.
func (c *Client) SearchProjectUsers(project, query string) ([]User, error) {
	var res []User
	q := url.Values{"project": {strings.TrimSpace(project)}, "query": {query}, "maxResults": {"20"}}
	if err := c.do("GET", "/rest/api/3/user/assignable/search", q, nil, &res); err != nil {
		return nil, err
	}
	return realPeople(res), nil
}
