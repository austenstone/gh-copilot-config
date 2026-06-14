package tui

import (
	"os"
	"testing"

	"github.com/austenstone/copilot-config/internal/profile"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
)

func drain(m model, cmd tea.Cmd) model {
	for cmd != nil {
		msg := cmd()
		if batch, ok := msg.(tea.BatchMsg); ok {
			cmd = nil
			for _, c := range batch {
				if c == nil {
					continue
				}
				// spinner ticks loop forever; skip them in the harness
				if _, isTick := c().(spinner.TickMsg); isTick {
					continue
				}
				m = drain(m, c)
			}
			continue
		}
		if _, isTick := msg.(spinner.TickMsg); isTick {
			return m
		}
		var next tea.Model
		next, cmd = m.Update(msg)
		m = next.(model)
	}
	return m
}

func TestSaveRefreshesList(t *testing.T) {
	dir := t.TempDir()
	os.Setenv("CC_PROFILES", dir)
	mgr, err := profile.Open()
	if err != nil {
		t.Fatal(err)
	}

	m := newModel(mgr)
	m = drain(m, m.Init())

	// press 's' to start save input
	var tm tea.Model
	var cmd tea.Cmd
	tm, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	m = tm.(model)
	m = drain(m, cmd)

	// type a name
	for _, r := range "myprofile" {
		tm, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = tm.(model)
		m = drain(m, cmd)
	}

	// press enter to submit
	tm, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = tm.(model)
	m = drain(m, cmd)

	t.Logf("status=%q err=%v rows=%d", m.status, m.statusErr, len(m.tbl.Rows()))
	for _, r := range m.tbl.Rows() {
		t.Logf("row: %v", r)
	}

	found := false
	for _, r := range m.tbl.Rows() {
		if len(r) > 1 && r[1] == "myprofile" {
			found = true
		}
	}
	if !found {
		t.Fatalf("myprofile not in table after save")
	}
}
