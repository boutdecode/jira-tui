package jira

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/kevin/jira-tui/internal/i18n"
)

// Error is returned for non-2xx responses.
type Error struct {
	Status     int
	Message    string
	RetryAfter time.Duration
}

func (e *Error) Error() string {
	switch e.Status {
	case 401:
		return i18n.T("authentification refusée (401) : vérifie JIRA_EMAIL / JIRA_TOKEN", "authentication refused (401): check JIRA_EMAIL / JIRA_TOKEN")
	case 403:
		msg := i18n.T("accès interdit (403)", "access forbidden (403)")
		if e.Message != "" {
			msg += i18n.T(" : ", ": ") + e.Message // Jira says why (missing permission, locked issue…)
		}
		return msg
	case 429:
		if e.RetryAfter > 0 {
			return i18n.F("trop de requêtes (429), réessaie dans %s", "too many requests (429), retry in %s", e.RetryAfter.Round(time.Second))
		}
		return i18n.T("trop de requêtes (429), abandon après plusieurs tentatives", "too many requests (429), giving up after several attempts")
	}
	return fmt.Sprintf("jira %d: %s", e.Status, e.Message)
}

type Client struct {
	base   string
	email  string
	token  string
	http   *http.Client
	me     string
	meUser User

	desks map[string]string // project key -> service desk id ("" = not a service desk project)

	mu    sync.Mutex
	names map[string]string
}

func NewClient(baseURL, email, token string) *Client {
	return &Client{
		base:  strings.TrimRight(baseURL, "/"),
		email: email,
		token: token,
		http:  &http.Client{Timeout: 20 * time.Second},
		names: map[string]string{},
	}
}

func (c *Client) BrowseURL(key string) string { return c.base + "/browse/" + key }

func (c *Client) do(method, path string, q url.Values, body, out any) error {
	return c.doWith(method, path, q, body, out, nil)
}

// doWith is do with extra request headers.
func (c *Client) doWith(method, path string, q url.Values, body, out any, extra map[string]string) error {
	var payload []byte
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		payload = b
	}
	u := c.base + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	hdr := map[string]string{"Authorization": "Basic " + base64.StdEncoding.EncodeToString([]byte(c.email+":"+c.token))}
	for k, v := range extra {
		hdr[k] = v
	}
	return WithRetry(func() error { return DoRequest(c.http, method, u, hdr, payload, out) })
}

const issueFields = "summary,status,issuetype,priority,assignee,reporter,updated,labels,created,components,timeoriginalestimate"

func (c *Client) Search(jql string) ([]Issue, error) {
	var all []Issue
	token := ""
	for page := 0; page < 4; page++ { // cap: 4 × 50 issues
		body := map[string]any{
			"jql":        jql,
			"maxResults": 50,
			"fields":     strings.Split(issueFields, ","),
		}
		if token != "" {
			body["nextPageToken"] = token
		}
		var res struct {
			Issues        []Issue `json:"issues"`
			NextPageToken string  `json:"nextPageToken"`
		}
		if err := c.do("POST", "/rest/api/3/search/jql", nil, body, &res); err != nil {
			return nil, err
		}
		all = append(all, res.Issues...)
		if res.NextPageToken == "" {
			break
		}
		token = res.NextPageToken
	}
	return all, nil
}

func (c *Client) GetIssue(key string) (Issue, error) {
	var is Issue
	q := url.Values{"fields": {issueFields + ",description"}}
	if err := c.do("GET", "/rest/api/3/issue/"+url.PathEscape(key), q, nil, &is); err != nil {
		return is, err
	}
	// Comments come from their own endpoint: it carries jsdPublic (customer visibility).
	var cs struct {
		Comments []Comment `json:"comments"`
	}
	cq := url.Values{"orderBy": {"created"}, "maxResults": {"100"}}
	if err := c.do("GET", "/rest/api/3/issue/"+url.PathEscape(key)+"/comment", cq, nil, &cs); err != nil {
		is.CommentsErr = err.Error()
		return is, nil
	}
	is.Fields.Comment.Comments = cs.Comments
	return is, nil
}

func (c *Client) Transitions(key string) ([]Transition, error) {
	var res struct {
		Transitions []Transition `json:"transitions"`
	}
	err := c.do("GET", "/rest/api/3/issue/"+url.PathEscape(key)+"/transitions", nil, nil, &res)
	return res.Transitions, err
}

func (c *Client) DoTransition(key, id string) error {
	body := map[string]any{"transition": map[string]string{"id": id}}
	return c.do("POST", "/rest/api/3/issue/"+url.PathEscape(key)+"/transitions", nil, body, nil)
}

// AddComment posts a comment. The Jira Service Management visibility is always sent
// explicitly ("internal": true|false) so nothing is left to a default.
func (c *Client) AddComment(key string, doc Doc, internal bool) error {
	body := map[string]any{
		"body": doc,
		"properties": []map[string]any{{
			"key":   "sd.public.comment",
			"value": map[string]any{"internal": internal},
		}},
	}
	return c.do("POST", "/rest/api/3/issue/"+url.PathEscape(key)+"/comment", nil, body, nil)
}

// Check verifies the credentials and returns the authenticated user.
func (c *Client) Check() (User, error) {
	var u User
	err := c.do("GET", "/rest/api/3/myself", nil, nil, &u)
	return u, err
}

// AccountID returns the authenticated user's account id (cached).
func (c *Client) AccountID() (string, error) { return c.myself() }

// UserName resolves an account id to a display name (cached; falls back to a short id).
func (c *Client) UserName(accountID string) string {
	c.mu.Lock()
	n, ok := c.names[accountID]
	c.mu.Unlock()
	if ok {
		return n
	}
	var u User
	if err := c.do("GET", "/rest/api/3/user", url.Values{"accountId": {accountID}}, nil, &u); err != nil || u.DisplayName == "" {
		short := accountID
		if len(short) > 8 {
			short = short[:8]
		}
		return short // not cached: a transient error may succeed next time
	}
	c.mu.Lock()
	c.names[accountID] = u.DisplayName
	c.mu.Unlock()
	return u.DisplayName
}

func (c *Client) myself() (string, error) {
	if c.me != "" {
		return c.me, nil
	}
	var u User
	if err := c.do("GET", "/rest/api/3/myself", nil, nil, &u); err != nil {
		return "", err
	}
	c.me, c.meUser = u.AccountID, u
	return c.me, nil
}

// Myself returns the authenticated user (cached).
func (c *Client) Myself() (User, error) {
	if _, err := c.myself(); err != nil {
		return User{}, err
	}
	return c.meUser, nil
}

func (c *Client) AssignTo(key, accountID string) error { return c.assign(key, accountID) }

// SearchAssignable lists the users that can be assigned to the issue (real people only).
func (c *Client) SearchAssignable(key, query string) ([]User, error) {
	var res []User
	q := url.Values{"issueKey": {key}, "query": {query}, "maxResults": {"20"}}
	if err := c.do("GET", "/rest/api/3/user/assignable/search", q, nil, &res); err != nil {
		return nil, err
	}
	return realPeople(res), nil
}

// realPeople drops apps and deactivated accounts.
func realPeople(us []User) []User {
	out := us[:0]
	for _, u := range us {
		if u.AccountType == "app" || (u.Active != nil && !*u.Active) {
			continue
		}
		out = append(out, u)
	}
	return out
}

// SearchMentionable lists who a mention can notify: the people who can see the issue and,
// for a Jira Service Management project, its customers (they are not Jira users, so the
// user search never finds them). A failure of the customer search is not an error: the
// project may simply not be a service desk, or the account may not be an agent.
func (c *Client) SearchMentionable(key, query string) ([]User, error) {
	var res []User
	q := url.Values{"issueKey": {key}, "query": {query}, "maxResults": {"20"}}
	if err := c.do("GET", "/rest/api/3/user/viewissue/search", q, nil, &res); err != nil {
		return nil, err
	}
	people := realPeople(res)
	seen := map[string]bool{}
	for _, u := range people {
		seen[u.AccountID] = true
	}
	for _, cu := range c.searchCustomers(key, query) {
		if !seen[cu.AccountID] {
			seen[cu.AccountID] = true
			people = append(people, cu)
		}
	}
	return people, nil
}

// serviceDeskID returns the id of the service desk of the issue's project, or "" when the
// project is not a service desk. Definitive answers are remembered; transient errors are not.
func (c *Client) serviceDeskID(key string) string {
	project, _, _ := strings.Cut(key, "-")
	c.mu.Lock()
	id, known := c.desks[project]
	c.mu.Unlock()
	if known {
		return id
	}
	var req struct {
		ServiceDeskID string `json:"serviceDeskId"`
	}
	err := c.doWith("GET", "/rest/servicedeskapi/request/"+url.PathEscape(key), nil, nil, &req, experimental)
	var je *Error
	switch {
	case err == nil:
	case errors.As(err, &je) && (je.Status == 404 || je.Status == 400 || je.Status == 403):
		req.ServiceDeskID = "" // not a request of a service desk (or not visible to this account)
	default:
		return ""
	}
	c.mu.Lock()
	if c.desks == nil {
		c.desks = map[string]string{}
	}
	c.desks[project] = req.ServiceDeskID
	c.mu.Unlock()
	return req.ServiceDeskID
}

var experimental = map[string]string{"X-ExperimentalApi": "opt-in"}

// searchCustomers finds the customers of the issue's service desk matching query.
func (c *Client) searchCustomers(key, query string) []User {
	desk := c.serviceDeskID(key)
	if desk == "" {
		return nil
	}
	var res struct {
		Values []struct {
			AccountID   string `json:"accountId"`
			DisplayName string `json:"displayName"`
			Active      *bool  `json:"active"`
		} `json:"values"`
	}
	q := url.Values{"query": {query}, "limit": {"20"}}
	if err := c.doWith("GET", "/rest/servicedeskapi/servicedesk/"+url.PathEscape(desk)+"/customer", q, nil, &res, experimental); err != nil {
		return nil
	}
	var out []User
	for _, v := range res.Values {
		if v.AccountID == "" || v.DisplayName == "" || (v.Active != nil && !*v.Active) {
			continue
		}
		out = append(out, User{AccountID: v.AccountID, DisplayName: v.DisplayName, AccountType: "customer"})
	}
	return out
}

var htmlTag = regexp.MustCompile(`<[^>]*>`)

// SearchIssues uses Jira's issue picker: recent issues for an empty query, matches otherwise.
func (c *Client) SearchIssues(query string) ([]IssueRef, error) {
	var res struct {
		Sections []struct {
			Issues []struct {
				Key         string `json:"key"`
				Summary     string `json:"summary"`
				SummaryText string `json:"summaryText"`
			} `json:"issues"`
		} `json:"sections"`
	}
	q := url.Values{"query": {query}, "showSubTasks": {"true"}}
	if err := c.do("GET", "/rest/api/3/issue/picker", q, nil, &res); err != nil {
		return nil, err
	}
	var out []IssueRef
	seen := map[string]bool{}
	for _, sec := range res.Sections {
		for _, is := range sec.Issues {
			if is.Key == "" || seen[is.Key] {
				continue
			}
			seen[is.Key] = true
			text := is.SummaryText
			if text == "" {
				text = html.UnescapeString(htmlTag.ReplaceAllString(is.Summary, ""))
			}
			out = append(out, IssueRef{Key: is.Key, Summary: text})
		}
	}
	if len(out) > 15 {
		out = out[:15]
	}
	return out, nil
}

func (c *Client) EditMeta(key string) (EditMeta, error) {
	var res struct {
		Fields map[string]struct {
			AllowedValues []Named `json:"allowedValues"`
		} `json:"fields"`
	}
	if err := c.do("GET", "/rest/api/3/issue/"+url.PathEscape(key)+"/editmeta", nil, nil, &res); err != nil {
		return EditMeta{}, err
	}
	em := EditMeta{Editable: map[string]bool{}}
	for k, f := range res.Fields {
		em.Editable[k] = true
		if k == "priority" {
			em.Priorities = f.AllowedValues
		}
	}
	return em, nil
}

// UpdateIssue changes the given fields only.
func (c *Client) UpdateIssue(key string, u FieldUpdate) error {
	fields := map[string]any{}
	if u.Summary != nil {
		fields["summary"] = *u.Summary
	}
	if u.Description != nil {
		if len(children(*u.Description)) == 0 {
			fields["description"] = nil
		} else {
			fields["description"] = *u.Description
		}
	}
	if u.PriorityID != nil {
		fields["priority"] = map[string]string{"id": *u.PriorityID}
	}
	if u.Labels != nil {
		labels := *u.Labels
		if labels == nil {
			labels = []string{}
		}
		fields["labels"] = labels
	}
	if len(fields) == 0 {
		return nil
	}
	return c.do("PUT", "/rest/api/3/issue/"+url.PathEscape(key), nil, map[string]any{"fields": fields}, nil)
}

func (c *Client) EditComment(key string, cm Comment, doc Doc) error {
	body := map[string]any{"body": doc}
	if cm.Public != nil {
		body["properties"] = []map[string]any{{
			"key":   "sd.public.comment",
			"value": map[string]any{"internal": !*cm.Public},
		}}
	}
	return c.do("PUT", "/rest/api/3/issue/"+url.PathEscape(key)+"/comment/"+url.PathEscape(cm.ID), nil, body, nil)
}

func (c *Client) DeleteComment(key, commentID string) error {
	return c.do("DELETE", "/rest/api/3/issue/"+url.PathEscape(key)+"/comment/"+url.PathEscape(commentID), nil, nil, nil)
}

func (c *Client) assign(key string, accountID any) error {
	return c.do("PUT", "/rest/api/3/issue/"+url.PathEscape(key)+"/assignee", nil,
		map[string]any{"accountId": accountID}, nil)
}

func (c *Client) AssignToMe(key string) error {
	id, err := c.myself()
	if err != nil {
		return err
	}
	return c.assign(key, id)
}

func (c *Client) Unassign(key string) error { return c.assign(key, nil) }

func (c *Client) Boards() ([]Board, error) {
	var res struct {
		Values []Board `json:"values"`
	}
	err := c.do("GET", "/rest/agile/1.0/board", url.Values{"maxResults": {"50"}}, nil, &res)
	return res.Values, err
}

func (c *Client) BoardColumns(id int) ([]Column, error) {
	var res struct {
		ColumnConfig struct {
			Columns []struct {
				Name     string `json:"name"`
				Statuses []struct {
					ID string `json:"id"`
				} `json:"statuses"`
			} `json:"columns"`
		} `json:"columnConfig"`
	}
	if err := c.do("GET", fmt.Sprintf("/rest/agile/1.0/board/%d/configuration", id), nil, nil, &res); err != nil {
		return nil, err
	}
	var cols []Column
	for _, col := range res.ColumnConfig.Columns {
		cc := Column{Name: col.Name}
		for _, s := range col.Statuses {
			cc.StatusID = append(cc.StatusID, s.ID)
		}
		cols = append(cols, cc)
	}
	return cols, nil
}

func (c *Client) BoardIssues(id int) ([]Issue, error) {
	var res struct {
		Issues []Issue `json:"issues"`
	}
	q := url.Values{"maxResults": {"100"}, "fields": {issueFields}}
	err := c.do("GET", fmt.Sprintf("/rest/agile/1.0/board/%d/issue", id), q, nil, &res)
	return res.Issues, err
}
