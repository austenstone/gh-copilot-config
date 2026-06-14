package tui

import (
	"os"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/austenstone/gh-copilot-config/internal/profile"
	zone "github.com/lrstanley/bubblezone/v2"
)

// renderAndLocate renders the model (which scans zone markers) and returns the
// bounds of the named row once the zone worker has recorded them. Scan buffers
// asynchronously, so we poll briefly.
func renderAndLocate(t *testing.T, m model, name string) *zone.ZoneInfo {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		_ = m.View() // triggers zone.Scan over the rendered output
		if z := zone.Get(name); z != nil && !z.IsZero() {
			return z
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("zone %q never resolved", name)
	return nil
}

func newReadyModel(t *testing.T) model {
	t.Helper()
	dir := t.TempDir()
	os.Setenv("CC_PROFILES", dir)
	mgr, err := profile.Open()
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.New("alpha", ""); err != nil {
		t.Fatal(err)
	}

	zone.NewGlobal()
	m := newModel(mgr, true)
	m = drain(m, m.Init())
	tm, cmd := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = tm.(model)
	return drain(m, cmd)
}

func TestClickInspectsRow(t *testing.T) {
	zone.NewGlobal()
	defer zone.Close()
	m := newReadyModel(t)

	z := renderAndLocate(t, m, "alpha")
	click := tea.MouseClickMsg{X: z.StartX, Y: z.StartY, Button: tea.MouseLeft}
	tm, cmd := m.Update(click)
	m = tm.(model)
	m = drain(m, cmd)

	if m.mode != modeDetail {
		t.Fatalf("click did not open detail view: mode=%d status=%q", m.mode, m.status)
	}
	if m.detailName != "alpha" {
		t.Fatalf("inspected wrong profile: %q", m.detailName)
	}
}

func TestHoverHighlightsRow(t *testing.T) {
	zone.NewGlobal()
	defer zone.Close()
	m := newReadyModel(t)

	z := renderAndLocate(t, m, "alpha")
	motion := tea.MouseMotionMsg{X: z.StartX, Y: z.StartY}
	tm, _ := m.Update(motion)
	m = tm.(model)

	if m.delegate.hovered != "alpha" {
		t.Fatalf("hover did not highlight row: hovered=%q", m.delegate.hovered)
	}
}
