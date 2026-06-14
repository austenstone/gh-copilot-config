package tui

import (
	"os"
	"testing"

	"github.com/austenstone/gh-copilot-config/internal/profile"
	tea "github.com/charmbracelet/bubbletea"
)

func TestClickInspectsRow(t *testing.T) {
	dir := t.TempDir()
	os.Setenv("CC_PROFILES", dir)
	mgr, err := profile.Open()
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.New("alpha", ""); err != nil {
		t.Fatal(err)
	}

	m := newModel(mgr)
	m = drain(m, m.Init())

	row := -1
	for i, r := range m.tbl.Rows() {
		if len(r) > 1 && r[1] == "alpha" {
			row = i
		}
	}
	if row < 0 {
		t.Fatalf("alpha not in table; rows=%v", m.tbl.Rows())
	}

	click := tea.MouseMsg{Y: listDataTop + row, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}
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
