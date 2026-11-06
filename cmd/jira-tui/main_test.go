package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestParseArgs(t *testing.T) {
	ok := []struct {
		args []string
		want options
	}{
		{nil, options{}},
		{[]string{"--demo"}, options{demo: true}},
		{[]string{"PAY-142"}, options{ref: "PAY-142"}},
		{[]string{"pay-142", "--output-json"}, options{ref: "PAY-142", outputJSON: true}},
		{[]string{"--output-json", "PAY-142"}, options{ref: "PAY-142", outputJSON: true}},
		{[]string{"--demo", "PAY-142", "--output-json", "--config", "/tmp/c.yaml"}, options{demo: true, ref: "PAY-142", outputJSON: true, config: "/tmp/c.yaml"}},
		{[]string{"https://x.atlassian.net/browse/PAY-7", "--transparent"}, options{ref: "PAY-7", transparent: true}},
	}
	for _, tc := range ok {
		if got, err := parseArgs(tc.args); err != nil || got != tc.want {
			t.Errorf("parseArgs(%v) = %+v, %v; want %+v", tc.args, got, err, tc.want)
		}
	}
	for _, args := range [][]string{{"--output-json"}, {"PAY-1", "PAY-2"}, {"--nope"}, {"truc"}, {"PAY-142", "--nope"}} {
		if got, err := parseArgs(args); err == nil {
			t.Errorf("parseArgs(%v) = %+v, want an error", args, got)
		}
	}
}

func runCLI(args ...string) (code int, stdout, stderr string) {
	var out, errb bytes.Buffer
	code = run(args, &out, &errb)
	return code, out.String(), errb.String()
}

func TestRunJSON(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	for _, args := range [][]string{{"--demo", "PAY-142", "--output-json"}, {"--output-json", "--demo", "pay-142"}} {
		code, out, errs := runCLI(args...)
		if code != 0 || errs != "" {
			t.Fatalf("%v: code=%d stderr=%q", args, code, errs)
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(out), &m); err != nil {
			t.Fatalf("stdout is not pure JSON: %v\n%s", err, out)
		}
		if m["key"] != "PAY-142" || m["status"].(map[string]any)["name"] != "En cours" {
			t.Errorf("unexpected JSON: %v", m["key"])
		}
		if strings.Contains(out, "\x1b") {
			t.Error("no escape sequences in the JSON output")
		}
	}
}

func TestRunExitCodes(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	for _, k := range []string{"JIRA_URL", "JIRA_EMAIL", "JIRA_TOKEN", "TEMPO_TOKEN"} {
		t.Setenv(k, "")
	}
	cases := []struct {
		name string
		args []string
		code int
		err  string
	}{
		{"unknown ticket", []string{"--demo", "ZZZ-1", "--output-json"}, 1, "ZZZ-1"},
		{"no reference", []string{"--output-json"}, 2, "exige une référence"},
		{"bad reference", []string{"--output-json", "nope"}, 2, "invalide"},
		{"unknown flag", []string{"--nope"}, 2, "nope"},
		{"no config, no form", []string{"PAY-1", "--output-json"}, 1, "configuration incomplète"},
	}
	for _, tc := range cases {
		code, out, errs := runCLI(tc.args...)
		if code != tc.code || !strings.Contains(errs, tc.err) {
			t.Errorf("%s: code=%d (want %d) stderr=%q", tc.name, code, tc.code, errs)
		}
		if out != "" {
			t.Errorf("%s: stdout must stay empty on failure, got %q", tc.name, out)
		}
	}
	if code, out, _ := runCLI("-h"); code != 0 || !strings.Contains(out, "[TICKET|URL]") || !strings.Contains(out, "-output-json") {
		t.Errorf("-h: code=%d out=%q", code, out)
	}
}
