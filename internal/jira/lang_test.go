package jira

import (
	"strings"
	"testing"
	"time"

	"github.com/kevin/jira-tui/internal/i18n"
)

func TestEnglishHTTPErrors(t *testing.T) {
	i18n.Set(i18n.EN)
	defer i18n.Set(i18n.FR)
	for _, c := range []struct {
		err  Error
		want string
	}{
		{Error{Status: 401}, "authentication refused (401): check JIRA_EMAIL / JIRA_TOKEN"},
		{Error{Status: 403}, "access forbidden (403)"},
		{Error{Status: 429, RetryAfter: 2 * time.Second}, "too many requests (429), retry in 2s"},
		{Error{Status: 429}, "giving up after several attempts"},
	} {
		if got := c.err.Error(); !strings.Contains(got, c.want) {
			t.Errorf("%d: %q does not contain %q", c.err.Status, got, c.want)
		}
	}
}
