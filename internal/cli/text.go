package cli

import (
	"errors"
	"io"
	"os"
	"strings"

	"github.com/kevin/jira-tui/internal/i18n"
)

// multi is a repeatable option (--label a --label b).
type multi []string

func (m *multi) String() string     { return strings.Join(*m, ",") }
func (m *multi) Set(v string) error { *m = append(*m, v); return nil }

// readText returns the text of a command: --file, else the argument ("-" reads standard
// input), else standard input when it is not a terminal. found is false when there was none.
func (r *runner) readText(arg string, hasArg bool, file string) (text string, found bool, err error) {
	switch {
	case file != "":
		b, err := os.ReadFile(file)
		if err != nil {
			return "", false, err
		}
		return string(b), true, nil
	case hasArg && arg != "-":
		return arg, true, nil
	case hasArg || !r.io.StdinIsTTY:
		if r.io.Stdin == nil {
			return "", false, errors.New(i18n.T("entrée standard indisponible", "standard input is not available"))
		}
		b, err := io.ReadAll(r.io.Stdin)
		if err != nil {
			return "", false, err
		}
		return string(b), len(strings.TrimSpace(string(b))) > 0 || hasArg, nil
	}
	return "", false, nil
}
