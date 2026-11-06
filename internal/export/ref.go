// Package export turns a Jira reference into a stable JSON document.
package export

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/kevin/jira-tui/internal/i18n"
)

var (
	keyOnly   = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]+-\d+$`)
	browseKey = regexp.MustCompile(`/browse/([A-Za-z][A-Za-z0-9_]+-\d+)(?:[/?#]|$)`)
	anyKey    = regexp.MustCompile(`(?:^|[^A-Za-z0-9_])([A-Za-z][A-Za-z0-9_]+-\d+)(?:[^A-Za-z0-9_]|$)`)
)

// ParseRef accepts a key ("PAY-142", "pay-142") or a Jira URL
// (".../browse/PAY-142", ".../boards/1?selectedIssue=PAY-142") and returns the upper-cased key.
func ParseRef(arg string) (string, error) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return "", errors.New(i18n.T("référence de ticket vide", "empty ticket reference"))
	}
	if keyOnly.MatchString(arg) {
		return strings.ToUpper(arg), nil
	}
	if strings.Contains(arg, "://") {
		u, err := url.Parse(arg)
		if err == nil {
			if sel := u.Query().Get("selectedIssue"); keyOnly.MatchString(sel) {
				return strings.ToUpper(sel), nil
			}
			if m := browseKey.FindStringSubmatch(u.Path); m != nil {
				return strings.ToUpper(m[1]), nil
			}
			if m := anyKey.FindStringSubmatch(u.Path); m != nil {
				return strings.ToUpper(m[1]), nil
			}
		}
	}
	return "", fmt.Errorf(i18n.T("référence de ticket invalide : %q (attendu : PAY-142 ou l'URL du ticket)", "invalid ticket reference: %q (expected: PAY-142 or the ticket URL)"), arg)
}
