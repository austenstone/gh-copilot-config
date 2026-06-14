# gh-copilot-config (v2)

A [`gh` CLI extension](https://docs.github.com/en/github-cli/github-cli/creating-github-cli-extensions)
to **save**, **restore**, and **create** named profiles of your GitHub Copilot
customizations, with a one-command **clean** toggle for a vanilla workspace.

This is a clean-room rewrite of the original in **pure Go**. Same behavior, one
language, one binary.

## Why a rewrite

The original was a Go binary that embedded a Bash engine that shelled out to a
Node.js script for JSONC editing: three languages stacked to manage local files.
v2 collapses that into a single Go program:

| Concern | Original | v2 |
|---|---|---|
| CLI + TUI | Go (bubbletea) | Go (cobra + bubbletea) |
| Engine / file ops | embedded Bash | native Go |
| JSONC comment preservation | embedded Node (`jsonc-parser`) | [`tailscale/hujson`](https://github.com/tailscale/hujson) |
| Directory sync | `rsync` | native Go (mirror + prune) |
| TUI → engine | subprocess (`exec bash exec node`) | in-process method calls |

External tools are still used only where they earn it: `sqlite3` for consistent
`VACUUM INTO` DB snapshots (with a file-copy fallback), `diff -ruN` for the diff
command, and `lsof` to detect a locked history DB.

## What it manages

Copilot customizations are scattered across several surfaces. This tool treats them
as a single declarative profile:

| Surface | Location |
|---|---|
| **Copilot CLI** | `~/.copilot/` (instructions, skills, agents, extensions, hooks, plugins, MCP, settings) |
| **Cross-tool agent config** | `~/.agents/` (skills + `.skill-lock.json`) |
| **GitHub Copilot.app** | `~/.copilot/m-settings.json`, `~/.copilot/m-mcp-servers.json`, safety snapshot of `~/.copilot/data.db` |
| **VS Code** (Code + Insiders) | `settings.json` (Copilot keys only), `mcp.json`, `prompts/`, `keybindings.json` |
| **Session history** (opt-in) | history DBs/state via `--with-history` |

VS Code `settings.json` is edited by key: only `github.copilot*`, `chat*`, and
`mcp*` keys are extracted/merged, and **all surrounding comments and unmanaged keys
are preserved** via the JSONC AST.

## Commands

```
list (ls)                 list profiles
status                    show active profile and drift
save  <name>              capture live config into a profile
apply <name>  (restore)   apply a profile to live config (auto-snapshots first)
clean         (off)       reset live config to vanilla (apply the empty 'clean' profile)
on                        re-apply your last non-clean profile
new   <name>              create an empty profile  (--from <name> to copy)
rm    <name>  (delete)    delete a profile
diff  <name>              show drift between live and a profile
tui   (ui)                launch the interactive TUI
```

Global flags: `--dry-run` (print planned writes, change nothing), `--all` (include
optional assets), `--with-history` (include session history), `--force`/`-y` (skip
confirmation).

Run a bare `gh copilot-config` in a TTY and you get the bubbletea TUI. Any
non-interactive invocation (piped, scripted, `CC_NO_TUI=1`) stays pure CLI.

## Behavior guarantees

- **apply auto-snapshots first** into `_autosave/<timestamp>` before any destructive write.
- **clean = the empty profile** = vanilla Copilot, nothing customized.
- **DB snapshots are backup-only**: captured on save, never restored on apply.
- **history is opt-in** (`--with-history`) and refuses while the DB is locked.
- **comments survive**: JSONC edits preserve every comment and unmanaged key.

## Layout

```
v2/
  main.go                       thin entry point
  internal/cmd/                 cobra commands + flags (root.go, commands.go)
  internal/profile/             the engine
    manifest.go                 asset table (what to manage, where it lives)
    store.go                    profiles dir, markers, listing
    asset.go                    save/apply/diff, FS helpers, db snapshot
    jsonc.go                    comment-preserving VS Code settings edits
    stat_darwin.go / stat_other.go   birthtime via build tags
    jsonc_test.go               comment-preservation tests
  internal/tui/tui.go           bubbletea UI (in-process engine calls)
```

## Build & run

```console
cd v2
go build -o gh-copilot-config ./
./gh-copilot-config list
```

Environment:

- `CC_PROFILES` overrides the profiles directory (default
  `$XDG_CONFIG_HOME/gh-copilot-config/profiles`).
- `CC_NO_TUI=1` forces CLI mode even in a terminal.

Requires Go 1.24+ to build. At runtime needs only `gh`; `sqlite3`, `diff`, and
`lsof` are used opportunistically and degrade gracefully.
