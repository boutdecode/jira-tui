package jira

import "strings"

// ProjectKey infers the project key (the part before the last "-") from issue keys.
func ProjectKey(issues []Issue) string {
	for _, is := range issues {
		if i := strings.LastIndex(is.Key, "-"); i > 0 {
			return is.Key[:i]
		}
	}
	return ""
}
