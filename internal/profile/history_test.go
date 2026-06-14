package profile

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDeltaBetween(t *testing.T) {
	older := t.TempDir()
	newer := t.TempDir()

	// instructions: keep one identical, modify one, remove one.
	writeFile(t, filepath.Join(older, "cli/instructions/keep.md"), "same")
	writeFile(t, filepath.Join(newer, "cli/instructions/keep.md"), "same")
	writeFile(t, filepath.Join(older, "cli/instructions/edit.md"), "before")
	writeFile(t, filepath.Join(newer, "cli/instructions/edit.md"), "after")
	writeFile(t, filepath.Join(older, "cli/instructions/gone.md"), "bye")
	// skills: add one.
	writeFile(t, filepath.Join(newer, "cli/skills/fresh/SKILL.md"), "new skill")

	d, err := DeltaBetween(older, newer)
	if err != nil {
		t.Fatal(err)
	}

	got := map[string]CategoryDelta{}
	for _, c := range d.Cats {
		got[c.Category] = c
	}

	instr := got[CatInstructions]
	if instr.Added != 0 || instr.Modified != 1 || instr.Removed != 1 {
		t.Errorf("instructions delta = %+v, want Modified:1 Removed:1", instr)
	}
	skills := got[CatSkills]
	if skills.Added != 1 || skills.Modified != 0 || skills.Removed != 0 {
		t.Errorf("skills delta = %+v, want Added:1", skills)
	}
}

func TestDeltaBetweenNoChange(t *testing.T) {
	older := t.TempDir()
	newer := t.TempDir()
	writeFile(t, filepath.Join(older, "cli/instructions/a.md"), "x")
	writeFile(t, filepath.Join(newer, "cli/instructions/a.md"), "x")

	d, err := DeltaBetween(older, newer)
	if err != nil {
		t.Fatal(err)
	}
	if d.Changed() {
		t.Errorf("expected no changes, got %+v", d.Cats)
	}
}
