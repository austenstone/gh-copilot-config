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

// SaveNamed snapshots the live config into profile name.
func (m *Manager) SaveNamed(name string) error {
	dir := m.ProfileDir(name)
	if err := m.do("create profile "+name, func() error { return os.MkdirAll(dir, 0o755) }); err != nil {
		return err
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
	if err := m.autosnapshot(); err != nil {
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

// diffRoots builds the two trees to compare: a fresh snapshot of live config
// (liveRoot) and the saved profile (profileRoot), honoring the active scope. It
// returns the roots, the file basenames to exclude, and a cleanup func.
func (m *Manager) diffRoots(name string) (profileRoot, liveRoot string, excludes []string, cleanup func(), err error) {
	if !m.Exists(name) {
		return "", "", nil, func() {}, fmt.Errorf("no such profile %q", name)
	}
	var cleanups []string
	cleanup = func() {
		for _, d := range cleanups {
			os.RemoveAll(d)
		}
	}

	tmp, err := os.MkdirTemp("", "cc-diff-")
	if err != nil {
		return "", "", nil, cleanup, err
	}
	cleanups = append(cleanups, tmp)

	// db-snapshot/history never byte-match a fresh snapshot (and the db copy is
	// huge and slow), so drop them from the snapshot entirely rather than copy
	// then ignore. Optional assets are absent from live without --all.
	excludes = []string{".keep"}
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
		return "", "", nil, cleanup, err
	}

	// When scoped, compare against only the scoped slice of the saved profile so
	// unrelated surfaces don't show up as spurious deletions. Mirror the same
	// assets out of the profile into a temp tree and diff that instead.
	profileRoot = m.ProfileDir(name)
	if m.scoped() {
		scopedRoot, err := os.MkdirTemp("", "cc-diff-prof-")
		if err != nil {
			return "", "", nil, cleanup, err
		}
		cleanups = append(cleanups, scopedRoot)
		for _, a := range m.Assets {
			if m.skip(a) {
				continue
			}
			src := filepath.Join(profileRoot, a.Rel)
			dst := filepath.Join(scopedRoot, a.Rel)
			switch {
			case isDir(src):
				if err := syncDir(src, dst); err != nil {
					return "", "", nil, cleanup, err
				}
			case exists(src):
				if err := copyFile(src, dst); err != nil {
					return "", "", nil, cleanup, err
				}
			}
		}
		profileRoot = scopedRoot
	}
	return profileRoot, tmp, excludes, cleanup, nil
}

// Diff returns a unified diff of live config against a profile; "" means no drift.
func (m *Manager) Diff(name string) (string, error) {
	profileRoot, liveRoot, excludes, cleanup, err := m.diffRoots(name)
	defer cleanup()
	if err != nil {
		return "", err
	}

	args := []string{"-ruN"}
	for _, e := range excludes {
		args = append(args, "--exclude", e)
	}
	args = append(args, profileRoot, liveRoot)
	out, _ := exec.Command("diff", args...).CombinedOutput()
	text := strings.ReplaceAll(string(out), profileRoot, "<profile:"+name+">")
	text = strings.ReplaceAll(text, m.ProfileDir(name), "<profile:"+name+">")
	text = strings.ReplaceAll(text, liveRoot, "<live>")
	return strings.TrimSpace(text), nil
}

// DiffChange is one entry in a diff summary: a path that was added, removed, or
// modified in live config relative to the saved profile.
type DiffChange struct {
	Path string // path relative to the surface root
	Kind string // "added" (only in live), "removed" (only in profile), "modified"
}

// DiffSummary lists which paths drifted without emitting full file contents. It
// runs `diff -qr` (quiet, no -N) so single-sided entries stay classified as
// added/removed instead of collapsing into modified. Cheap relative to Diff.
func (m *Manager) DiffSummary(name string) ([]DiffChange, error) {
	profileRoot, liveRoot, excludes, cleanup, err := m.diffRoots(name)
	defer cleanup()
	if err != nil {
		return nil, err
	}

	args := []string{"-qr"}
	for _, e := range excludes {
		args = append(args, "--exclude", e)
	}
	args = append(args, profileRoot, liveRoot)
	out, _ := exec.Command("diff", args...).CombinedOutput()

	var changes []DiffChange
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		switch {
		case strings.HasPrefix(line, "Files ") && strings.HasSuffix(line, " differ"):
			// "Files <profileRoot>/x and <liveRoot>/x differ"
			mid := strings.TrimSuffix(strings.TrimPrefix(line, "Files "), " differ")
			parts := strings.SplitN(mid, " and ", 2)
			rel := strings.TrimPrefix(strings.TrimPrefix(parts[len(parts)-1], liveRoot), "/")
			changes = append(changes, DiffChange{Path: rel, Kind: "modified"})
		case strings.HasPrefix(line, "Only in "):
			// "Only in <dir>: <entry>"
			rest := strings.TrimPrefix(line, "Only in ")
			idx := strings.LastIndex(rest, ": ")
			if idx < 0 {
				continue
			}
			dir, entry := rest[:idx], rest[idx+2:]
			joined := filepath.Join(dir, entry)
			kind := "removed"
			if strings.HasPrefix(joined, liveRoot) {
				kind = "added"
			}
			full := strings.TrimPrefix(joined, profileRoot)
			full = strings.TrimPrefix(full, liveRoot)
			full = strings.TrimPrefix(full, "/")
			changes = append(changes, DiffChange{Path: full, Kind: kind})
		}
	}
	return changes, nil
}

// autosnapshot copies the current live state into _autosave/<timestamp> before a
// destructive apply. It always includes optional assets and never history.
func (m *Manager) autosnapshot() error {
	ts := time.Now().Format("20060102-150405")
	dir := filepath.Join(m.Dir, "_autosave", ts)
	snap := *m
	snap.Optional = true
	snap.History = false
	snap.Surfaces = nil // a safety snapshot is always full, never scoped
	snap.Features = nil
	if err := snap.do(fmt.Sprintf("autosnapshot live -> _autosave/%s", ts), func() error { return os.MkdirAll(dir, 0o755) }); err != nil {
		return err
	}
	if err := snap.Save(dir); err != nil {
		return err
	}
	if !m.DryRun {
		m.logf("↳ safety snapshot: profiles/_autosave/%s", ts)
	}
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
