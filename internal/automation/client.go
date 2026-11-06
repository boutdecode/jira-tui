package automation

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kevin/jira-tui/internal/i18n"
	"github.com/kevin/jira-tui/internal/jira"
)

const maxPages = 5

// Client calls the public Automation REST API with the Jira email + classic API token.
type Client struct {
	site, root   string
	email, token string
	hc           *http.Client

	mu      sync.Mutex
	cloudID string
}

// NewClient builds a client. cloudID and root may be empty (detected / default).
func NewClient(site, email, token, cloudID, root string) *Client {
	site = strings.TrimRight(site, "/")
	if root == "" {
		root = site + "/gateway/api/automation/public"
	}
	return &Client{site: site, root: strings.TrimRight(root, "/"), email: email, token: token, cloudID: cloudID,
		hc: &http.Client{Timeout: 20 * time.Second}}
}

func (c *Client) headers() map[string]string {
	return map[string]string{"Authorization": "Basic " + base64.StdEncoding.EncodeToString([]byte(c.email+":"+c.token))}
}

// CloudID returns the site's cloud id (configured, else read once from /_edge/tenant_info).
func (c *Client) CloudID() (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cloudID != "" {
		return c.cloudID, nil
	}
	var res struct {
		CloudID string `json:"cloudId"`
	}
	err := jira.WithRetry(func() error { return jira.DoRequest(c.hc, "GET", c.site+"/_edge/tenant_info", nil, nil, &res) })
	if err != nil {
		return "", fmt.Errorf(i18n.T("cloudId introuvable (%v) — définissez cloud_id dans config.yaml", "cloudId not found (%v) — set cloud_id in config.yaml"), err)
	}
	if res.CloudID == "" {
		return "", errors.New(i18n.T("cloudId introuvable — définissez cloud_id dans config.yaml", "cloudId not found — set cloud_id in config.yaml"))
	}
	c.cloudID = res.CloudID
	return c.cloudID, nil
}

// call sends one request and adds a hint to authentication failures.
func (c *Client) call(method, u string, payload []byte, out any) error {
	err := jira.WithRetry(func() error { return jira.DoRequest(c.hc, method, u, c.headers(), payload, out) })
	var je *jira.Error
	if errors.As(err, &je) && je.Status == 401 {
		return errors.New(i18n.T("authentification refusée (401) — l'API Automation exige un token API classique (non scopé) et un compte autorisé", "authentication refused (401) — the Automation API requires a classic (unscoped) API token and an authorized account"))
	}
	return err
}

// List returns the manual rules that apply to an issue (numeric Jira issue id).
func (c *Client) List(issueID string) ([]Rule, error) {
	cid, err := c.CloudID()
	if err != nil {
		return nil, err
	}
	base := c.root + "/jira/" + cid + "/rest/v1/rule/manual/search"
	body, _ := json.Marshal(map[string]any{"objects": []string{IssueARI(cid, issueID)}})

	var rules []Rule
	prev, query := "", ""
	for page := 0; page < maxPages; page++ {
		var raw map[string]any
		if page == 0 {
			err = c.call("POST", base, body, &raw)
		} else {
			err = c.call("GET", base+"?"+query, nil, &raw)
		}
		if err != nil {
			return nil, err
		}
		rules = Merge(rules, parseRules(raw))
		query = nextQuery(raw)
		if query == "" || query == prev {
			break
		}
		prev = query
	}
	return rules, nil
}

// Run runs a rule on an issue. values holds what the user typed for rule.Inputs.
// Jira answers "launched" (SUCCESS), not "finished": the rule runs asynchronously.
func (c *Client) Run(rule Rule, issueID string, values map[string]string) error {
	cid, err := c.CloudID()
	if err != nil {
		return err
	}
	ari := IssueARI(cid, issueID)
	payload := map[string]any{"objects": []string{ari}}
	if len(rule.Inputs) > 0 {
		ins := map[string]any{}
		for _, in := range rule.Inputs {
			v, ok := values[in.Name]
			if !ok {
				v = in.Default
			}
			switch in.Type {
			case "NUMBER":
				f, err := strconv.ParseFloat(strings.Replace(strings.TrimSpace(v), ",", ".", 1), 64)
				if err != nil {
					return fmt.Errorf(i18n.T("« %s » doit être un nombre", "“%s” must be a number"), in.Name)
				}
				var num any = f
				if f == math.Trunc(f) && math.Abs(f) < 1e15 {
					num = int64(f)
				}
				ins[in.Name] = map[string]any{"inputType": "NUMBER", "value": num}
			case "TEXT":
				ins[in.Name] = map[string]any{"inputType": "TEXT", "value": v}
			default:
				return fmt.Errorf(i18n.T("saisie « %s » de type %s non gérée — lancez la règle depuis Jira", "input “%s” of type %s is not supported — run the rule from Jira"), in.Name, in.Type)
			}
		}
		payload["userInputs"] = ins
	}
	body, _ := json.Marshal(payload)

	var res map[string]any
	u := c.root + "/jira/" + cid + "/rest/v1/rule/manual/" + url.PathEscape(rule.ID) + "/invocation"
	if err := c.call("POST", u, body, &res); err != nil {
		return err
	}
	status := ""
	if v, ok := res[ari]; ok {
		status = fmt.Sprint(v)
	} else if len(res) == 1 {
		for _, v := range res {
			status = fmt.Sprint(v)
		}
	}
	switch status {
	case "SUCCESS":
		return nil
	case "INVALID_TARGET_OBJECT":
		return ErrNotApplicable
	case "":
		return fmt.Errorf(i18n.T("réponse inattendue de Jira : %v", "unexpected response from Jira: %v"), res)
	}
	return fmt.Errorf(i18n.T("statut inattendu renvoyé par Jira : %s", "unexpected status returned by Jira: %s"), status)
}

// --- tolerant parsing: the response schema is not documented in detail ---

func firstString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		switch v := m[k].(type) {
		case string:
			if v != "" {
				return v
			}
		case float64:
			return strconv.FormatFloat(v, 'f', -1, 64)
		}
	}
	return ""
}

func parseRules(raw map[string]any) []Rule {
	var arr []any
	for _, k := range []string{"data", "results", "rules", "values"} {
		if a, ok := raw[k].([]any); ok {
			arr = a
			break
		}
	}
	var out []Rule
	for _, it := range arr {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		src := m
		if n, ok := m["rule"].(map[string]any); ok { // some shapes wrap the rule
			src = n
		}
		id := firstString(src, "id", "ruleId", "uuid", "ruleUuid")
		if id == "" {
			id = firstString(m, "id", "ruleId", "uuid", "ruleUuid")
		}
		if id == "" {
			continue
		}
		name := firstString(src, "name", "title", "ruleName", "label")
		if name == "" {
			name = firstString(m, "name", "title", "ruleName", "label")
		}
		if name == "" {
			name = id
		}
		r := Rule{ID: id, Name: name, Inputs: parseInputs(m)}
		if len(r.Inputs) == 0 && src != nil {
			r.Inputs = parseInputs(src)
		}
		out = append(out, r)
	}
	return out
}

func parseInputs(m map[string]any) []Input {
	var def any
	for _, k := range []string{"inputs", "userInputs", "inputDefinitions", "input"} {
		if v, ok := m[k]; ok && v != nil {
			def = v
			break
		}
	}
	mk := func(name string, d any) Input {
		in := Input{Name: name, Type: "TEXT"}
		if dm, ok := d.(map[string]any); ok {
			if t := firstString(dm, "inputType", "type"); t != "" {
				in.Type = strings.ToUpper(t)
			}
			switch v := dm["value"].(type) {
			case string:
				in.Default = v
			case float64:
				in.Default = strconv.FormatFloat(v, 'f', -1, 64)
			default:
				in.Default = firstString(dm, "defaultValue", "default")
			}
		}
		return in
	}
	var out []Input
	switch d := def.(type) {
	case map[string]any:
		names := make([]string, 0, len(d))
		for n := range d {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			out = append(out, mk(n, d[n]))
		}
	case []any:
		for _, e := range d {
			if em, ok := e.(map[string]any); ok {
				if n := firstString(em, "name", "key", "id", "label"); n != "" {
					out = append(out, mk(n, em))
				}
			}
		}
	}
	return out
}

// nextQuery extracts the query string of links.next ("now returns only query parameters").
// It never returns a host: the client always stays on its own base URL.
func nextQuery(raw map[string]any) string {
	links, _ := raw["links"].(map[string]any)
	next, _ := links["next"].(string)
	if next == "" {
		return ""
	}
	if strings.Contains(next, "://") {
		if u, err := url.Parse(next); err == nil {
			return u.RawQuery
		}
		return ""
	}
	if i := strings.Index(next, "?"); i >= 0 {
		next = next[i+1:]
	}
	return strings.TrimPrefix(next, "?")
}
