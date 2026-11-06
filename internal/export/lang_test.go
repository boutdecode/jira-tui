package export

import (
	"strings"
	"testing"

	"github.com/kevin/jira-tui/internal/i18n"
)

func TestEnglishRefErrors(t *testing.T) {
	i18n.Set(i18n.EN)
	defer i18n.Set(i18n.FR)
	if _, err := ParseRef(""); err == nil || err.Error() != "empty ticket reference" {
		t.Errorf("empty: %v", err)
	}
	if _, err := ParseRef("nope"); err == nil || !strings.Contains(err.Error(), "invalid ticket reference") {
		t.Errorf("invalid: %v", err)
	}
}
