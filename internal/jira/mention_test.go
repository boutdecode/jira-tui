package jira

import (
	"math/rand"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const base = "https://acme.atlassian.net"

func conv(mentions map[string]string) Converter {
	return Converter{BrowseURL: func(key string) string { return base + "/browse/" + key }, Mentions: mentions}
}

func people() map[string]string {
	return map[string]string{"Sofia M.": "acc-sofia", "Kevin": "acc-kevin", "Kevin Dupont": "acc-kd", "Léo B.": "acc-leo"}
}

func nodesOf(t *testing.T, doc Doc) []map[string]any {
	t.Helper()
	d := asJSON(t, doc).(map[string]any)
	var out []map[string]any
	for _, b := range d["content"].([]any) {
		for _, n := range b.(map[string]any)["content"].([]any) {
			out = append(out, n.(map[string]any))
		}
	}
	return out
}

func TestMentionIsWrittenAndRead(t *testing.T) {
	c := conv(people())
	doc := c.ToADF("Salut @Sofia M., peux-tu voir ça ? cc @Kevin")
	nodes := nodesOf(t, doc)
	var mentions []string
	for _, n := range nodes {
		if n["type"] == "mention" {
			a := n["attrs"].(map[string]any)
			mentions = append(mentions, a["id"].(string)+"|"+a["text"].(string))
		}
	}
	if strings.Join(mentions, ",") != "acc-sofia|@Sofia M.,acc-kevin|@Kevin" {
		t.Fatalf("mentions = %v\n%s", mentions, debugJSON(doc))
	}
	// and back: same text, same table
	md, table, ok := Converter{BrowseURL: c.BrowseURL}.ToMarkdown(asJSON(t, doc))
	if !ok || md != "Salut @Sofia M., peux-tu voir ça ? cc @Kevin" {
		t.Fatalf("md = %q ok=%v", md, ok)
	}
	if table["Sofia M."] != "acc-sofia" || table["Kevin"] != "acc-kevin" || len(table) != 2 {
		t.Errorf("table = %v", table)
	}
	// ADFToText shows what a reader sees
	if got := ADFToText(asJSON(t, doc)); got != "Salut @Sofia M., peux-tu voir ça ? cc @Kevin" {
		t.Errorf("text = %q", got)
	}
}

func TestMentionNamesAndBoundaries(t *testing.T) {
	c := conv(people())
	kinds := func(md string) string {
		var out []string
		for _, n := range nodesOf(t, c.ToADF(md)) {
			if n["type"] == "mention" {
				out = append(out, n["attrs"].(map[string]any)["id"].(string))
			}
		}
		return strings.Join(out, ",")
	}
	for md, want := range map[string]string{
		"@Kevin Dupont est là":      "acc-kd",    // the longest known name wins
		"@Kevin est là":             "acc-kevin", //
		"@Kevinson n'est pas connu": "",          // not the end of a word
		"écrire à a@Kevin":          "",          // preceded by a letter: an e-mail, not a mention
		"@Inconnu reste du texte":   "",
		"(@Léo B.) et @Sofia M.":    "acc-leo,acc-sofia",
		"@Sofia M.\n@Kevin":         "acc-sofia,acc-kevin",
		`\@Kevin échappé`:           "",
		"`@Kevin` dans du code":     "",
	} {
		if got := kinds(md); got != want {
			t.Errorf("%q: mentions %q, want %q", md, got, want)
		}
	}
}

func TestLiteralAtNameIsProtected(t *testing.T) {
	c := conv(people())
	// plain text that happens to read "@Kevin" must not become a mention when edited
	doc := asJSON(t, TextToADF("je disais @Kevin sans mention, et a@Kevin aussi"))
	md, _, ok := c.ToMarkdown(doc)
	if !ok {
		t.Fatal("should stay editable")
	}
	if !strings.Contains(md, `\@Kevin sans mention`) || !strings.Contains(md, "a@Kevin aussi") {
		t.Errorf("md = %q", md)
	}
	back := c.ToADF(md)
	if ADFToText(asJSON(t, back)) != "je disais @Kevin sans mention, et a@Kevin aussi" {
		t.Errorf("text changed: %q", ADFToText(asJSON(t, back)))
	}
	for _, n := range nodesOf(t, back) {
		if n["type"] == "mention" {
			t.Errorf("a literal text became a mention: %v", n)
		}
	}
}

func TestSameNameForTwoAccountsIsRefused(t *testing.T) {
	doc := `{"type":"doc","content":[{"type":"paragraph","content":[
		{"type":"mention","attrs":{"id":"a1","text":"@Alex"}},{"type":"text","text":" et "},
		{"type":"mention","attrs":{"id":"a2","text":"@Alex"}}]}]}`
	if md, _, ok := conv(nil).ToMarkdown(decode(t, doc)); ok {
		t.Errorf("two people called Alex must not be editable (the text could not tell them apart): %q", md)
	}
	// the same person twice is fine
	same := strings.Replace(doc, `"a2"`, `"a1"`, 1)
	if md, _, ok := conv(nil).ToMarkdown(decode(t, same)); !ok || md != "@Alex et @Alex" {
		t.Errorf("the same account twice: %q %v", md, ok)
	}
	// a mention without id or name cannot be written
	for _, bad := range []string{`{"type":"mention","attrs":{"text":"@X"}}`, `{"type":"mention","attrs":{"id":"1"}}`} {
		if _, _, ok := conv(nil).ToMarkdown(decode(t, `{"type":"doc","content":[{"type":"paragraph","content":[`+bad+`]}]}`)); ok {
			t.Errorf("%s must not be editable", bad)
		}
	}
}

func TestSmartLinks(t *testing.T) {
	c := conv(nil)
	card := func(url string) string {
		return `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"voir "},{"type":"inlineCard","attrs":{"url":"` + url + `"}}]}]}`
	}
	for url, want := range map[string]string{
		base + "/browse/PAY-142":                     "voir #PAY-142",
		base + "/browse/PAY-142?x=1":                 "voir <" + base + "/browse/PAY-142?x=1>",
		"https://www.figma.com/file/ab":              "voir <https://www.figma.com/file/ab>",
		"https://other.atlassian.net/browse/PAY-142": "voir <https://other.atlassian.net/browse/PAY-142>",
	} {
		md, _, ok := c.ToMarkdown(decode(t, card(url)))
		if !ok || md != want {
			t.Errorf("%s:\n got %q (ok=%v)\nwant %q", url, md, ok, want)
			continue
		}
		if back := c.ToADF(md); canon(back) != canon(decode(t, card(url))) {
			t.Errorf("%s: the round trip changed the card\n%s", url, debugJSON(back))
		}
	}
	// typing #KEY makes a smart link to that issue
	n := nodesOf(t, c.ToADF("voir #PAY-142."))
	if len(n) != 3 || n[1]["type"] != "inlineCard" || n[1]["attrs"].(map[string]any)["url"] != base+"/browse/PAY-142" || n[2]["text"] != "." {
		t.Errorf("nodes = %v", n)
	}
	// what is not a reference stays text
	for _, md := range []string{"#123", "a#PAY-1", "#pay-1", "#PAY-", "#PAY-1a", "#PAY-1-2", `\#PAY-1`, "`#PAY-1`", "# Titre"} {
		for _, nn := range nodesOf(t, c.ToADF("x "+md)) {
			if nn["type"] == "inlineCard" {
				t.Errorf("%q must not be a smart link", md)
			}
		}
	}
	// "# Titre" is still a heading, "#PAY-1" at the start of a line is a reference, not a heading
	d := asJSON(t, c.ToADF("# Titre\n\n#PAY-7")).(map[string]any)["content"].([]any)
	if d[0].(map[string]any)["type"] != "heading" || d[1].(map[string]any)["type"] != "paragraph" {
		t.Errorf("blocks = %s", debugJSON(d))
	}
	// without a converter there is no way to know the site: "#PAY-1" stays text
	for _, nn := range nodesOf(t, MarkdownToADF("voir #PAY-1")) {
		if nn["type"] == "inlineCard" {
			t.Error("a smart link needs the site address")
		}
	}
	// literal text that looks like a reference is protected
	md, _, ok := c.ToMarkdown(asJSON(t, TextToADF("le ticket #PAY-9 en clair et <https://x.io> aussi")))
	if !ok || !strings.Contains(md, `\#PAY-9`) || !strings.Contains(md, `\<https://x.io>`) {
		t.Errorf("md = %q ok=%v", md, ok)
	}
}

// Typed text with people and tickets reaches a fixed point, like plain Markdown does.
func TestMentionsAndTicketsReachAFixedPoint(t *testing.T) {
	c := conv(people())
	alphabet := []string{"@Sofia M.", "@Kevin", "@Kevin Dupont", "@Inconnu", "#PAY-1", "#PAY-22", "#12", "<https://x.io/a>", "<", ">", "@", "#", "**", "*", "_", "[", "](", "x)", "`", "\\", "- ", "# ", "1. ", "> ", "\n", "\n\n", " ", " ", "a", "é", ".", ","}
	for seed := int64(1); seed <= 2; seed++ {
		rng := rand.New(rand.NewSource(seed))
		special := 0
		for n := 0; n < 3000; n++ {
			var sb strings.Builder
			for k := rng.Intn(14) + 1; k > 0; k-- {
				sb.WriteString(alphabet[rng.Intn(len(alphabet))])
			}
			typed := sb.String()
			adf := asJSON(t, c.ToADF(typed))
			md1, table, ok := c.ToMarkdown(adf)
			if !ok {
				continue
			}
			c2 := conv(table)
			again := asJSON(t, c2.ToADF(md1))
			if canon(adf) != canon(again) {
				t.Fatalf("seed %d: the document changed when written back\n typed %q\n md1   %q", seed, typed, md1)
			}
			if md2, _, ok2 := c2.ToMarkdown(again); !ok2 || md2 != md1 {
				t.Fatalf("seed %d: not a fixed point\n typed %q\n md1 %q\n md2 %q (ok=%v)", seed, typed, md1, md2, ok2)
			}
			if strings.Contains(debugJSON(adf), `"mention"`) || strings.Contains(debugJSON(adf), `"inlineCard"`) {
				special++
			}
		}
		if special < 300 {
			t.Errorf("seed %d: only %d documents had a mention or a smart link: the test is too weak", seed, special)
		}
	}
}

func TestSearchMentionableAndIssues(t *testing.T) {
	c, rec := serve(t, 200, `[{"accountId":"a1","displayName":"Sofia M.","accountType":"atlassian","active":true},{"accountId":"bot","displayName":"App","accountType":"app"},{"accountId":"x","displayName":"Gone","active":false}]`)
	us, err := c.SearchMentionable("PAY-1", "so")
	if err != nil {
		t.Fatal(err)
	}
	_ = rec // the requests of this call are checked in TestMentionableIncludesServiceDeskCustomers
	if len(us) != 1 || us[0].DisplayName != "Sofia M." {
		t.Errorf("users = %+v", us)
	}

	c2, rec2 := serve(t, 200, `{"sections":[
		{"id":"hs","issues":[{"key":"PAY-1","summaryText":"Premier","summary":"<b>Premier</b>"},{"key":"PAY-2","summary":"Avec <b>HTML</b> &amp; entité"}]},
		{"id":"cs","issues":[{"key":"PAY-1","summaryText":"Doublon"},{"key":"","summaryText":"sans clé"},{"key":"PAY-3","summaryText":"Troisième"}]}]}`)
	is, err := c2.SearchIssues("pay")
	if err != nil {
		t.Fatal(err)
	}
	if rec2.path != "/rest/api/3/issue/picker" || !strings.Contains(rec2.query, "query=pay") {
		t.Errorf("request %s ? %s", rec2.path, rec2.query)
	}
	var got []string
	for _, i := range is {
		got = append(got, i.Key+"="+i.Summary)
	}
	if strings.Join(got, "|") != "PAY-1=Premier|PAY-2=Avec HTML & entité|PAY-3=Troisième" {
		t.Errorf("issues = %v (deduplicated by key, tags stripped)", got)
	}
}

func TestMentionAndCardAreWhatTheClientSends(t *testing.T) {
	c := conv(people())
	cl, rec := serve(t, 200, `{}`)
	if err := cl.AddComment("PAY-1", c.ToADF("@Sofia M. voir #PAY-9"), false); err != nil {
		t.Fatal(err)
	}
	body := asJSON(t, rec.body["body"])
	var types []string
	for _, n := range nodesOf(t, body.(map[string]any)) {
		types = append(types, n["type"].(string))
	}
	if strings.Join(types, ",") != "mention,text,inlineCard" {
		t.Errorf("sent nodes = %v\n%s", types, debugJSON(rec.body))
	}
}

// deskServer answers the user search, the service desk lookup and the customer search.
type deskServer struct {
	hits         map[string]int
	requestCode  int // status of GET /servicedeskapi/request/{key}
	customerCode int // status of GET …/customer
	header       string
	customerQ    string
	userQ        string
}

func newDeskServer(t *testing.T, ds *deskServer) *Client {
	t.Helper()
	ds.hits = map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ds.hits[r.URL.Path]++
		switch {
		case strings.HasSuffix(r.URL.Path, "/user/viewissue/search"):
			ds.userQ = r.URL.RawQuery
			_, _ = w.Write([]byte(`[{"accountId":"u1","displayName":"Sofia M.","accountType":"atlassian","active":true}]`))
		case strings.HasPrefix(r.URL.Path, "/rest/servicedeskapi/request/"):
			if ds.requestCode != 0 && ds.requestCode != 200 {
				w.WriteHeader(ds.requestCode)
				return
			}
			_, _ = w.Write([]byte(`{"issueKey":"SUP-1","serviceDeskId":"7"}`))
		case strings.HasPrefix(r.URL.Path, "/rest/servicedeskapi/servicedesk/7/customer"):
			ds.header, ds.customerQ = r.Header.Get("X-ExperimentalApi"), r.URL.RawQuery
			if ds.customerCode != 0 && ds.customerCode != 200 {
				w.WriteHeader(ds.customerCode)
				return
			}
			_, _ = w.Write([]byte(`{"values":[
				{"accountId":"c1","displayName":"Julie Client","active":true},
				{"accountId":"u1","displayName":"Sofia M.","active":true},
				{"accountId":"c2","displayName":"Parti","active":false},
				{"accountId":"","displayName":"Sans compte"}]}`))
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
		}
	}))
	t.Cleanup(srv.Close)
	return NewClient(srv.URL, "me@x.io", "tok")
}

func names(us []User) string {
	var out []string
	for _, u := range us {
		out = append(out, u.DisplayName+"/"+u.AccountType)
	}
	return strings.Join(out, ",")
}

func TestMentionableIncludesServiceDeskCustomers(t *testing.T) {
	ds := &deskServer{}
	c := newDeskServer(t, ds)
	us, err := c.SearchMentionable("SUP-1", "ju")
	if err != nil {
		t.Fatal(err)
	}
	// people first, then the customers; no duplicate, no inactive account, nobody without an id
	if got := names(us); got != "Sofia M./atlassian,Julie Client/customer" {
		t.Errorf("people = %s", got)
	}
	if us[1].AccountID != "c1" {
		t.Errorf("customer = %+v", us[1])
	}
	if !strings.Contains(ds.userQ, "issueKey=SUP-1") || !strings.Contains(ds.userQ, "query=ju") {
		t.Errorf("user search query = %q", ds.userQ)
	}
	if ds.header != "opt-in" || !strings.Contains(ds.customerQ, "query=ju") {
		t.Errorf("customer request: header=%q query=%q", ds.header, ds.customerQ)
	}
	// the service desk is looked up once per project
	if _, err := c.SearchMentionable("SUP-2", "x"); err != nil {
		t.Fatal(err)
	}
	if ds.hits["/rest/servicedeskapi/request/SUP-1"] != 1 || ds.hits["/rest/servicedeskapi/request/SUP-2"] != 0 {
		t.Errorf("lookups = %v", ds.hits)
	}
}

func TestMentionableOutsideAServiceDesk(t *testing.T) {
	ds := &deskServer{requestCode: 404}
	c := newDeskServer(t, ds)
	for i := 0; i < 2; i++ {
		us, err := c.SearchMentionable("PAY-1", "so")
		if err != nil || names(us) != "Sofia M./atlassian" {
			t.Fatalf("people = %s, err = %v", names(us), err)
		}
	}
	if ds.hits["/rest/servicedeskapi/request/PAY-1"] != 1 || ds.hits["/rest/servicedeskapi/servicedesk/7/customer"] != 0 {
		t.Errorf("a project that is not a service desk is asked about once: %v", ds.hits)
	}
}

func TestMentionableSurvivesCustomerSearchFailures(t *testing.T) {
	// the customer search is forbidden: users are still returned, without an error
	ds := &deskServer{customerCode: 403}
	c := newDeskServer(t, ds)
	us, err := c.SearchMentionable("SUP-1", "")
	if err != nil || names(us) != "Sofia M./atlassian" {
		t.Errorf("403 on customers: %s, %v", names(us), err)
	}
	// a transient error on the lookup is not remembered: the next search tries again
	ds2 := &deskServer{requestCode: 500}
	c2 := newDeskServer(t, ds2)
	if _, err := c2.SearchMentionable("SUP-1", ""); err != nil {
		t.Fatal(err)
	}
	ds2.requestCode = 200
	us, _ = c2.SearchMentionable("SUP-1", "")
	if !strings.Contains(names(us), "Julie Client/customer") {
		t.Errorf("after the outage: %s", names(us))
	}
}
