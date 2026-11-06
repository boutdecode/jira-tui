package i18n

import "testing"

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestDetect(t *testing.T) {
	for _, c := range []struct {
		name string
		env  map[string]string
		want Lang
	}{
		{"french", map[string]string{"LANG": "fr_FR.UTF-8"}, FR},
		{"canadian french", map[string]string{"LANG": "fr_CA.UTF-8"}, FR},
		{"english", map[string]string{"LANG": "en_US.UTF-8"}, EN},
		{"other language", map[string]string{"LANG": "de_DE.UTF-8"}, EN},
		{"nothing set", map[string]string{}, EN},
		{"LC_ALL beats LANG", map[string]string{"LC_ALL": "en_GB.UTF-8", "LANG": "fr_FR.UTF-8"}, EN},
		{"C locale is skipped", map[string]string{"LC_ALL": "C", "LANG": "fr_FR.UTF-8"}, FR},
		{"POSIX locale is skipped", map[string]string{"LC_ALL": "POSIX", "LANG": "fr_FR.UTF-8"}, FR},
		{"C.UTF-8 only", map[string]string{"LANG": "C.UTF-8"}, EN},
		{"LANGUAGE list", map[string]string{"LANGUAGE": "fr:en", "LANG": "en_US.UTF-8"}, FR},
		{"explicit override", map[string]string{"JIRA_TUI_LANG": "en", "LANG": "fr_FR.UTF-8"}, EN},
		{"explicit fr", map[string]string{"JIRA_TUI_LANG": "fr", "LANG": "en_US.UTF-8"}, FR},
		{"fr-CA tag", map[string]string{"LANGUAGE": "fr-CA"}, FR},
	} {
		if got := Detect(env(c.env)); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func TestT(t *testing.T) {
	defer Set(FR)
	Set(FR)
	if T("bonjour", "hello") != "bonjour" || F("%d jour", "%d day", 2) != "2 jour" {
		t.Error("french")
	}
	Set(EN)
	if T("bonjour", "hello") != "hello" || F("%d jour", "%d day", 2) != "2 day" {
		t.Error("english")
	}
}
