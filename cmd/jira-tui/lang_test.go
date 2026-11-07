package main

import (
	"strings"
	"testing"

	"github.com/kevin/jira-tui/internal/i18n"
)

func TestEnglishCLI(t *testing.T) {
	i18n.Set(i18n.EN)
	defer i18n.Set(i18n.FR)

	code, stdout, _ := runCLI("-h")
	if code != 0 || !strings.Contains(stdout, "Usage: jira-tui") || !strings.Contains(stdout, "Options:") ||
		!strings.Contains(stdout, "fake data, no network") || !strings.Contains(stdout, "Exit codes:") {
		t.Errorf("usage (%d):\n%s", code, stdout)
	}
	if strings.Contains(stdout, "Options :") || strings.Contains(stdout, "factices") {
		t.Errorf("French in the English usage:\n%s", stdout)
	}

	code, _, stderr := runCLI("--output-json")
	if code != 2 || !strings.Contains(stderr, "error: --output-json requires a ticket reference") || !strings.Contains(stderr, "Help: jira-tui -h") {
		t.Errorf("missing ref (%d): %s", code, stderr)
	}
	code, _, stderr = runCLI("not-a-ticket")
	if code != 2 || !strings.Contains(stderr, "invalid ticket reference") {
		t.Errorf("bad ref (%d): %s", code, stderr)
	}
	code, _, stderr = runCLI("PAY-1", "PAY-2")
	if code != 2 || !strings.Contains(stderr, "a single ticket reference is expected") {
		t.Errorf("two refs (%d): %s", code, stderr)
	}
	// the demo ticket is exported in English too
	code, stdout, _ = runCLI("--demo", "PAY-142", "--output-json")
	if code != 0 || !strings.Contains(stdout, `"summary": "Payment fails with a 3DS2 card"`) || !strings.Contains(stdout, `"name": "In Progress"`) {
		t.Errorf("json (%d):\n%s", code, stdout)
	}
	code, _, stderr = runCLI("--demo", "PAY-999", "--output-json")
	if code != 1 || !strings.Contains(stderr, "ticket PAY-999 not found") {
		t.Errorf("unknown (%d): %s", code, stderr)
	}
}

func TestVersion(t *testing.T) {
	code, stdout, stderr := runCLI("--version")
	if code != 0 || strings.TrimSpace(stdout) != "jira-tui "+version || stderr != "" {
		t.Errorf("--version = %d %q %q", code, stdout, stderr)
	}
	// no configuration needed, and no reference required
	if code, _, _ := runCLI("--version", "--output-json"); code != 0 {
		t.Errorf("--version with --output-json = %d", code)
	}
}

func TestCommandsAreDispatched(t *testing.T) {
	// a command word is not a ticket reference
	code, stdout, stderr := runCLI("list", "--demo", "--limit", "2", "--output-json")
	if code != 0 || !strings.Contains(stdout, `"key": "PAY-142"`) || stderr != "" {
		t.Fatalf("list: %d\n%s\n%s", code, stdout, stderr)
	}
	code, stdout, stderr = runCLI("whoami", "--demo")
	if code != 0 || !strings.HasPrefix(stdout, "Kevin\t") {
		t.Errorf("whoami: %d %q %q", code, stdout, stderr)
	}
	code, stdout, _ = runCLI("transition", "PAY-142", "--demo", "--dry-run", "En revue")
	if code != 0 || !strings.Contains(stdout, `"action": "transition"`) {
		t.Errorf("dry-run: %d %q", code, stdout)
	}
	if code, _, stderr := runCLI("comment", "--demo"); code != 2 || !strings.Contains(stderr, "référence de ticket") {
		t.Errorf("usage: %d %q", code, stderr)
	}
	// the ways of calling the program before the commands existed are untouched
	for _, args := range [][]string{{"PAY-142", "--demo", "--output-json"}, {"--demo", "pay-142", "--output-json"}} {
		if code, stdout, _ := runCLI(args...); code != 0 || !strings.Contains(stdout, `"key": "PAY-142"`) {
			t.Errorf("%v: %d %q", args, code, stdout)
		}
	}
	if code, _, _ := runCLI("not-a-command"); code != 2 {
		t.Errorf("an unknown word is still a usage error, got %d", code)
	}
	// the main help lists the commands
	_, stdout, _ = runCLI("-h")
	for _, want := range []string{"list", "comment", "transition", "assign", "create", "log", "auto", "completion"} {
		if !strings.Contains(stdout, "  "+want) {
			t.Errorf("-h misses the command %q:\n%s", want, stdout)
		}
	}
}
