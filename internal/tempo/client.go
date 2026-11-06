package tempo

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/kevin/jira-tui/internal/i18n"
	"github.com/kevin/jira-tui/internal/jira"
)

// Client talks to the Tempo Cloud API with a Tempo API token (not the Atlassian one).
type Client struct {
	base    string
	token   string
	hc      *http.Client
	resolve func(accountID string) string // account id -> display name
	me      func() (string, error)        // current user's account id
}

func NewClient(baseURL, token string, resolve func(string) string, me func() (string, error)) *Client {
	return &Client{
		base: strings.TrimRight(baseURL, "/"), token: token,
		hc:      &http.Client{Timeout: 20 * time.Second},
		resolve: resolve, me: me,
	}
}

func (c *Client) headers() map[string]string {
	return map[string]string{"Authorization": "Bearer " + c.token}
}

type wlResponse struct {
	Metadata struct {
		Next string `json:"next"`
	} `json:"metadata"`
	Results []struct {
		ID          int    `json:"tempoWorklogId"`
		Seconds     int    `json:"timeSpentSeconds"`
		StartDate   string `json:"startDate"`
		Description string `json:"description"`
		Author      struct {
			AccountID string `json:"accountId"`
		} `json:"author"`
	} `json:"results"`
}

// Worklogs returns every worklog of an issue (numeric Jira issue id), newest first.
func (c *Client) Worklogs(issueID string) ([]Worklog, error) {
	next := c.base + "/worklogs/issue/" + url.PathEscape(issueID) + "?limit=1000"
	baseHost := hostOf(c.base)
	var out []Worklog
	for page := 0; next != "" && page < 10; page++ {
		var res wlResponse
		u := next
		if err := jira.WithRetry(func() error { return jira.DoRequest(c.hc, "GET", u, c.headers(), nil, &res) }); err != nil {
			return nil, err
		}
		for _, r := range res.Results {
			name := r.Author.AccountID
			if c.resolve != nil && name != "" {
				name = c.resolve(name)
			}
			out = append(out, Worklog{ID: r.ID, AuthorID: r.Author.AccountID, Author: name,
				Seconds: r.Seconds, Date: r.StartDate, Description: r.Description})
		}
		next = res.Metadata.Next
		if next != "" && hostOf(next) != baseHost { // never send the token to another host
			break
		}
	}
	SortNewestFirst(out)
	return out, nil
}

func hostOf(u string) string {
	p, err := url.Parse(u)
	if err != nil {
		return ""
	}
	return p.Host
}

// SortNewestFirst orders worklogs by date, then id, newest first.
func SortNewestFirst(ws []Worklog) {
	for i := 1; i < len(ws); i++ { // small lists: insertion sort keeps this dependency-free
		for j := i; j > 0 && newer(ws[j], ws[j-1]); j-- {
			ws[j], ws[j-1] = ws[j-1], ws[j]
		}
	}
}

func newer(a, b Worklog) bool {
	if a.Date != b.Date {
		return a.Date > b.Date
	}
	return a.ID > b.ID
}

// AddWorklog logs time on an issue for the current user. date is YYYY-MM-DD.
func (c *Client) AddWorklog(issueID string, seconds int, date, description string) error {
	id, err := strconv.Atoi(issueID)
	if err != nil {
		return fmt.Errorf(i18n.T("identifiant de ticket invalide : %q", "invalid ticket id: %q"), issueID)
	}
	if c.me == nil {
		return errors.New(i18n.T("compte utilisateur inconnu", "unknown user account"))
	}
	author, err := c.me()
	if err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]any{
		"issueId":          id,
		"timeSpentSeconds": seconds,
		"startDate":        date,
		"description":      description,
		"authorAccountId":  author,
	})
	return jira.WithRetry(func() error {
		return jira.DoRequest(c.hc, "POST", c.base+"/worklogs", c.headers(), payload, nil)
	})
}
