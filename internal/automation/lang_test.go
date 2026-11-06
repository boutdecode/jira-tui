package automation

import (
	"errors"
	"strings"
	"testing"

	"github.com/kevin/jira-tui/internal/i18n"
)

func TestEnglishErrors(t *testing.T) {
	i18n.Set(i18n.EN)
	defer i18n.Set(i18n.FR)
	if got := ErrNotApplicable.Error(); got != "rule not applicable to this ticket" {
		t.Errorf("ErrNotApplicable = %q", got)
	}
	if !errors.Is(ErrNotApplicable, ErrNotApplicable) {
		t.Error("errors.Is must still match")
	}
	err := CheckValue(Input{Name: "Urgency", Type: "NUMBER"}, "abc")
	if err == nil || !strings.Contains(err.Error(), "must be a number") {
		t.Errorf("CheckValue = %v", err)
	}
	i18n.Set(i18n.FR)
	if got := ErrNotApplicable.Error(); got != "règle non applicable à ce ticket" {
		t.Errorf("the message must follow the language, got %q", got)
	}
}
