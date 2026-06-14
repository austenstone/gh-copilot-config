package profile

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// snapshotTimeFormat is the layout autosnapshot dirs are stamped with.
const snapshotTimeFormat = "20060102-150405"

// snapMetaFile records what triggered a snapshot and what it targeted.
const snapMetaFile = ".meta"

// Snapshot is one point-in-time capture of config. Destructive writes
// auto-snapshot into profiles/_autosave/<ts> first, so the set of snapshots
// forms a browsable, restorable timeline of how config evolved.
type Snapshot struct {
	ID      string    // directory name, e.g. "20060102-150405"
	Taken   time.Time // parsed from the directory name
	Dir     string    // absolute path to the snapshot tree
	Trigger string    // what caused it: apply, overwrite, delete, restore
	Target  string    // the profile involved, when any
}

// Snapshots returns the autosave timeline newest-first.
func (m *Manager) Snapshots() ([]Snapshot, error) {
	root := filepath.Join(m.Dir, "_autosave")
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var snaps []Snapshot
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		ts, err := time.ParseInLocation(snapshotTimeFormat, e.Name(), time.Local)
		if err != nil {
			continue
		}
		dir := filepath.Join(root, e.Name())
		trigger, target := readSnapMeta(dir)
		snaps = append(snaps, Snapshot{ID: e.Name(), Taken: ts, Dir: dir, Trigger: trigger, Target: target})
	}
	sort.Slice(snaps, func(i, j int) bool { return snaps[i].Taken.After(snaps[j].Taken) })
	return snaps, nil
}

// writeSnapMeta stamps a snapshot dir with what triggered it and what it
// targeted, so the timeline reads as a story and can be filtered per profile.
func writeSnapMeta(dir, trigger, target string) error {
	var b strings.Builder
	if trigger != "" {
		fmt.Fprintf(&b, "trigger=%s\n", trigger)
	}
	if target != "" {
		fmt.Fprintf(&b, "target=%s\n", target)
	}
	return os.WriteFile(filepath.Join(dir, snapMetaFile), []byte(b.String()), 0o644)
}

// readSnapMeta reads provenance back; missing or legacy snapshots return empty.
func readSnapMeta(dir string) (trigger, target string) {
	b, err := os.ReadFile(filepath.Join(dir, snapMetaFile))
	if err != nil {
		return "", ""
	}
	for _, line := range strings.Split(string(b), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch strings.TrimSpace(k) {
		case "trigger":
			trigger = strings.TrimSpace(v)
		case "target":
			target = strings.TrimSpace(v)
		}
	}
	return trigger, target
}

// Restore brings a snapshot's captured config back to the live locations. It
// auto-snapshots the current live state first, so a restore is itself
// reversible and the history stays indestructible.
func (m *Manager) Restore(snap Snapshot) error {
	if !isDir(snap.Dir) {
		return fmt.Errorf("snapshot %q not found", snap.ID)
	}
	if err := m.snapshotLive("restore", snap.ID); err != nil {
		return err
	}
	r := *m
	r.Optional = true
	r.History = false
	r.Surfaces = nil
	r.Features = nil
	if err := r.Apply(snap.Dir, false); err != nil {
		return err
	}
	m.logf("restored snapshot %s -> live", snap.ID)
	return nil
}

// InspectDir classifies any snapshot or profile directory into an Inventory,
// so a snapshot reads with the same surface→feature shape as a saved profile.
func (m *Manager) InspectDir(dir string) (Inventory, error) { return inventoryOf(dir) }

// CategoryDelta counts how one customization category changed between two
// revisions of config.
type CategoryDelta struct {
	Category                 string
	Added, Removed, Modified int
}

// Delta summarizes per-category changes from an older revision to a newer one,
// holding only the categories that actually changed, in canonical order.
type Delta struct {
	Cats []CategoryDelta
}

// Changed reports whether any category changed between the two revisions.
func (d Delta) Changed() bool { return len(d.Cats) > 0 }

// DeltaBetween summarizes what changed in each customization category from the
// older directory to the newer one. Items are matched by surface+name; a match
// whose underlying file content differs counts as a modification.
func DeltaBetween(olderDir, newerDir string) (Delta, error) {
	older, err := inventoryOf(olderDir)
	if err != nil {
		return Delta{}, err
	}
	newer, err := inventoryOf(newerDir)
	if err != nil {
		return Delta{}, err
	}
	o, n := flattenByCategory(older), flattenByCategory(newer)
	var d Delta
	for _, cat := range Categories {
		om, nm := o[cat], n[cat]
		cd := CategoryDelta{Category: cat}
		for key, np := range nm {
			op, ok := om[key]
			switch {
			case !ok:
				cd.Added++
			case !sameContent(op, np):
				cd.Modified++
			}
		}
		for key := range om {
			if _, ok := nm[key]; !ok {
				cd.Removed++
			}
		}
		if cd.Added+cd.Removed+cd.Modified > 0 {
			d.Cats = append(d.Cats, cd)
		}
	}
	return d, nil
}

// flattenByCategory collapses an inventory to category → (surface+name → path),
// keying by surface and name so identically named items on different surfaces
// stay distinct.
func flattenByCategory(inv Inventory) map[string]map[string]string {
	out := map[string]map[string]string{}
	for s, cats := range inv.Items {
		for cat, items := range cats {
			m := out[cat]
			if m == nil {
				m = map[string]string{}
				out[cat] = m
			}
			for _, it := range items {
				m[string(s)+"\x00"+it.Name] = it.Path
			}
		}
	}
	return out
}

// sameContent reports whether two files are byte-identical. Directory-backed
// items (extensions, plugins) have no single file to compare, so they're
// treated as unchanged rather than spuriously flagged.
func sameContent(a, b string) bool {
	ai, ae := os.Stat(a)
	bi, be := os.Stat(b)
	if ae != nil || be != nil {
		return ae != nil && be != nil
	}
	if ai.IsDir() || bi.IsDir() {
		return true
	}
	if ai.Size() != bi.Size() {
		return false
	}
	ab, err := os.ReadFile(a)
	if err != nil {
		return false
	}
	bb, err := os.ReadFile(b)
	if err != nil {
		return false
	}
	return bytes.Equal(ab, bb)
}

// DiffSnapshots returns a unified diff between two snapshot trees (older →
// newer), with the temp paths normalized back to their snapshot IDs.
func (m *Manager) DiffSnapshots(older, newer Snapshot) (string, error) {
	args := []string{"-ruN", "--exclude", ".keep", older.Dir, newer.Dir}
	out, _ := exec.Command("diff", args...).CombinedOutput()
	text := string(out)
	text = strings.ReplaceAll(text, older.Dir, "a/"+older.ID)
	text = strings.ReplaceAll(text, newer.Dir, "b/"+newer.ID)
	return strings.TrimSpace(text), nil
}
