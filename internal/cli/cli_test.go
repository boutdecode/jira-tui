package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kevin/jira-tui/internal/automation"
	"github.com/kevin/jira-tui/internal/config"
	"github.com/kevin/jira-tui/internal/i18n"
	"github.com/kevin/jira-tui/internal/jira"
	"github.com/kevin/jira-tui/internal/mock"
)

// spy records what the commands write on top of the in-memory Jira.
type spy struct {
	*mock.Service
	jqls    []string
	writes  []string
	comment struct {
		key      string
		body     jira.Doc
		internal bool
	}
	updates []jira.FieldUpdate
	created []jira.NewIssue
	assign  string // the account id given to AssignTo
	trans   string // the transition id
}

func (s *spy) Search(jql string) ([]jira.Issue, error) {
	s.jqls = append(s.jqls, jql)
	return s.Service.Search(jql)
}
func (s *spy) AddComment(key string, body jira.Doc, internal bool) error {
	s.writes = append(s.writes, "comment")
	s.comment.key, s.comment.body, s.comment.internal = key, body, internal
	return s.Service.AddComment(key, body, internal)
}
func (s *spy) DoTransition(key, id string) error {
	s.writes = append(s.writes, "transition")
	s.trans = id
	return s.Service.DoTransition(key, id)
}
func (s *spy) AssignToMe(key string) error {
	s.writes = append(s.writes, "assign-me")
	return s.Service.AssignToMe(key)
}
func (s *spy) AssignTo(key, id string) error {
	s.writes = append(s.writes, "assign")
	s.assign = id
	return s.Service.AssignTo(key, id)
}
func (s *spy) Unassign(key string) error {
	s.writes = append(s.writes, "unassign")
	return s.Service.Unassign(key)
}
func (s *spy) UpdateIssue(key string, u jira.FieldUpdate) error {
	s.writes = append(s.writes, "update")
	s.updates = append(s.updates, u)
	return s.Service.UpdateIssue(key, u)
}
func (s *spy) CreateIssue(in jira.NewIssue) (jira.IssueRef, error) {
	s.writes = append(s.writes, "create")
	s.created = append(s.created, in)
	return s.Service.CreateIssue(in)
}

// timeSpy records the time that is logged.
type timeSpy struct {
	*mock.Time
	logged []struct {
		id, date, desc string
		secs           int
	}
}

func (t *timeSpy) AddWorklog(id string, secs int, date, desc string) error {
	t.logged = append(t.logged, struct {
		id, date, desc string
		secs           int
	}{id, date, desc, secs})
	return t.Time.AddWorklog(id, secs, date, desc)
}

// rig is one command-line session.
type rig struct {
	t               *testing.T
	svc             *spy
	time            *timeSpy
	auto            *mock.Automations
	cfg             config.Config
	stdin           string
	tty             bool
	noTempo, noAuto bool
}

func newRig(t *testing.T) *rig {
	t.Helper()
	ms := mock.New()
	return &rig{
		t: t, svc: &spy{Service: ms}, time: &timeSpy{Time: mock.NewTime()}, auto: mock.NewAutomations(ms),
		cfg: config.Config{JQL: config.DefaultJQL, HoursPerDay: 8, Filters: []config.Filter{{Name: "Mes bugs", JQL: "type = Bug ORDER BY created DESC"}}},
	}
}

// run executes jira-tui <args>; the result is the exit code and the two output streams.
func (g *rig) run(args ...string) (int, string, string) {
	g.t.Helper()
	var out, errb bytes.Buffer
	env := IO{Stdin: strings.NewReader(g.stdin), StdinIsTTY: g.tty, Stdout: &out, Stderr: &errb}
	build := func(string, bool) (Deps, error) {
		d := Deps{Svc: g.svc, Config: g.cfg, Now: func() time.Time { return time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC) }}
		if !g.noTempo {
			d.Time = g.time
		}
		if !g.noAuto {
			d.Auto = g.auto
		}
		return d, nil
	}
	code := Run(args, env, build)
	return code, out.String(), errb.String()
}

func mustCode(t *testing.T, want int, got int, stdout, stderr string) {
	t.Helper()
	if got != want {
		t.Fatalf("exit code %d, want %d\nstdout: %s\nstderr: %s", got, want, stdout, stderr)
	}
}

func jsonOf(t *testing.T, s string, v any) {
	t.Helper()
	if err := json.Unmarshal([]byte(s), v); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, s)
	}
}

func nodes(t *testing.T, doc any) []string {
	t.Helper()
	raw, _ := json.Marshal(doc)
	var d map[string]any
	_ = json.Unmarshal(raw, &d)
	var out []string
	for _, b := range d["content"].([]any) {
		inl, _ := b.(map[string]any)["content"].([]any)
		for _, n := range inl {
			out = append(out, n.(map[string]any)["type"].(string))
		}
	}
	return out
}

func TestUnknownCommandAndHelp(t *testing.T) {
	g := newRig(t)
	if code, _, stderr := g.run("nope"); code != 2 || !strings.Contains(stderr, "commande inconnue") {
		t.Errorf("unknown: %d %q", code, stderr)
	}
	for _, name := range commandNames() {
		code, stdout, stderr := g.run(name, "-h")
		mustCode(t, 0, code, stdout, stderr)
		if !strings.Contains(stdout, "Usage : jira-tui "+name) || !strings.Contains(stdout, "-output-json") || !strings.Contains(stdout, "-demo") {
			t.Errorf("%s -h:\n%s", name, stdout)
		}
	}
	// write commands get --dry-run, read commands do not
	if _, out, _ := g.run("comment", "-h"); !strings.Contains(out, "-dry-run") || !strings.Contains(out, "-internal") {
		t.Errorf("comment -h:\n%s", out)
	}
	if _, out, _ := g.run("list", "-h"); strings.Contains(out, "dry-run") {
		t.Errorf("list has no --dry-run:\n%s", out)
	}
	// an unknown option is a usage error that points to the help
	code, _, stderr := g.run("list", "--nope")
	if code != 2 || !strings.Contains(stderr, "jira-tui list -h") {
		t.Errorf("unknown option: %d %q", code, stderr)
	}
}

func TestList(t *testing.T) {
	g := newRig(t)
	code, out, errs := g.run("list", "--limit", "3")
	mustCode(t, 0, code, out, errs)
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 3 {
		t.Fatalf("3 lines expected:\n%s", out)
	}
	cols := strings.Split(lines[0], "\t")
	if len(cols) != 5 || cols[0] != "PAY-142" || cols[1] != "En cours" || cols[2] != "Camille R." || cols[3] != "High" {
		t.Errorf("columns = %q", cols)
	}
	if g.svc.jqls[0] != config.DefaultJQL {
		t.Errorf("default query = %q", g.svc.jqls[0])
	}

	code, out, errs = g.run("list", "--output-json", "--limit", "2")
	mustCode(t, 0, code, out, errs)
	var rows []issueRow
	jsonOf(t, out, &rows)
	if len(rows) != 2 || rows[0].Key != "PAY-142" || rows[0].URL != "https://example.atlassian.net/browse/PAY-142" || rows[0].Assignee == nil || *rows[0].Assignee != "Camille R." {
		t.Errorf("rows = %+v", rows)
	}

	// --mine / --open build the query, a filter or a JQL replace it
	g.svc.jqls = nil
	g.run("list", "--mine", "--open")
	g.run("list", "--filter", "mes")
	g.run("list", "project", "=", "PAY", "AND", "type", "=", "Bug")
	want := []string{"assignee = currentUser() AND resolution = Unresolved ORDER BY updated DESC", "type = Bug ORDER BY created DESC", "project = PAY AND type = Bug"}
	for i, w := range want {
		if i >= len(g.svc.jqls) || g.svc.jqls[i] != w {
			t.Errorf("query %d = %q, want %q", i, g.svc.jqls, w)
		}
	}
	for _, args := range [][]string{{"list", "--mine", "project = PAY"}, {"list", "--filter", "x", "--open"}, {"list", "--limit", "0"}} {
		if code, _, _ := g.run(args...); code != 2 {
			t.Errorf("%v: code %d, want 2", args, code)
		}
	}
	if code, _, stderr := g.run("list", "--filter", "inconnu"); code != 1 || !strings.Contains(stderr, "Mes bugs") {
		t.Errorf("unknown filter: %d %q", code, stderr)
	}
	// an empty result is an empty output, not an error
	if code, out, _ := g.run("list", "--output-json", "key", "=", "ZZZ-1"); code != 0 {
		t.Errorf("code %d", code)
	} else if strings.TrimSpace(out) == "" {
		t.Error("JSON output must always be valid")
	}
}

func TestShowAndWhoami(t *testing.T) {
	g := newRig(t)
	code, out, errs := g.run("show", "pay-142")
	mustCode(t, 0, code, out, errs)
	for _, want := range []string{"PAY-142  Le paiement échoue", "https://example.atlassian.net/browse/PAY-142", "Statut: En cours", "Assigné: Camille R.", "Temps passé: 5 h 15 min", "Commentaires (3)", "(interne)", "(client)", "erreur 502"} {
		if !strings.Contains(out, want) {
			t.Errorf("show misses %q\n%s", want, out)
		}
	}
	code, out, _ = g.run("show", "PAY-142", "--output-json")
	mustCode(t, 0, code, out, "")
	var tk struct {
		Key      string
		Comments []struct{ Visibility string }
	}
	jsonOf(t, strings.ToLower(out), &tk) // keys are lower case in the JSON
	if len(tk.Comments) != 3 {
		t.Errorf("comments = %+v", tk)
	}
	for args, want := range map[string]int{"show": 2, "show nope": 2, "show PAY-1 PAY-2": 2, "show PAY-999": 1} {
		if code, _, _ := g.run(strings.Fields(args)...); code != want {
			t.Errorf("%q: code %d, want %d", args, code, want)
		}
	}
	// no Tempo: the time is simply absent
	g.noTempo = true
	if _, out, _ := g.run("show", "PAY-142"); strings.Contains(out, "Temps passé") {
		t.Errorf("no Tempo, no time:\n%s", out)
	}

	code, out, _ = g.run("whoami")
	if code != 0 || strings.TrimSpace(out) != "Kevin\thttps://example.atlassian.net" {
		t.Errorf("whoami: %d %q", code, out)
	}
	_, out, _ = g.run("whoami", "--output-json")
	var who map[string]string
	jsonOf(t, out, &who)
	if who["name"] != "Kevin" || who["site"] != "https://example.atlassian.net" || who["account_id"] == "" {
		t.Errorf("whoami json = %v", who)
	}
}

func TestComment(t *testing.T) {
	g := newRig(t)
	code, out, errs := g.run("comment", "PAY-142", "Déployé en prod")
	mustCode(t, 0, code, out, errs)
	if g.svc.comment.internal || g.svc.comment.key != "PAY-142" || !strings.Contains(out, "visible du client") {
		t.Errorf("default must be customer-visible and say so: %+v %q", g.svc.comment, out)
	}
	if got := jira.ADFToText(g.svc.comment.body); got != "Déployé en prod" {
		t.Errorf("body = %q", got)
	}
	code, out, _ = g.run("comment", "PAY-142", "note interne", "--internal")
	if code != 0 || !g.svc.comment.internal || !strings.Contains(out, "(interne)") {
		t.Errorf("internal: %d %+v %q", code, g.svc.comment, out)
	}
	// several words are one text; Markdown applies; #KEY becomes a smart link
	g.run("comment", "PAY-142", "voir", "**ceci**", "et", "#PAY-139")
	if got := strings.Join(nodes(t, g.svc.comment.body), ","); got != "text,text,text,inlineCard" {
		t.Errorf("nodes = %s", got)
	}
	// "--" ends the options: a text can start with a dash
	g.run("comment", "PAY-142", "--", "-x est un texte")
	if got := jira.ADFToText(g.svc.comment.body); got != "-x est un texte" {
		t.Errorf("body = %q", got)
	}
	// JSON output
	_, out, _ = g.run("comment", "PAY-142", "x", "--output-json", "--internal")
	var res map[string]string
	jsonOf(t, out, &res)
	if res["key"] != "PAY-142" || res["visibility"] != "internal" || res["url"] == "" {
		t.Errorf("json = %v", res)
	}
}

func TestCommentTextSources(t *testing.T) {
	g := newRig(t)
	// "-" reads standard input
	g.stdin, g.tty = "texte du pipe\n\nsecond paragraphe\n", false
	code, out, errs := g.run("comment", "PAY-142", "-")
	mustCode(t, 0, code, out, errs)
	if got := jira.ADFToText(g.svc.comment.body); got != "texte du pipe\n\nsecond paragraphe" {
		t.Errorf("stdin text = %q", got)
	}
	// no argument and a pipe: standard input too
	g.stdin = "sans argument"
	if code, _, _ := g.run("comment", "PAY-142"); code != 0 || jira.ADFToText(g.svc.comment.body) != "sans argument" {
		t.Errorf("implicit stdin: %d %q", code, jira.ADFToText(g.svc.comment.body))
	}
	// a terminal with no text: usage error, nothing sent
	g.svc.writes, g.tty = nil, true
	if code, _, stderr := g.run("comment", "PAY-142"); code != 2 || !strings.Contains(stderr, "vide") || len(g.svc.writes) != 0 {
		t.Errorf("empty: %d %q %v", code, stderr, g.svc.writes)
	}
	// blank text too
	g.tty = false
	g.stdin = "  \n"
	if code, _, _ := g.run("comment", "PAY-142"); code != 2 || len(g.svc.writes) != 0 {
		t.Errorf("blank: %d %v", code, g.svc.writes)
	}
	// a file
	path := filepath.Join(t.TempDir(), "note.md")
	_ = os.WriteFile(path, []byte("depuis un fichier"), 0o600)
	g.tty = true
	if code, _, _ := g.run("comment", "PAY-142", "--file", path); code != 0 || jira.ADFToText(g.svc.comment.body) != "depuis un fichier" {
		t.Errorf("file: %q", jira.ADFToText(g.svc.comment.body))
	}
	if code, _, _ := g.run("comment", "PAY-142", "--file", path+".absent"); code != 1 {
		t.Errorf("missing file: code %d", code)
	}
}

func TestCommentMentions(t *testing.T) {
	g := newRig(t)
	code, out, errs := g.run("comment", "PAY-142", "cc @Sofia M. merci", "--mention", "sofia")
	mustCode(t, 0, code, out, errs)
	if got := strings.Join(nodes(t, g.svc.comment.body), ","); got != "text,mention,text" {
		t.Fatalf("nodes = %s", got)
	}
	// a name that matches nobody, or several people, is an error: nothing is sent
	g.svc.writes = nil
	if code, _, stderr := g.run("comment", "PAY-142", "x", "--mention", "zzz"); code != 1 || !strings.Contains(stderr, "aucune personne") {
		t.Errorf("unknown: %d %q", code, stderr)
	}
	if code, _, stderr := g.run("comment", "PAY-142", "x", "--mention", "a"); code != 1 || !strings.Contains(stderr, "ambigu") || !strings.Contains(stderr, "Camille R.") {
		t.Errorf("ambiguous: %d %q", code, stderr)
	}
	if len(g.svc.writes) != 0 {
		t.Errorf("a failed mention must not send anything: %v", g.svc.writes)
	}
	// service desk customers can be mentioned too
	if code, _, _ := g.run("comment", "PAY-142", "bonjour @Julie Martin", "--mention", "julie"); code != 0 {
		t.Errorf("customer mention: code %d", code)
	}
}

func TestDryRunWritesNothing(t *testing.T) {
	g := newRig(t)
	for _, args := range [][]string{
		{"comment", "PAY-142", "x", "--dry-run"},
		{"transition", "PAY-142", "En revue", "--dry-run"},
		{"assign", "PAY-142", "me", "--dry-run"},
		{"assign", "PAY-142", "none", "--dry-run"},
		{"assign", "PAY-142", "sofia", "--dry-run"},
		{"edit", "PAY-142", "--title", "Nouveau", "--priority", "low", "--add-label", "x", "--dry-run"},
		{"create", "--project", "PAY", "--title", "T", "--priority", "high", "--assign", "me", "--dry-run"},
		{"log", "PAY-142", "1h", "--dry-run"},
		{"auto", "PAY-142", "Escalader au support N2", "--input", "Motif=x", "--dry-run"},
	} {
		code, out, errs := g.run(args...)
		mustCode(t, 0, code, out, errs)
		var v map[string]any
		jsonOf(t, out, &v)
		if v["action"] == nil || v["action"] == "" {
			t.Errorf("%v: no action in %s", args, out)
		}
	}
	if len(g.svc.writes) != 0 || len(g.time.logged) != 0 || len(g.auto.Runs) != 0 {
		t.Errorf("--dry-run wrote: %v %v %v", g.svc.writes, g.time.logged, g.auto.Runs)
	}
}

func TestTransition(t *testing.T) {
	g := newRig(t)
	code, out, errs := g.run("transition", "PAY-142")
	mustCode(t, 0, code, out, errs)
	if !strings.Contains(out, "3\tEn revue\tEn revue") {
		t.Errorf("list:\n%s", out)
	}
	_, out, _ = g.run("transition", "PAY-142", "--list", "--output-json")
	var rows []map[string]string
	jsonOf(t, out, &rows)
	if len(rows) != 4 || rows[2]["id"] != "3" || rows[2]["to"] != "En revue" {
		t.Errorf("json = %v", rows)
	}
	for query, id := range map[string]string{"En revue": "3", "en revue": "3", "term": "4", "À faire": "1", "en c": "2"} {
		code, out, errs := g.run("transition", "PAY-142", query)
		mustCode(t, 0, code, out, errs)
		if g.svc.trans != id || !strings.Contains(out, "PAY-142 → ") {
			t.Errorf("%q: transition %q, output %q", query, g.svc.trans, out)
		}
	}
	// "en" is the start of two statuses; "zz" of none
	g.svc.writes = nil
	code, _, stderr := g.run("transition", "PAY-142", "en")
	if code != 1 || !strings.Contains(stderr, "ambigu") || !strings.Contains(stderr, "En cours") || !strings.Contains(stderr, "En revue") {
		t.Errorf("ambiguous: %d %q", code, stderr)
	}
	if code, _, stderr := g.run("transition", "PAY-142", "zz"); code != 1 || !strings.Contains(stderr, "choix") {
		t.Errorf("unknown: %d %q", code, stderr)
	}
	if len(g.svc.writes) != 0 {
		t.Errorf("nothing must be sent: %v", g.svc.writes)
	}
	if code, _, _ := g.run("transition", "PAY-142", "a", "b"); code != 2 {
		t.Errorf("extra argument: %d", code)
	}
	_, out, _ = g.run("transition", "PAY-142", "Terminé", "--output-json")
	var res map[string]string
	jsonOf(t, out, &res)
	if res["status"] != "Terminé" || res["key"] != "PAY-142" {
		t.Errorf("json = %v", res)
	}
}

func TestAssign(t *testing.T) {
	g := newRig(t)
	for _, c := range []struct{ who, write, want string }{
		{"me", "assign-me", "assigné à Kevin"},
		{"moi", "assign-me", "assigné à Kevin"},
		{"none", "unassign", "désassigné"},
		{"sofia", "assign", "assigné à Sofia M."},
		{"Léo B.", "assign", "assigné à Léo B."},
	} {
		g.svc.writes = nil
		code, out, errs := g.run("assign", "PAY-142", c.who)
		mustCode(t, 0, code, out, errs)
		if len(g.svc.writes) != 1 || g.svc.writes[0] != c.write || !strings.Contains(out, c.want) {
			t.Errorf("%s: writes %v output %q", c.who, g.svc.writes, c.want+" ≠ "+out)
		}
	}
	if g.svc.assign != "Léo B." {
		t.Errorf("account id = %q", g.svc.assign)
	}
	g.svc.writes = nil
	if code, _, stderr := g.run("assign", "PAY-142", "e"); code != 1 || !strings.Contains(stderr, "ambigu") {
		t.Errorf("ambiguous: %d %q", code, stderr)
	}
	if code, _, stderr := g.run("assign", "PAY-142", "zzz"); code != 1 || !strings.Contains(stderr, "aucune personne") {
		t.Errorf("unknown: %d %q", code, stderr)
	}
	// customers are not Jira users: they cannot be assigned
	if code, _, _ := g.run("assign", "PAY-142", "julie"); code != 1 {
		t.Errorf("a customer is not assignable: code %d", code)
	}
	if code, _, _ := g.run("assign", "PAY-142"); code != 2 {
		t.Errorf("missing who: %d", code)
	}
	if len(g.svc.writes) != 0 {
		t.Errorf("writes = %v", g.svc.writes)
	}
	_, out, _ := g.run("assign", "PAY-142", "none", "--output-json")
	var res map[string]any
	jsonOf(t, out, &res)
	if v, ok := res["assignee"]; !ok || v != nil {
		t.Errorf("unassigned must be null: %v", res)
	}
}

func TestEdit(t *testing.T) {
	g := newRig(t)
	code, out, errs := g.run("edit", "PAY-142", "--title", " Nouveau titre ", "--priority", "low", "--label", "a", "--label", "b", "--label", "a")
	mustCode(t, 0, code, out, errs)
	u := g.svc.updates[0]
	if *u.Summary != "Nouveau titre" || *u.PriorityID != "3" || strings.Join(*u.Labels, "|") != "a|b" || u.Description != nil {
		t.Errorf("update = %+v", u)
	}
	if !strings.Contains(out, "PAY-142 modifié : titre, priorité, étiquettes") {
		t.Errorf("output = %q", out)
	}
	// labels: add and remove keep the others (the labels are now a and b)
	g.run("edit", "PAY-142", "--add-label", "urgent", "--add-label", "a", "--remove-label", "b", "--remove-label", "absent")
	if got := strings.Join(*g.svc.updates[1].Labels, "|"); got != "a|urgent" {
		t.Errorf("labels = %q", got)
	}
	// a description: Markdown, from the argument or standard input; "" clears it
	g.run("edit", "PAY-142", "--description", "# Titre\n\n- a\n- b")
	if got := strings.Join(nodes(t, *g.svc.updates[2].Description), ","); !strings.Contains(got, "text") {
		t.Errorf("description nodes = %s", got)
	}
	g.stdin = "depuis le pipe"
	g.run("edit", "PAY-142", "--description", "-")
	if got := jira.ADFToText(*g.svc.updates[3].Description); got != "depuis le pipe" {
		t.Errorf("stdin description = %q", got)
	}
	g.run("edit", "PAY-142", "--description", "")
	if d := g.svc.updates[4].Description; d == nil || len((*d)["content"].([]any)) != 0 {
		t.Errorf("an empty description must clear it: %+v", d)
	}
	// usage
	for _, args := range [][]string{
		{"edit", "PAY-142"},
		{"edit", "PAY-142", "--title", " "},
		{"edit", "PAY-142", "--label", "a", "--add-label", "b"},
		{"edit"},
		{"edit", "PAY-142", "extra", "--title", "x"},
	} {
		if code, _, _ := g.run(args...); code != 2 {
			t.Errorf("%v: code %d, want 2", args, code)
		}
	}
	if code, _, stderr := g.run("edit", "PAY-142", "--priority", "zzz"); code != 1 || !strings.Contains(stderr, "High") {
		t.Errorf("unknown priority: %d %q", code, stderr)
	}
}

func TestEditNeverDestroysAnUneditableDescription(t *testing.T) {
	g := newRig(t)
	// PAY-137's description holds a status lozenge: Markdown cannot keep it
	n := len(g.svc.updates)
	code, _, stderr := g.run("edit", "PAY-137", "--description", "remplacement")
	if code != 1 || !strings.Contains(stderr, "--force") || len(g.svc.updates) != n {
		t.Fatalf("refused: code %d %q updates %d", code, stderr, len(g.svc.updates))
	}
	code, out, errs := g.run("edit", "PAY-137", "--description", "remplacement", "--force")
	mustCode(t, 0, code, out, errs)
	if len(g.svc.updates) != n+1 || jira.ADFToText(*g.svc.updates[n].Description) != "remplacement" {
		t.Errorf("forced update missing")
	}
	// the title of the same ticket needs no force: only the description is at risk
	if code, _, _ := g.run("edit", "PAY-137", "--title", "ok"); code != 0 {
		t.Errorf("title: code %d", code)
	}
}

func TestCreate(t *testing.T) {
	g := newRig(t)
	code, out, errs := g.run("create", "--project", "pay", "--title", "Crash 3DS")
	mustCode(t, 0, code, out, errs)
	in := g.svc.created[0]
	if in.ProjectKey != "PAY" || in.TypeID != "10" || in.Summary != "Crash 3DS" || in.PriorityID != "" || in.AssignToMe || in.ParentKey != "" || in.Description != nil {
		t.Errorf("created %+v", in)
	}
	if got := strings.TrimSpace(out); got != "PAY-147\thttps://example.atlassian.net/browse/PAY-147" {
		t.Errorf("output = %q", got)
	}
	// everything
	g.stdin = "Voir avec @Sofia M. et #PAY-137\n\n- a\n- b"
	code, out, errs = g.run("create", "--project", "PAY", "--type", "bug", "--title", "Complet", "--description", "-",
		"--mention", "sofia", "--priority", "high", "--label", "x", "--label", "y", "--assign", "me", "--output-json")
	mustCode(t, 0, code, out, errs)
	in = g.svc.created[1]
	if in.TypeID != "11" || in.PriorityID != "1" || strings.Join(in.Labels, "|") != "x|y" || !in.AssignToMe {
		t.Errorf("created %+v", in)
	}
	if got := strings.Join(nodes(t, *in.Description), ","); !strings.Contains(got, "mention") || !strings.Contains(got, "inlineCard") {
		t.Errorf("description nodes = %s", got)
	}
	var res map[string]string
	jsonOf(t, out, &res)
	if res["key"] != "PAY-148" || res["summary"] != "Complet" || res["url"] == "" {
		t.Errorf("json = %v", res)
	}
}

func TestCreateValidatesBeforeSending(t *testing.T) {
	g := newRig(t)
	for name, c := range map[string]struct {
		args []string
		code int
		msg  string
	}{
		"no project":          {[]string{"create", "--title", "T"}, 2, "--project"},
		"no title":            {[]string{"create", "--project", "PAY"}, 2, "--title"},
		"extra argument":      {[]string{"create", "x", "--project", "PAY", "--title", "T"}, 2, "inattendu"},
		"bad assign":          {[]string{"create", "--project", "PAY", "--title", "T", "--assign", "bob"}, 2, "--assign"},
		"unknown project":     {[]string{"create", "--project", "NOPE", "--title", "T"}, 1, "NOPE"},
		"unknown type":        {[]string{"create", "--project", "PAY", "--title", "T", "--type", "zzz"}, 1, "Tâche"},
		"sub-task, no parent": {[]string{"create", "--project", "PAY", "--title", "T", "--type", "sous"}, 1, "--parent"},
		"parent, plain type":  {[]string{"create", "--project", "PAY", "--title", "T", "--type", "bug", "--parent", "PAY-1"}, 1, "sous-tâche"},
		"unknown priority":    {[]string{"create", "--project", "PAY", "--title", "T", "--priority", "zzz"}, 1, "High"},
		"required field":      {[]string{"create", "--project", "WEB", "--title", "T"}, 1, "champ obligatoire non géré : Environnement"},
		"unknown mention":     {[]string{"create", "--project", "PAY", "--title", "T", "--description", "x", "--mention", "zzz"}, 1, "aucune personne"},
	} {
		code, out, stderr := g.run(c.args...)
		if code != c.code || !strings.Contains(stderr, c.msg) || out != "" {
			t.Errorf("%s: code %d (want %d), stderr %q, stdout %q", name, code, c.code, stderr, out)
		}
	}
	if len(g.svc.created) != 0 {
		t.Errorf("an invalid request created something: %+v", g.svc.created)
	}
	// a sub-task: the type is found by itself from --parent
	code, out, errs := g.run("create", "--project", "PAY", "--title", "Sous-tâche", "--parent", "pay-142")
	mustCode(t, 0, code, out, errs)
	if in := g.svc.created[0]; in.TypeID != "13" || in.ParentKey != "PAY-142" {
		t.Errorf("created %+v", in)
	}
}

func TestLog(t *testing.T) {
	g := newRig(t)
	code, out, errs := g.run("log", "PAY-142", "1h30", "--description", "revue", "--date", "hier")
	mustCode(t, 0, code, out, errs)
	if len(g.time.logged) != 1 {
		t.Fatalf("logged %+v", g.time.logged)
	}
	l := g.time.logged[0]
	if l.id != "10000" || l.secs != 5400 || l.date != "2026-11-05" || l.desc != "revue" {
		t.Errorf("logged %+v", l)
	}
	if !strings.Contains(out, "PAY-142 : 1 h 30 min ajoutés le 2026-11-05") {
		t.Errorf("output = %q", out)
	}
	// a day is HoursPerDay long; the date defaults to today
	g.cfg.HoursPerDay = 7
	g.run("log", "PAY-142", "1d")
	if l := g.time.logged[1]; l.secs != 7*3600 || l.date != "2026-11-06" {
		t.Errorf("logged %+v", l)
	}
	_, out, _ = g.run("log", "PAY-142", "45m", "--output-json")
	var res map[string]any
	jsonOf(t, out, &res)
	if res["seconds"] != float64(2700) || res["key"] != "PAY-142" {
		t.Errorf("json = %v", res)
	}
	n := len(g.time.logged)
	for _, args := range [][]string{{"log", "PAY-142"}, {"log", "PAY-142", "abc"}, {"log", "PAY-142", "30h"}, {"log", "PAY-142", "1h", "--date", "2099-01-01"}, {"log", "PAY-142", "1h", "--date", "nope"}, {"log", "PAY-142", "1h", "2h"}} {
		if code, _, _ := g.run(args...); code != 2 {
			t.Errorf("%v: code %d, want 2", args, code)
		}
	}
	g.noTempo = true
	if code, _, stderr := g.run("log", "PAY-142", "1h"); code != 1 || !strings.Contains(stderr, "tempo_token") {
		t.Errorf("no Tempo: %d %q", code, stderr)
	}
	if len(g.time.logged) != n {
		t.Error("an invalid request logged time")
	}
}

func TestAuto(t *testing.T) {
	g := newRig(t)
	g.cfg.Automations = []config.Automation{{Name: "Règle déclarée", ID: "decl-1"}}
	code, out, errs := g.run("auto", "PAY-142")
	mustCode(t, 0, code, out, errs)
	for _, want := range []string{"rule-comment\tAjouter un commentaire interne de suivi", "rule-escalate\tEscalader au support N2\tMotif (TEXT), Urgence (1-5) (NUMBER)", "rule-assign\tAssigner à un utilisateur\tUtilisateur (USER)", "decl-1\tRègle déclarée"} {
		if !strings.Contains(out, want) {
			t.Errorf("list misses %q\n%s", want, out)
		}
	}
	// run a rule without input, then one with inputs (the default of a NUMBER applies)
	if code, out, errs := g.run("auto", "PAY-142", "ajouter un commentaire"); code != 0 || len(g.auto.Runs) != 1 || !strings.Contains(out, "lancée sur PAY-142") {
		t.Fatalf("run: %d %v %q %q", code, g.auto.Runs, out, errs)
	}
	code, out, errs = g.run("auto", "PAY-142", "escalader", "--input", "Motif=Client bloqué")
	mustCode(t, 0, code, out, errs)
	r := g.auto.Runs[1]
	if r.RuleID != "rule-escalate" || r.IssueID != "10000" || r.Values["Motif"] != "Client bloqué" || r.Values["Urgence (1-5)"] != "3" {
		t.Errorf("run = %+v", r)
	}
	// by id
	if code, _, _ := g.run("auto", "PAY-142", "rule-comment"); code != 0 || len(g.auto.Runs) != 3 {
		t.Errorf("by id: %d %d", code, len(g.auto.Runs))
	}
	n := len(g.auto.Runs)
	for _, c := range []struct {
		args []string
		code int
		msg  string
	}{
		{[]string{"auto", "PAY-142", "assigner"}, 1, "non gérée"},                                            // USER input: never run
		{[]string{"auto", "PAY-142", "escalader", "--input", "Urgence (1-5)=abc"}, 2, "doit être un nombre"}, // NUMBER validated
		{[]string{"auto", "PAY-142", "escalader", "--input", "Inconnue=1"}, 2, "Motif"},                      // unknown input
		{[]string{"auto", "PAY-142", "escalader", "--input", "sans-egal"}, 2, "nom=valeur"},                  // malformed
		{[]string{"auto", "PAY-142", "zzz"}, 1, "ne correspond à rien"},
		{[]string{"auto", "PAY-142", "a", "b"}, 2, "inattendu"},
	} {
		if code, _, stderr := g.run(c.args...); code != c.code || !strings.Contains(stderr, c.msg) {
			t.Errorf("%v: code %d (want %d) %q", c.args, code, c.code, stderr)
		}
	}
	if len(g.auto.Runs) != n {
		t.Error("an invalid request ran a rule")
	}
	g.noAuto = true
	if code, _, stderr := g.run("auto", "PAY-142", "--list"); code != 1 || !strings.Contains(stderr, "indisponibles") {
		t.Errorf("no automation: %d %q", code, stderr)
	}
}

type failingAuto struct{}

func (failingAuto) List(string) ([]automation.Rule, error) { return nil, errors.New("401 refusé") }
func (failingAuto) Run(automation.Rule, string, map[string]string) error {
	return errors.New("non")
}

func TestAutoFallsBackOnTheConfig(t *testing.T) {
	g := newRig(t)
	var out, errb bytes.Buffer
	build := func(string, bool) (Deps, error) {
		return Deps{Svc: g.svc, Auto: failingAuto{}, Config: config.Config{Automations: []config.Automation{{Name: "Règle déclarée", ID: "decl-1"}}}}, nil
	}
	env := IO{Stdin: strings.NewReader(""), StdinIsTTY: true, Stdout: &out, Stderr: &errb}
	if code := Run([]string{"auto", "PAY-142", "--list"}, env, build); code != 0 || !strings.Contains(out.String(), "decl-1") || !strings.Contains(errb.String(), "401 refusé") {
		t.Errorf("fallback: %d %q %q", code, out.String(), errb.String())
	}
	// no declared rule either: a failure
	out.Reset()
	build2 := func(string, bool) (Deps, error) { return Deps{Svc: g.svc, Auto: failingAuto{}}, nil }
	if code := Run([]string{"auto", "PAY-142", "--list"}, env, build2); code != 1 {
		t.Errorf("no fallback: %d", code)
	}
}

func TestBuilderErrorIsAnError(t *testing.T) {
	var out, errb bytes.Buffer
	env := IO{Stdin: strings.NewReader(""), Stdout: &out, Stderr: &errb}
	code := Run([]string{"list"}, env, func(string, bool) (Deps, error) { return Deps{}, errors.New("configuration incomplète") })
	if code != 1 || !strings.Contains(errb.String(), "configuration incomplète") || out.Len() != 0 {
		t.Errorf("%d %q %q", code, out.String(), errb.String())
	}
	// a usage error never needs the services
	called := false
	Run([]string{"comment"}, env, func(string, bool) (Deps, error) { called = true; return Deps{}, nil })
	if called {
		t.Error("a missing ticket reference must fail before the configuration is read")
	}
}

func TestCompletion(t *testing.T) {
	g := newRig(t)
	for _, shell := range []string{"bash", "zsh", "fish"} {
		code, out, errs := g.run("completion", shell)
		mustCode(t, 0, code, out, errs)
		for _, want := range []string{"list", "comment", "transition", "assign", "create", "internal", "dry-run", "mention", "limit"} {
			if !strings.Contains(out, want) {
				t.Errorf("%s completion misses %q", shell, want)
			}
		}
	}
	for _, args := range [][]string{{"completion"}, {"completion", "powershell"}, {"completion", "bash", "zsh"}} {
		if code, _, _ := g.run(args...); code != 2 {
			t.Errorf("%v: code %d", args, code)
		}
	}
	if _, out, _ := g.run("completion", "zsh"); !strings.HasPrefix(out, "#compdef jira-tui") {
		t.Errorf("zsh script:\n%s", out)
	}
}

func TestEnglish(t *testing.T) {
	i18n.Set(i18n.EN)
	defer i18n.Set(i18n.FR)
	g := newRig(t)
	_, out, _ := g.run("comment", "PAY-142", "x")
	if !strings.Contains(out, "comment added (visible to the customer)") {
		t.Errorf("out = %q", out)
	}
	_, out, _ = g.run("assign", "PAY-142", "me")
	if !strings.Contains(out, "assigned to Kevin") {
		t.Errorf("out = %q", out)
	}
	_, out, _ = g.run("log", "PAY-142", "1h", "--date", "yesterday")
	if !strings.Contains(out, "logged on 2026-11-05") {
		t.Errorf("out = %q", out)
	}
	_, out, _ = g.run("show", "PAY-142")
	for _, want := range []string{"Status:", "Assignee:", "Comments (4)", "(customer)", "(internal)", "Time spent:"} {
		if !strings.Contains(out, want) {
			t.Errorf("EN show misses %q\n%s", want, out)
		}
	}
	for _, french := range []string{"Statut", "Assigné", "Commentaires", "(interne)", "Temps passé", "ajoutés"} {
		if strings.Contains(out, french) {
			t.Errorf("French %q in an English output", french)
		}
	}
	code, _, stderr := g.run("transition", "PAY-142", "zz")
	if code != 1 || !strings.HasPrefix(stderr, "error:") || !strings.Contains(stderr, "matches no possible transition") {
		t.Errorf("EN error: %d %q", code, stderr)
	}
	_, out, _ = g.run("comment", "-h")
	if !strings.Contains(out, "Usage: jira-tui comment") || !strings.Contains(out, "WARNING: visible to the customer by default") {
		t.Errorf("EN help:\n%s", out)
	}
	if code, _, stderr := g.run("comment", "PAY-142"); code != 2 || !strings.Contains(stderr, "the comment text is empty") || !strings.Contains(stderr, "Help: jira-tui comment -h") {
		t.Errorf("EN usage error: %d %q", code, stderr)
	}
}
