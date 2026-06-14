package main

import (
	"embed"
	"fmt"
	"hash/fnv"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"

	"golang.org/x/term"
)

// The bash engine and its node helper are embedded so the precompiled binary
// is self-contained: gh ships only the binary, so the scripts must travel
// inside it. They're extracted to a per-content cache dir on first run and
// exec'd for all the real work. Go only provides the CLI dispatch + TUI.
//
//go:embed all:engine
var engineFS embed.FS

var version = "dev"

func main() {
	engineDir, err := extractEngine()
	if err != nil {
		fmt.Fprintln(os.Stderr, "gh-copilot-config: preparing engine:", err)
		os.Exit(1)
	}

	args := os.Args[1:]
	if wantTUI(args) {
		if err := runTUI(engineDir); err != nil {
			fmt.Fprintln(os.Stderr, "gh-copilot-config:", err)
			os.Exit(1)
		}
		return
	}
	os.Exit(runEngine(engineDir, args))
}

// A bare invocation on a terminal opens the TUI; `tui`/`ui` force it. Anything
// else (subcommands, pipes) passes straight through to the bash engine, so the
// existing CLI keeps working unchanged.
func wantTUI(args []string) bool {
	if len(args) == 1 && (args[0] == "tui" || args[0] == "ui") {
		return true
	}
	if len(args) == 0 {
		return term.IsTerminal(int(os.Stdout.Fd())) && term.IsTerminal(int(os.Stdin.Fd()))
	}
	return false
}

func enginePath(engineDir string) string {
	return filepath.Join(engineDir, "gh-copilot-config.sh")
}

func runEngine(engineDir string, args []string) int {
	cmd := exec.Command("bash", append([]string{enginePath(engineDir)}, args...)...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.Env = os.Environ()
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if ok := asExitError(err, &ee); ok {
			return ee.ExitCode()
		}
		fmt.Fprintln(os.Stderr, "gh-copilot-config:", err)
		return 1
	}
	return 0
}

func asExitError(err error, target **exec.ExitError) bool {
	if ee, ok := err.(*exec.ExitError); ok {
		*target = ee
		return true
	}
	return false
}

// extractEngine writes the embedded engine to a cache dir keyed by a hash of
// its contents, so a new build re-extracts and an unchanged one is reused.
func extractEngine() (string, error) {
	h := fnv.New64a()
	if err := fs.WalkDir(engineFS, "engine", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, e := engineFS.ReadFile(p)
		if e != nil {
			return e
		}
		_, _ = h.Write([]byte(p))
		_, _ = h.Write(b)
		return nil
	}); err != nil {
		return "", err
	}

	cache, err := os.UserCacheDir()
	if err != nil {
		cache = os.TempDir()
	}
	root := filepath.Join(cache, "gh-copilot-config", strconv.FormatUint(h.Sum64(), 16))
	engineDir := filepath.Join(root, "engine")
	marker := filepath.Join(root, ".extracted")
	if _, err := os.Stat(marker); err == nil {
		return engineDir, nil
	}

	if err := os.RemoveAll(root); err != nil {
		return "", err
	}
	if err := fs.WalkDir(engineFS, "engine", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		target := filepath.Join(root, p)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		b, e := engineFS.ReadFile(p)
		if e != nil {
			return e
		}
		if e := os.MkdirAll(filepath.Dir(target), 0o755); e != nil {
			return e
		}
		return os.WriteFile(target, b, 0o644)
	}); err != nil {
		return "", err
	}
	if err := os.WriteFile(marker, []byte(version), 0o644); err != nil {
		return "", err
	}
	return engineDir, nil
}
