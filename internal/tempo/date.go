package tempo

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/kevin/jira-tui/internal/i18n"
)

// ParseDate reads "", "aujourd'hui", "hier", "2026-11-04", "04/11/2026" or "04/11"
// and returns YYYY-MM-DD. Future dates are refused.
func ParseDate(s string, now time.Time) (string, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	var d time.Time
	switch s {
	case "", "aujourd'hui", "aujourdhui", "auj", "today":
		d = today
	case "hier", "yesterday":
		d = today.AddDate(0, 0, -1)
	default:
		var err error
		for _, layout := range []string{"2006-01-02", "02/01/2006", "2/1/2006"} {
			if d, err = time.ParseInLocation(layout, s, now.Location()); err == nil {
				break
			}
		}
		if err != nil {
			t, e := time.ParseInLocation("02/01", s, now.Location())
			if e != nil {
				return "", fmt.Errorf(i18n.T("date illisible : %q (ex. hier, 04/11, 2026-11-04)", "unreadable date: %q (e.g. yesterday, 04/11, 2026-11-04)"), s)
			}
			d = time.Date(now.Year(), t.Month(), t.Day(), 0, 0, 0, 0, now.Location())
		}
	}
	if d.After(today) {
		return "", errors.New(i18n.T("la date ne peut pas être dans le futur", "the date cannot be in the future"))
	}
	return d.Format("2006-01-02"), nil
}
