package profile

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ---- progress + dry-run ------------------------------------------------

func (m *Manager) logf(format string, a ...any) { fmt.Fprintf(m.Out, format+"\n", a...) }

func (m *Manager) do(desc string, fn func() error) error {
	if m.DryRun {
		fmt.Fprintf(m.Out, "[dry-run] %s\n", desc)
		return nil
	}
	return fn()
}

func (m *Manager) skip(a Asset) bool {
	if m.Surfaces != nil && !m.Surfaces[a.Surface] {
		return true
	}
	if m.Features != nil && !m.Features[a.Feature] {
		return true
	}
	if a.Optional && !m.Optional {
		return true
	}
	if a.Kind == KindHistory && !m.History {
		return true
	}
	if a.Kind == KindDBSnapshot && !m.DBSnapshot {
		return true
	}
	return false
}

// scoped reports whether a surface or feature filter is active.
func (m *Manager) scoped() bool { return m.Surfaces != nil || m.Features != nil }

// ---- high-level commands ------------------------------------------------

// SaveNamed snapshots the live config into profile name. When the profile
// already exists its prior contents are snapshotted first, so an overwrite can
// never lose the old version.
func (m *Manager) SaveNamed(name string) error {
	dir := m.ProfileDir(name)
	existed := isDir(dir)
	if err := m.do("create profile "+name, func() error { return os.MkdirAll(dir, 0o755) }); err != nil {
		return err
	}
	if existed {
		if err := m.snapshotProfileDir(name, "overwrite"); err != nil {
			return err
		}
	}
	if err := m.Save(dir); err != nil {
		return err
	}
	m.logf("saved live config -> profile %q", name)
	return nil
}

// ApplyNamed applies profile name to the live config, auto-snapshotting the
// current live state first and recording the profile as active. Callers are
// responsible for any confirmation.
func (m *Manager) ApplyNamed(name string) error {
	if !m.Exists(name) {
		return fmt.Errorf("no such profile %q", name)
	}
	if err := m.snapshotLive("apply", name); err != nil {
		return err
	}
	if err := m.Apply(m.ProfileDir(name), name == "clean"); err != nil {
		return err
	}
	if err := m.SetActive(name); err != nil {
		return err
	}
	m.logf("applied profile %q", name)
	if m.History {
		m.logf("↳ relaunch the GitHub app / Copilot CLI for history changes to take effect")
	}
	return nil
}

// New creates a profile, empty or copied from base (when base != "").
func (m *Manager) New(name, base string) error {
	if m.Exists(name) {
		return fmt.Errorf("profile %q already exists", name)
	}
	dir := m.ProfileDir(name)
	if base != "" {
		if !m.Exists(base) {
			return fmt.Errorf("base profile %q not found", base)
		}
		if err := m.do(fmt.Sprintf("copy %s -> %s", base, name), func() error { return syncDir(m.ProfileDir(base), dir) }); err != nil {
			return err
		}
		m.logf("created profile %q from %q", name, base)
		return nil
	}
	if err := m.do("create empty profile "+name, func() error {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dir, ".keep"), nil, 0o644)
	}); err != nil {
		return err
	}
	m.logf("created empty profile %q", name)
	return nil
}

// Remove deletes a profile and clears any markers pointing at it.
func (m *Manager) Remove(name string) error {
	if name == "clean" {
		return fmt.Errorf("refusing to delete the built-in 'clean' profile")
	}
	if !m.Exists(name) {
		return fmt.Errorf("no such profile %q", name)
	}
	if err := m.snapshotProfileDir(name, "delete"); err != nil {
		return err
	}
	if err := m.do("delete profile "+name, func() error { return os.RemoveAll(m.ProfileDir(name)) }); err != nil {
		return err
	}
	if !m.DryRun {
		if m.Active() == name {
			_ = os.Remove(filepath.Join(m.Dir, ".active"))
		}
		if m.Last() == name {
			_ = os.Remove(filepath.Join(m.Dir, ".last"))
		}
	}
	m.logf("deleted profile %q", name)
	return nil
}

// Diff returns a unified diff of live config against a profile; "" means no drift.
func (m *Manager) Diff(name string) (string, error) {
	if !m.Exists(name) {
		return "", fmt.Errorf("no such profile %q", name)
	}
	tmp, err := os.MkdirTemp("", "cc-diff-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)

	// db-snapshot/history never byte-match a fresh snapshot (and the db copy is
	// huge and slow), so drop them from the snapshot entirely rather than copy
	// then ignore. Optional assets are absent from live without --all.
	excludes := []string{".keep"}
	snap := *m
	snap.DryRun = false
	snap.Out = io.Discard
	snap.Assets = snap.Assets[:0:0]
	for _, a := range m.Assets {
		if a.Kind == KindDBSnapshot || a.Kind == KindHistory || (a.Optional && !m.Optional) {
			excludes = append(excludes, filepath.Base(a.Rel))
			continue
		}
		snap.Assets = append(snap.Assets, a)
	}
	if err := snap.Save(tmp); err != nil {
		return "", err
	}

	// When scoped, compare against only the scoped slice of the saved profile so
	// unrelated surfaces don't show up as spurious deletions. Mirror the same
	// assets out of the profile into a temp tree and diff that instead.
	profileRoot := m.ProfileDir(name)
	if m.scoped() {
		scopedRoot, err := os.MkdirTemp("", "cc-diff-prof-")
		if err != nil {
			return "", err
		}
		defer os.RemoveAll(scopedRoot)
		for _, a := range m.Assets {
			if m.skip(a) {
				continue
			}
			src := filepath.Join(profileRoot, a.Rel)
			dst := filepath.Join(scopedRoot, a.Rel)
			switch {
			case isDir(src):
				if err := syncDir(src, dst); err != nil {
					return "", err
				}
			case exists(src):
				if err := copyFile(src, dst); err != nil {
					return "", err
				}
			}
		}
		profileRoot = scopedRoot
	}

	args := []string{"-ruN"}
	for _, e := range excludes {
		args = append(args, "--exclude", e)
	}
	args = append(args, profileRoot, tmp)
	out, _ := exec.Command("diff", args...).CombinedOutput()
	text := strings.ReplaceAll(string(out), profileRoot, "<profile:"+name+">")
	text = strings.ReplaceAll(text, m.ProfileDir(name), "<profile:"+name+">")
	text = strings.ReplaceAll(text, tmp, "<live>")
	return strings.TrimSpace(text), nil
}

// Snapshot captures the current live config into the timeline on demand,
// tagged "manual" against the active profile. It is deduped, so capturing twice
// with no change in between writes a single snapshot.
func (m *Manager) Snapshot() error {
	return m.snapshotLive("manual", m.Active())
}

// snapshotLive captures the current live config into the autosave timeline,
// tagged with what triggered it. It always includes optional assets, never
// history, and is skipped when nothing changed since the newest snapshot.
func (m *Manager) snapshotLive(trigger, target string) error {
	return m.recordSnapshot(trigger, target, func(dir string) error {
		snap := *m
		snap.Optional = true
		snap.History = false
		snap.Surfaces = nil // a safety snapshot is always full, never scoped
		snap.Features = nil
		return snap.Save(dir)
	})
}

// snapshotProfileDir captures a profile's current on-disk contents into the
// timeline before it is overwritten or deleted, so nothing is ever lost.
func (m *Manager) snapshotProfileDir(name, trigger string) error {
	src := m.ProfileDir(name)
	if !isDir(src) {
		return nil
	}
	return m.recordSnapshot(trigger, name, func(dir string) error { return syncDir(src, dir) })
}

// recordSnapshot writes a new _autosave/<ts> entry via fill, stamps it with
// provenance, and drops it when it's identical to the newest existing snapshot
// so aggressive autosaving doesn't pile up duplicates.
func (m *Manager) recordSnapshot(trigger, target string, fill func(dir string) error) error {
	ts := time.Now().Format(snapshotTimeFormat)
	rel := filepath.Join("_autosave", ts)
	dir := filepath.Join(m.Dir, rel)
	if m.DryRun {
		m.logf("[dry-run] snapshot %s -> %s", trigger, rel)
		return nil
	}
	prev, _ := m.Snapshots()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := fill(dir); err != nil {
		return err
	}
	if len(prev) > 0 {
		if d, err := DeltaBetween(prev[0].Dir, dir); err == nil && !d.Changed() {
			_ = os.RemoveAll(dir)
			return nil
		}
	}
	if err := writeSnapMeta(dir, trigger, target); err != nil {
		return err
	}
	m.logf("↳ safety snapshot: profiles/%s (%s)", rel, trigger)
	return nil
}

// ---- save: live -> profile ----------------------------------------------

// Save writes the live config into a profile directory.
func (m *Manager) Save(dir string) error {
	for _, a := range m.Assets {
		if m.skip(a) {
			continue
		}
		if err := m.saveAsset(a, dir); err != nil {
			return fmt.Errorf("save %s: %w", a.Name, err)
		}
	}
	return nil
}

func (m *Manager) saveAsset(a Asset, dir string) error {
	prof := filepath.Join(dir, a.Rel)
	switch a.Kind {
	case KindFile:
		if exists(a.Live) {
			return m.do("save "+a.Name, func() error { return copyFile(a.Live, prof) })
		}
		return m.do("drop "+a.Name+" (absent)", func() error { return remove(prof) })
	case KindDir:
		if isDir(a.Live) {
			return m.do("save "+a.Name+"/", func() error { return syncDir(a.Live, prof) })
		}
		return m.do("drop "+a.Name+"/ (absent)", func() error { return os.RemoveAll(prof) })
	case KindJSONKeys:
		if isFile(a.Live) {
			return m.do("extract "+a.Name+" keys", func() error { return extractToProfile(a.Live, prof, a.KeyRE) })
		}
		return m.do("drop "+a.Name+" (absent)", func() error { return remove(prof) })
	case KindDBSnapshot:
		if exists(a.Live) {
			return m.do("snapshot "+a.Name, func() error { return dbSnapshot(a.Live, prof) })
		}
		return m.do("drop "+a.Name+" (absent)", func() error { return remove(prof) })
	case KindHistory:
		switch {
		case isDir(a.Live):
			return m.do("backup "+a.Name+"/", func() error { return syncDir(a.Live, prof) })
		case isFile(a.Live):
			return m.do("backup "+a.Name, func() error { return dbSnapshot(a.Live, prof) })
		}
		return m.do("drop "+a.Name+" (absent)", func() error { return os.RemoveAll(prof) })
	}
	return fmt.Errorf("unknown kind %q", a.Kind)
}

// ---- apply: profile -> live ---------------------------------------------

// Apply writes a profile directory onto the live locations. clean signals the
// empty "clean" profile, which also clears live session history.
func (m *Manager) Apply(dir string, clean bool) error {
	for _, a := range m.Assets {
		if m.skip(a) {
			continue
		}
		if err := m.applyAsset(a, dir, clean); err != nil {
			return fmt.Errorf("apply %s: %w", a.Name, err)
		}
	}
	return nil
}

func (m *Manager) applyAsset(a Asset, dir string, clean bool) error {
	prof := filepath.Join(dir, a.Rel)
	switch a.Kind {
	case KindFile:
		switch {
		case exists(prof):
			return m.do("write "+a.Name, func() error { return copyFile(prof, a.Live) })
		case exists(a.Live) && !a.Optional:
			return m.do("remove "+a.Name, func() error { return remove(a.Live) })
		}
		return nil
	case KindDir:
		switch {
		case isDir(prof):
			return m.do("write "+a.Name+"/", func() error { return syncDir(prof, a.Live) })
		case isDir(a.Live) && !a.Optional:
			return m.do("remove "+a.Name+"/", func() error { return os.RemoveAll(a.Live) })
		}
		return nil
	case KindJSONKeys:
		switch {
		case isFile(prof):
			return m.do("merge "+a.Name+" keys", func() error { return applyKeysFile(a.Live, prof, a.KeyRE) })
		case isFile(a.Live):
			return m.do("strip "+a.Name+" keys", func() error { return applyKeysFile(a.Live, "", a.KeyRE) })
		}
		return nil
	case KindDBSnapshot:
		return m.do("skip "+a.Name+" (backup-only)", func() error { return nil })
	case KindHistory:
		switch {
		case isDir(prof):
			return m.do("restore "+a.Name+"/", func() error { return syncDir(prof, a.Live) })
		case isFile(prof):
			return m.do("restore "+a.Name, func() error { return restoreDB(prof, a.Live) })
		case clean:
			return m.do("clear "+a.Name, func() error { return removeHistory(a.Live) })
		}
		return nil
	}
	return fmt.Errorf("unknown kind %q", a.Kind)
}

// ---- filesystem helpers -------------------------------------------------

func exists(p string) bool { _, err := os.Stat(p); return err == nil }
func isDir(p string) bool  { fi, err := os.Stat(p); return err == nil && fi.IsDir() }
func isFile(p string) bool { fi, err := os.Stat(p); return err == nil && fi.Mode().IsRegular() }

func remove(p string) error {
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// copyFile copies a regular file, preserving permissions and mtime, writing
// atomically via a temp file.
func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	fi, err := in.Stat()
	if err != nil {
		return err
	}
	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, fi.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	_ = os.Chtimes(tmp, fi.ModTime(), fi.ModTime())
	return os.Rename(tmp, dst)
}

// syncDir mirrors src into dst (creating, overwriting, and pruning) while
// ignoring VCS and OS noise, equivalent to `rsync -a --delete`.
func syncDir(src, dst string) error {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	if err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if rel == "." {
			return nil
		}
		if ignored(rel) {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return copyFile(p, target)
	}); err != nil {
		return err
	}
	// Prune entries in dst that no longer exist in src.
	return filepath.WalkDir(dst, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dst, p)
		if rel == "." || ignored(rel) {
			if d.IsDir() && rel != "." {
				return fs.SkipDir
			}
			return nil
		}
		if !exists(filepath.Join(src, rel)) {
			if err := os.RemoveAll(p); err != nil {
				return err
			}
			if d.IsDir() {
				return fs.SkipDir
			}
		}
		return nil
	})
}

func ignored(rel string) bool {
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		if part == ".git" || part == ".DS_Store" || part == "node_modules" {
			return true
		}
	}
	return false
}

// dbSnapshot writes a consistent single-file copy of a live SQLite DB. It uses
// `sqlite3 VACUUM INTO` when available (safe while the DB is open) and falls
// back to a plain copy otherwise.
func dbSnapshot(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if _, err := exec.LookPath("sqlite3"); err == nil {
		for _, s := range []string{dst, dst + "-wal", dst + "-shm"} {
			_ = os.Remove(s)
		}
		out, err := exec.Command("sqlite3", src, fmt.Sprintf("VACUUM INTO '%s'", dst)).CombinedOutput()
		if err != nil {
			return fmt.Errorf("sqlite3 VACUUM INTO: %v: %s", err, strings.TrimSpace(string(out)))
		}
		return nil
	}
	return copyFile(src, dst)
}

// restoreDB writes a DB snapshot back to its live path, clearing stale sidecars.
func restoreDB(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	for _, s := range []string{dst + "-wal", dst + "-shm"} {
		_ = os.Remove(s)
	}
	return copyFile(src, dst)
}

// removeHistory deletes a live history store so the app recreates an empty one.
func removeHistory(live string) error {
	if isDir(live) {
		return os.RemoveAll(live)
	}
	for _, s := range []string{live, live + "-wal", live + "-shm"} {
		if err := remove(s); err != nil {
			return err
		}
	}
	return nil
}
