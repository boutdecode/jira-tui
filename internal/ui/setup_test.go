package ui

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/kevin/jira-tui/internal/config"
)

func setupKey(t *testing.T, s Setup, k tea.Key) Setup {
	t.Helper()
	next, cmd := s.Update(tea.KeyPressMsg(k))
	s = next.(Setup)
	return settleSetup(t, s, cmd)
}

func settleSetup(t *testing.T, s Setup, cmd tea.Cmd) Setup {
	t.Helper()
	for i := 0; cmd != nil && i < 5; i++ {
		msg := runCmd(cmd)
		if msg == nil {
			return s
		}
		if _, ok := msg.(tea.QuitMsg); ok {
			return s
		}
		next, c := s.Update(msg)
		s, cmd = next.(Setup), c
	}
	return s
}

func typeSetup(t *testing.T, s Setup, text string) Setup {
	for _, r := range text {
		s = setupKey(t, s, tea.Key{Code: r, Text: string(r)})
	}
	return s
}

func fill(t *testing.T, s Setup, url, email, token string) Setup {
	s = typeSetup(t, s, url)
	s = setupKey(t, s, tea.Key{Code: tea.KeyEnter})
	s = typeSetup(t, s, email)
	s = setupKey(t, s, tea.Key{Code: tea.KeyEnter})
	return typeSetup(t, s, token)
}

func TestSetupSuccess(t *testing.T) {
	var got [3]string
	check := func(u, e, tok string) (string, error) { got = [3]string{u, e, tok}; return "Kevin", nil }
	s := NewSetup(config.Config{JQL: config.DefaultJQL}, "/tmp/c.yaml", check)
	s = fill(t, s, "mon-site.atlassian.net/", "a@b.fr", "tok123")
	if !strings.Contains(ansi.Strip(s.View().Content), "Première configuration") {
		t.Error("title missing")
	}
	s = setupKey(t, s, tea.Key{Code: tea.KeyEnter})
	if s.Result == nil {
		t.Fatalf("no result, err = %q", s.errMsg)
	}
	if got != [3]string{"https://mon-site.atlassian.net", "a@b.fr", "tok123"} {
		t.Fatalf("check got %v", got)
	}
	if s.Result.Token != "tok123" || s.Result.JQL != config.DefaultJQL {
		t.Fatalf("result %+v", s.Result)
	}
}

func TestSetupErrorsAndCancel(t *testing.T) {
	s := NewSetup(config.Config{}, "/tmp/c.yaml", func(string, string, string) (string, error) { return "", errors.New("authentification refusée (401)") })
	// empty fields: nothing sent
	for i := 0; i < 3; i++ {
		s = setupKey(t, s, tea.Key{Code: tea.KeyEnter})
	}
	if s.Result != nil || !strings.Contains(s.errMsg, "obligatoires") {
		t.Fatalf("errMsg = %q", s.errMsg)
	}
	s = fill(t, s, "x.atlassian.net", "a@b.fr", "bad")
	s = setupKey(t, s, tea.Key{Code: tea.KeyEnter})
	if s.Result != nil || !strings.Contains(s.errMsg, "401") {
		t.Fatalf("errMsg = %q", s.errMsg)
	}
	if strings.Contains(ansi.Strip(s.View().Content), "bad") {
		t.Error("token must be masked")
	}
	if s = setupKey(t, s, tea.Key{Code: tea.KeyEscape}); !s.Cancelled || s.Result != nil {
		t.Error("esc should cancel")
	}
}

func TestSetupPrefilled(t *testing.T) {
	s := NewSetup(config.Config{URL: "https://x", Email: "a@b"}, "p", nil)
	if s.focus != 2 {
		t.Fatalf("should start on the first empty field, got %d", s.focus)
	}
}
