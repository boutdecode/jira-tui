package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnvOverridesFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.yaml")
	_ = os.WriteFile(p, []byte("url: https://file.example\nemail: f@x\ntoken: ftok\nfilters:\n  - {name: Mes bugs, jql: \"type = Bug\"}\n"), 0o600)
	t.Setenv("JIRA_URL", "")
	t.Setenv("JIRA_EMAIL", "env@x")
	t.Setenv("JIRA_TOKEN", "")

	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.URL != "https://file.example" || c.Email != "env@x" || c.Token != "ftok" {
		t.Fatalf("got %+v", c)
	}
	if c.JQL != DefaultJQL || len(c.Filters) != 1 {
		t.Fatalf("defaults/filters: %+v", c)
	}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestMissingFileAndValidate(t *testing.T) {
	t.Setenv("JIRA_URL", "")
	t.Setenv("JIRA_EMAIL", "")
	t.Setenv("JIRA_TOKEN", "")
	c, err := Load(filepath.Join(t.TempDir(), "nope.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if c.Validate() == nil {
		t.Fatal("expected validation error")
	}
}

func TestSaveRoundTrip(t *testing.T) {
	t.Setenv("JIRA_URL", "")
	t.Setenv("JIRA_EMAIL", "")
	t.Setenv("JIRA_TOKEN", "")
	p := filepath.Join(t.TempDir(), "sub", "c.yaml")
	in := Config{URL: "https://x.atlassian.net", Email: "a@b.c", Token: "secret", JQL: DefaultJQL,
		Filters: []Filter{{Name: "Bugs", JQL: "type = Bug"}}}
	if err := Save(p, in); err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(p)
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v", fi.Mode().Perm())
	}
	if d, _ := os.Stat(filepath.Dir(p)); d.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode = %v", d.Mode().Perm())
	}
	raw, _ := os.ReadFile(p)
	if strings.Contains(string(raw), "default_jql") {
		t.Error("default JQL must not be written")
	}
	out, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if out.URL != in.URL || out.Token != "secret" || len(out.Filters) != 1 || out.JQL != DefaultJQL {
		t.Fatalf("round trip: %+v", out)
	}
}

func TestTempoConfig(t *testing.T) {
	t.Setenv("JIRA_URL", "")
	t.Setenv("JIRA_EMAIL", "")
	t.Setenv("JIRA_TOKEN", "")
	t.Setenv("TEMPO_URL", "")
	t.Setenv("TEMPO_TOKEN", "")
	p := filepath.Join(t.TempDir(), "c.yaml")
	_ = os.WriteFile(p, []byte("url: u\nemail: e\ntoken: t\ntempo_token: ftok\nhours_per_day: 7\n"), 0o600)

	c, _ := Load(p)
	if c.TempoToken != "ftok" || c.HoursPerDay != 7 || c.TempoURL != DefaultTempoURL {
		t.Fatalf("file: %+v", c)
	}
	t.Setenv("TEMPO_TOKEN", "envtok")
	t.Setenv("TEMPO_URL", "https://api.eu.tempo.io/4")
	if c, _ = Load(p); c.TempoToken != "envtok" || c.TempoURL != "https://api.eu.tempo.io/4" {
		t.Fatalf("env: %+v", c)
	}
	// defaults when nothing is set
	t.Setenv("TEMPO_TOKEN", "")
	t.Setenv("TEMPO_URL", "")
	c, _ = Load(filepath.Join(t.TempDir(), "none.yaml"))
	if c.HoursPerDay != DefaultHoursPerDay || c.TempoToken != "" {
		t.Fatalf("defaults: %+v", c)
	}
	// LoadFile ignores the environment (so a setup never persists env secrets)
	t.Setenv("TEMPO_TOKEN", "envtok")
	if f, _ := LoadFile(p); f.TempoToken != "ftok" {
		t.Fatalf("LoadFile read the env: %+v", f)
	}
	// defaults are not written back
	if err := Save(p, c); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(p)
	if strings.Contains(string(raw), "tempo_url") || strings.Contains(string(raw), "hours_per_day") {
		t.Errorf("defaults persisted:\n%s", raw)
	}
}

func TestAutomationConfig(t *testing.T) {
	for _, k := range []string{"JIRA_URL", "JIRA_EMAIL", "JIRA_TOKEN"} {
		t.Setenv(k, "")
	}
	p := filepath.Join(t.TempDir(), "c.yaml")
	_ = os.WriteFile(p, []byte("url: u\nemail: e\ntoken: t\ncloud_id: abc\nautomations:\n  - {name: Escalader, id: r-1}\n  - {name: Autre, id: r-2}\n"), 0o600)
	c, err := Load(p)
	if err != nil || c.CloudID != "abc" || len(c.Automations) != 2 || c.Automations[0].ID != "r-1" || c.Automations[1].Name != "Autre" {
		t.Fatalf("%v %+v", err, c)
	}
	if err := Save(p, c); err != nil {
		t.Fatal(err)
	}
	back, _ := Load(p)
	if back.CloudID != "abc" || len(back.Automations) != 2 {
		t.Errorf("Save lost the automation settings: %+v", back)
	}
}
