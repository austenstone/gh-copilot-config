package main

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func feed(t *testing.T, m model, msg tea.Msg) model {
	t.Helper()
	next, _ := m.Update(msg)
	return next.(model)
}

func kr(r rune) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}} }

func TestTUIFlow(t *testing.T) {
	m := newModel("/tmp/engine")
	m = feed(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = feed(t, m, profilesMsg{profiles: []profile{
		{name: "default", active: true, sizeKB: 1200, created: time.Now(), modified: time.Now()},
		{name: "clean", created: time.Now(), modified: time.Now()},
	}})

	v := m.View()
	if !strings.Contains(v, "PROFILE") || !strings.Contains(v, "default") {
		t.Fatalf("table not rendered:\n%s", v)
	}

	if got := m.selected(); got != "default" {
		t.Fatalf("selected = %q, want default", got)
	}

	m = feed(t, m, kr('s'))
	if m.mode != modeInput {
		t.Fatalf("save should enter input mode, got %v", m.mode)
	}
	m = feed(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.mode != modeList {
		t.Fatalf("esc should return to list, got %v", m.mode)
	}

	m = feed(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.mode != modeConfirm || m.pending != "apply" {
		t.Fatalf("enter should confirm apply, got mode=%v pending=%q", m.mode, m.pending)
	}
	m = feed(t, m, kr('n'))
	if m.mode != modeList {
		t.Fatalf("n should cancel confirm, got %v", m.mode)
	}

	m = feed(t, m, kr('x'))
	if m.mode != modeConfirm || m.pending != "delete" {
		t.Fatalf("x should confirm delete, got mode=%v pending=%q", m.mode, m.pending)
	}
	m = feed(t, m, kr('n'))

	m = feed(t, m, kr('?'))
	if !m.showHelp {
		t.Fatal("? should toggle help")
	}

	m = feed(t, m, actionDoneMsg{title: "test", out: "hello world", err: false})
	if m.mode != modeOutput || !strings.Contains(m.View(), "hello world") {
		t.Fatalf("actionDone with output should show output pane")
	}
}
