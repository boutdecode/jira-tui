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
