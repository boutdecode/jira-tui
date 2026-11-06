package jira

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
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
		return i18n.T("accès interdit (403)", "access forbidden (403)")
	case 429:
		if e.RetryAfter > 0 {
			return i18n.F("trop de requêtes (429), réessaie dans %s", "too many requests (429), retry in %s", e.RetryAfter.Round(time.Second))
		}
		return i18n.T("trop de requêtes (429), abandon après plusieurs tentatives", "too many requests (429), giving up after several attempts")
	}
	return fmt.Sprintf("jira %d: %s", e.Status, e.Message)
}

type Client struct {
	base  string
	email string
	token string
	http  *http.Client
	me    string

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
func (c *Client) AddComment(key, text string, internal bool) error {
	body := map[string]any{
		"body": TextToADF(text),
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
	c.me = u.AccountID
	return c.me, nil
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
