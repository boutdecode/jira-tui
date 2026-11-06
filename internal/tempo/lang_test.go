package tempo

import (
	"strings"
	"testing"
	"time"

	"github.com/kevin/jira-tui/internal/i18n"
)

func TestEnglishErrors(t *testing.T) {
	i18n.Set(i18n.EN)
	defer i18n.Set(i18n.FR)
	now := time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)

	for _, c := range []struct{ in, want string }{
		{"", "empty duration"},
		{"abc", "unreadable duration"},
		{"0m", "must be positive"},
		{"25h", "24 h maximum per entry"},
	} {
		if _, err := ParseDuration(c.in, 8); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("ParseDuration(%q) = %v, want %q", c.in, err, c.want)
		}
	}
	if _, err := ParseDate("nope", now); err == nil || !strings.Contains(err.Error(), "unreadable date") {
		t.Errorf("unreadable date: %v", err)
	}
	if _, err := ParseDate("2026-11-07", now); err == nil || !strings.Contains(err.Error(), "cannot be in the future") {
		t.Errorf("future date: %v", err)
	}
	// both languages are accepted as input whatever the interface language
	for _, s := range []string{"hier", "yesterday", "aujourd'hui", "today", ""} {
		if _, err := ParseDate(s, now); err != nil {
			t.Errorf("ParseDate(%q): %v", s, err)
		}
	}
}
