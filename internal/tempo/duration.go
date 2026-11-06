package tempo

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/kevin/jira-tui/internal/i18n"
)

var durTok = regexp.MustCompile(`^\s*(\d+(?:[.,]\d+)?)\s*([a-zA-Z]*)\s*`)

// ParseDuration reads "1h30", "1h 30m", "90m", "1.5h", "45" (minutes), "2d", "1d 2h".
// A day is hoursPerDay hours; a bare number is minutes (or hours right after "d", minutes after "h").
func ParseDuration(s string, hoursPerDay int) (int, error) {
	if hoursPerDay <= 0 {
		hoursPerDay = 8
	}
	rest := strings.ToLower(strings.TrimSpace(s))
	if rest == "" {
		return 0, errors.New(i18n.T("durée vide (ex. 1h30, 45m, 2d)", "empty duration (e.g. 1h30, 45m, 2d)"))
	}
	total, prev := 0.0, ""
	for rest != "" {
		m := durTok.FindStringSubmatch(rest)
		if m == nil {
			return 0, fmt.Errorf(i18n.T("durée illisible : %q (ex. 1h30, 45m, 2d)", "unreadable duration: %q (e.g. 1h30, 45m, 2d)"), s)
		}
		rest = rest[len(m[0]):]
		v, _ := strconv.ParseFloat(strings.Replace(m[1], ",", ".", 1), 64)
		unit := m[2]
		if unit == "" { // bare number: follows the previous unit
			switch prev {
			case "d":
				unit = "h"
			default:
				unit = "m"
			}
		}
		switch unit {
		case "m", "min", "mn":
			total += v * 60
			prev = "m"
		case "h":
			total += v * 3600
			prev = "h"
		case "d", "j":
			total += v * float64(hoursPerDay) * 3600
			prev = "d"
		default:
			return 0, fmt.Errorf(i18n.T("unité inconnue %q (h, m, d)", "unknown unit %q (h, m, d)"), unit)
		}
	}
	secs := int(math.Round(total))
	switch {
	case secs <= 0:
		return 0, errors.New(i18n.T("la durée doit être positive", "the duration must be positive"))
	case secs > 24*3600:
		return 0, errors.New(i18n.T("24 h maximum par saisie", "24 h maximum per entry"))
	}
	return secs, nil
}

// FormatDuration renders seconds as "19 h 15 min" (hours, never days: a day's length varies).
func FormatDuration(secs int) string {
	h, m := secs/3600, (secs%3600)/60
	switch {
	case h > 0 && m > 0:
		return fmt.Sprintf("%d h %d min", h, m)
	case h > 0:
		return fmt.Sprintf("%d h", h)
	case m > 0:
		return fmt.Sprintf("%d min", m)
	}
	return "0 min"
}
