package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/kevin/jira-tui/internal/mock"
)

func demoWithTime(t *testing.T) Model {
	t.Helper()
	m := demo(t, 130, 36).WithTime(mock.NewTime(), 8)
	return press(t, m, "enter") // open PAY-142
}

func TestTimeSectionAndTotals(t *testing.T) {
	m := demoWithTime(t)
	out := view(m)
	for _, want := range []string{
		"Temps passé (5 h 15 min sur 8 h estimées)", // 3h + 1h30 + 45m, estimate 8h
		"Camille R.", "Reproduction du bug en local", "Estimation",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("detail misses %q", want)
		}
	}
	if len(m.worklogs) != 3 || !m.wlLoaded {
		t.Fatalf("worklogs = %d loaded=%v", len(m.worklogs), m.wlLoaded)
	}
}

func TestAddTime(t *testing.T) {
	m := demoWithTime(t)
	m = press(t, m, "t")
	if m.mode != modeLog || !strings.Contains(view(m), "Ajouter du temps à PAY-142") {
		t.Fatalf("mode = %v", m.mode)
	}
	m = typeText(t, m, "1h30")
	if !strings.Contains(view(m), "= 1 h 30 min le "+time.Now().Format("02/01/2006")) {
		t.Error("live preview missing")
	}
	m = press(t, m, "enter") // → date (empty = today)
	m = press(t, m, "enter") // → description
	m = typeText(t, m, "revue")
	m = press(t, m, "enter") // submit
	if m.mode != modeNormal {
		t.Fatalf("form should close, mode = %v err = %q", m.mode, m.logErr)
	}
	if len(m.worklogs) != 4 {
		t.Fatalf("worklogs = %d, want 4", len(m.worklogs))
	}
	if w := m.worklogs[0]; w.Seconds != 5400 || w.Description != "revue" || w.Date != time.Now().Format("2006-01-02") {
		t.Fatalf("new worklog = %+v", w)
	}
	if !strings.Contains(m.status, "1 h 30 min ajoutés à PAY-142") {
		t.Errorf("status = %q", m.status)
	}
	if !strings.Contains(view(m), "Temps passé (6 h 45 min sur 8 h estimées)") {
		t.Error("total not refreshed")
	}
}

func TestAddTimeInvalidInput(t *testing.T) {
	m := demoWithTime(t)
	m = press(t, m, "t")
	m = typeText(t, m, "abc")
	for i := 0; i < 3; i++ {
		m = press(t, m, "enter")
	}
	if m.mode != modeLog || !strings.Contains(view(m), "durée illisible") || m.logFocus != 0 {
		t.Fatalf("invalid duration must keep the form open: mode=%v err=%q focus=%d", m.mode, m.logErr, m.logFocus)
	}
	// fix the duration, then use a future date
	for range "abc" {
		m = press(t, m, "backspace")
	}
	m = typeText(t, m, "1h")
	m = press(t, m, "enter")
	m = typeText(t, m, "2999-01-01")
	m = press(t, m, "enter")
	m = press(t, m, "enter")
	if m.mode != modeLog || !strings.Contains(m.logErr, "futur") || m.logFocus != 1 {
		t.Fatalf("future date must be refused: mode=%v err=%q focus=%d", m.mode, m.logErr, m.logFocus)
	}
	if len(m.worklogs) != 3 {
		t.Error("nothing must be sent on invalid input")
	}
	if m = press(t, m, "esc"); m.mode != modeNormal || len(m.worklogs) != 3 {
		t.Error("esc should cancel")
	}
}

func TestTimeWithoutTempo(t *testing.T) {
	m := press(t, demo(t, 130, 36), "enter") // no WithTime
	if !strings.Contains(view(m), "Tempo non configuré") {
		t.Error("detail should explain how to enable Tempo")
	}
	m = press(t, m, "t")
	if m.mode != modeNormal || !m.isErr || !strings.Contains(m.status, "tempo_token") {
		t.Fatalf("mode=%v status=%q", m.mode, m.status)
	}
}
