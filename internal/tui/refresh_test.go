package tui

import (
	"os"
	"testing"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"github.com/austenstone/gh-copilot-config/internal/profile"
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

// listNames returns the profile names currently held by the list.
func listNames(m model) []string {
	var out []string
	for _, it := range m.list.Items() {
		if pi, ok := it.(profileItem); ok {
			out = append(out, pi.p.Name)
		}
	}
	return out
}

func TestSaveRefreshesList(t *testing.T) {
	dir := t.TempDir()
	os.Setenv("CC_PROFILES", dir)
	mgr, err := profile.Open()
	if err != nil {
		t.Fatal(err)
	}

	m := newModel(mgr, true)
	m = drain(m, m.Init())

	// press 's' to start save input
	var tm tea.Model
	var cmd tea.Cmd
	tm, cmd = m.Update(tea.KeyPressMsg{Code: 's', Text: "s"})
	m = tm.(model)
	m = drain(m, cmd)

	// type a name
	for _, r := range "myprofile" {
		tm, cmd = m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		m = tm.(model)
		m = drain(m, cmd)
	}

	// press enter to submit
	tm, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = tm.(model)
	m = drain(m, cmd)

	names := listNames(m)
	t.Logf("status=%q err=%v names=%v", m.status, m.statusErr, names)

	found := false
	for _, n := range names {
		if n == "myprofile" {
			found = true
		}
	}
	if !found {
		t.Fatalf("myprofile not in list after save; names=%v", names)
	}
}
