package profile

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Manager performs all profile operations against a profiles directory.
type Manager struct {
	Dir        string    // profiles store
	Assets     []Asset   // managed manifest
	Out        io.Writer // human-readable progress output
	DryRun     bool      // print planned changes, touch nothing
	Optional   bool      // include assets flagged optional (--all)
	History    bool      // include session history (--with-history)
	DBSnapshot bool      // include backup-only DB snapshots (--with-db)
}

// Open resolves the profiles directory, ensures the built-in empty "clean"
// profile exists, and loads the manifest.
func Open() (*Manager, error) {
	dir := storeDir()
	clean := filepath.Join(dir, "clean")
	if err := os.MkdirAll(clean, 0o755); err != nil {
		return nil, err
	}
	keep := filepath.Join(clean, ".keep")
	if !exists(keep) {
		if err := os.WriteFile(keep, nil, 0o644); err != nil {
			return nil, err
		}
	}
	return &Manager{Dir: dir, Assets: Manifest(), Out: io.Discard}, nil
}

func storeDir() string {
	if d := os.Getenv("CC_PROFILES"); d != "" {
		return d
	}
	cfg := os.Getenv("XDG_CONFIG_HOME")
	if cfg == "" {
		home, _ := os.UserHomeDir()
		cfg = filepath.Join(home, ".config")
	}
	return filepath.Join(cfg, "gh-copilot-config", "profiles")
}

// Profile is the metadata shown in listings.
type Profile struct {
	Name              string
	Created, Modified time.Time
	Size              int64
	Active, Last      bool
}

func (m *Manager) ProfileDir(name string) string { return filepath.Join(m.Dir, name) }

func (m *Manager) Exists(name string) bool { return isDir(m.ProfileDir(name)) }

func (m *Manager) Active() string { return m.marker(".active") }
func (m *Manager) Last() string   { return m.marker(".last") }

func (m *Manager) marker(name string) string {
	b, err := os.ReadFile(filepath.Join(m.Dir, name))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// SetActive records name as the active profile (and last non-clean profile).
func (m *Manager) SetActive(name string) error {
	if m.DryRun {
		return nil
	}
	if err := os.WriteFile(filepath.Join(m.Dir, ".active"), []byte(name+"\n"), 0o644); err != nil {
		return err
	}
	if name != "clean" {
		return os.WriteFile(filepath.Join(m.Dir, ".last"), []byte(name+"\n"), 0o644)
	}
	return nil
}

// Profiles returns every profile sorted by sortKey (created|modified|name).
func (m *Manager) Profiles(sortKey string, reverse bool) ([]Profile, error) {
	entries, err := os.ReadDir(m.Dir)
	if err != nil {
		return nil, err
	}
	active, last := m.Active(), m.Last()
	var ps []Profile
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() || name == "_autosave" || strings.HasPrefix(name, ".") {
			continue
		}
		created, modified, size := statTree(m.ProfileDir(name))
		ps = append(ps, Profile{name, created, modified, size, name == active, name == last})
	}
	sort.SliceStable(ps, func(i, j int) bool {
		switch sortKey {
		case "name":
			return ps[i].Name < ps[j].Name
		case "modified":
			return ps[i].Modified.Before(ps[j].Modified)
		default:
			return ps[i].Created.Before(ps[j].Created)
		}
	})
	if reverse {
		for i, j := 0, len(ps)-1; i < j; i, j = i+1, j-1 {
			ps[i], ps[j] = ps[j], ps[i]
		}
	}
	return ps, nil
}

// statTree returns the birth time, newest mtime, and total byte size of a tree.
func statTree(dir string) (created, modified time.Time, size int64) {
	created = birthTime(dir)
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		if info.ModTime().After(modified) {
			modified = info.ModTime()
		}
		if !d.IsDir() {
			size += info.Size()
		}
		return nil
	})
	if created.IsZero() {
		created = modified
	}
	return created, modified, size
}

// HistoryLocked reports whether the GitHub app or Copilot CLI currently hold a
// session DB open, which makes history writes unsafe.
func (m *Manager) HistoryLocked() bool {
	if _, err := exec.LookPath("lsof"); err != nil {
		return false
	}
	home, _ := os.UserHomeDir()
	for _, db := range []string{
		filepath.Join(home, ".copilot", "data.db"),
		filepath.Join(home, ".copilot", "session-store.db"),
	} {
		if !exists(db) {
			continue
		}
		out, _ := exec.Command("lsof", "-t", "--", db).Output()
		if strings.TrimSpace(string(out)) != "" {
			return true
		}
	}
	return false
}

// ProfileHasHistory reports whether a profile holds any saved session history.
func (m *Manager) ProfileHasHistory(name string) bool {
	for _, a := range m.Assets {
		if a.Kind == KindHistory && exists(filepath.Join(m.ProfileDir(name), a.Rel)) {
			return true
		}
	}
	return false
}

// FmtDate formats a timestamp as YYYY-MM-DD, or "-" if zero.
func FmtDate(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Format("2006-01-02")
}

// HumanSize renders a byte count as a short human-readable string.
func HumanSize(b int64) string {
	switch {
	case b < 1024:
		return fmt.Sprintf("%dB", b)
	case b < 1<<20:
		return fmt.Sprintf("%dK", b/1024)
	case b < 1<<30:
		return fmt.Sprintf("%.1fM", float64(b)/(1<<20))
	default:
		return fmt.Sprintf("%.1fG", float64(b)/(1<<30))
	}
}
