// Package i18n picks the interface language (French or English) from the system locale.
//
// Strings live next to the code as a (French, English) pair: T("bonjour", "hello").
package i18n

import (
	"fmt"
	"strings"
	"sync/atomic"
)

type Lang int32

const (
	FR Lang = iota
	EN
)

// The strings of the source were written in French, so French is the zero value
// (and what tests expect). main calls Set(Detect(os.Getenv)) at startup.
var cur atomic.Int32

func Set(l Lang)    { cur.Store(int32(l)) }
func Current() Lang { return Lang(cur.Load()) }

// Detect returns FR when the system locale is French and EN otherwise. It reads, in
// order of priority, JIRA_TUI_LANG, LC_ALL, LC_MESSAGES, LANGUAGE and LANG. The first
// non-empty one that is not "C"/"POSIX" decides.
func Detect(getenv func(string) string) Lang {
	for _, k := range []string{"JIRA_TUI_LANG", "LC_ALL", "LC_MESSAGES", "LANGUAGE", "LANG"} {
		v := strings.ToLower(strings.TrimSpace(getenv(k)))
		if v == "" {
			continue
		}
		// LANGUAGE is a colon-separated preference list: the first entry wins.
		v, _, _ = strings.Cut(v, ":")
		if v == "" || v == "c" || v == "posix" || strings.HasPrefix(v, "c.") {
			continue
		}
		if v == "fr" || strings.HasPrefix(v, "fr_") || strings.HasPrefix(v, "fr-") || strings.HasPrefix(v, "fr.") {
			return FR
		}
		return EN
	}
	return EN
}

// T returns the French or English text according to the current language.
func T(fr, en string) string {
	if Current() == EN {
		return en
	}
	return fr
}

// F is T followed by fmt.Sprintf.
func F(fr, en string, args ...any) string { return fmt.Sprintf(T(fr, en), args...) }
