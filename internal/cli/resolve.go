package cli

import (
	"fmt"
	"strings"

	"github.com/kevin/jira-tui/internal/i18n"
	"github.com/kevin/jira-tui/internal/jira"
)

// pick returns the index of the item that query designates: an exact match (ignoring case)
// first, then a prefix that matches one item only. Anything else is an error that lists the
// choices, so a script never gets a silently wrong pick.
func pick(query string, items []string) (int, error) {
	q := strings.ToLower(strings.TrimSpace(query))
	var exact, prefix []int
	for i, it := range items {
		l := strings.ToLower(it)
		switch {
		case l == q:
			exact = append(exact, i)
		case q != "" && strings.HasPrefix(l, q):
			prefix = append(prefix, i)
		}
	}
	for _, found := range [][]int{exact, prefix} {
		switch len(found) {
		case 1:
			return found[0], nil
		case 0:
		default:
			var names []string
			for _, i := range found {
				names = append(names, items[i])
			}
			return -1, fmt.Errorf(i18n.T("« %s » est ambigu (%s)", "“%s” is ambiguous (%s)"), query, strings.Join(names, ", "))
		}
	}
	return -1, fmt.Errorf(i18n.T("« %s » ne correspond à rien (choix : %s)", "“%s” matches nothing (choices: %s)"), query, strings.Join(items, ", "))
}

// pickUser chooses among the users found for query: the account id, an exact name, or the
// only result.
func pickUser(query string, us []jira.User) (jira.User, error) {
	if len(us) == 0 {
		return jira.User{}, fmt.Errorf(i18n.T("aucune personne ne correspond à « %s »", "nobody matches “%s”"), query)
	}
	for _, u := range us {
		if u.AccountID == query {
			return u, nil
		}
	}
	names := make([]string, len(us))
	for i, u := range us {
		names[i] = u.DisplayName
	}
	if len(us) == 1 {
		return us[0], nil
	}
	i, err := pick(query, names)
	if err != nil {
		return jira.User{}, fmt.Errorf(i18n.T("« %s » est ambigu (%s)", "“%s” is ambiguous (%s)"), query, strings.Join(names, ", "))
	}
	return us[i], nil
}

// mentionTable resolves --mention options into the name -> account id table the Markdown
// converter turns into real mentions. find searches the people that can be mentioned.
func mentionTable(names []string, find func(query string) ([]jira.User, error)) (map[string]string, error) {
	table := map[string]string{}
	for _, n := range names {
		us, err := find(n)
		if err != nil {
			return nil, err
		}
		u, err := pickUser(n, us)
		if err != nil {
			return nil, err
		}
		if old, ok := table[u.DisplayName]; ok && old != u.AccountID {
			return nil, fmt.Errorf(i18n.T("deux personnes s'appellent « %s » : mention impossible", "two people are called “%s”: cannot mention"), u.DisplayName)
		}
		table[u.DisplayName] = u.AccountID
	}
	return table, nil
}
