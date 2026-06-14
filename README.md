# gh-copilot-config

A [`gh` CLI extension](https://docs.github.com/en/github-cli/github-cli/creating-github-cli-extensions)
to **save**, **restore**, and **create** named profiles of your GitHub Copilot
customizations, with a one-command **clean** toggle for a vanilla workspace.

Written in **pure Go**: one language, one binary, no runtime dependencies.

## Design

Copilot customizations are managed entirely in native Go: file operations,
directory sync, and comment-preserving JSONC edits all run in-process.

| Concern | How |
|---|---|
| CLI + TUI | Go (cobra + bubbletea) |
| Engine / file ops | native Go |
| JSONC comment preservation | [`tailscale/hujson`](https://github.com/tailscale/hujson) |
| Directory sync | native Go (mirror + prune) |
| TUI → engine | in-process method calls |

External tools are used only where they earn it: `sqlite3` for consistent
`VACUUM INTO` DB snapshots (with a file-copy fallback), `diff -ruN` for the diff
command, and `lsof` to detect a locked history DB.

## What it manages

Copilot customizations are scattered across several surfaces. This tool treats them
as a single declarative profile:

| Surface | Location |
|---|---|
| **Copilot CLI** | `~/.copilot/` (instructions, skills, agents, extensions, hooks, plugins, MCP, settings) |
| **Cross-tool agent config** | `~/.agents/` (skills + `.skill-lock.json`) |
| **GitHub Copilot.app** | `~/.copilot/m-settings.json`, `~/.copilot/m-mcp-servers.json` |
| **VS Code** (Code + Insiders) | `settings.json` (Copilot keys only), `mcp.json`, `prompts/`, `keybindings.json` |
| **Session history** (opt-in) | history DBs/state via `--with-history` |
| **Copilot databases** (opt-in) | backup-only snapshot of `~/.copilot/data.db` via `--with-db` |

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
optional assets), `--with-history` (include session history), `--with-db` (snapshot
Copilot databases, backup-only), `--force`/`-y` (skip confirmation).

### Scoping by surface and feature

Copilot config lives on two axes: **surface** (where it lives) and **feature**
(what it is). `save`, `apply`, and `diff` accept `--surface` and `--feature` to
work on a slice instead of the whole profile:

```console
gh copilot-config save  work --surface vscode,cli      # only those tools
gh copilot-config apply work --surface cli             # restore just the CLI
gh copilot-config diff  work --feature instructions,mcp
```

- `--surface`: `cli`, `vscode`, `insiders`, `app`, `agents`, `history`
- `--feature`: `instructions`, `prompts`, `agents`, `skills`, `hooks`, `mcp`,
  `extensions`, `plugins`, `settings`, `db`, `history`

Scoping never weakens safety: `apply` still takes a **full** `_autosave`
snapshot before writing, even when the apply itself is scoped.

Run a bare `gh copilot-config` in a TTY and you get the bubbletea TUI. Any
non-interactive invocation (piped, scripted, `CC_NO_TUI=1`) stays pure CLI. In
the TUI, inspecting a profile lays its config out as **surface tabs** (`tab` to
switch) over **feature sub-tabs** (`←/→`).

## Behavior guarantees

- **apply auto-snapshots first** into `_autosave/<timestamp>` before any destructive write.
- **clean = the empty profile** = vanilla Copilot, nothing customized.
- **DB snapshots are opt-in and backup-only** (`--with-db`): captured on save, never restored on apply.
- **history is opt-in** (`--with-history`) and refuses while the DB is locked.
- **comments survive**: JSONC edits preserve every comment and unmanaged key.

## Layout

```
main.go                       thin entry point
internal/cmd/                 cobra commands + flags (root.go, commands.go)
internal/profile/             the engine
  manifest.go                 asset table (what to manage, where it lives)
  store.go                    profiles dir, markers, listing
  asset.go                    save/apply/diff, FS helpers, db snapshot
  inventory.go                per-profile asset classification (TUI detail view)
  jsonc.go                    comment-preserving VS Code settings edits
  stat_darwin.go / stat_other.go   birthtime via build tags
  jsonc_test.go               comment-preservation tests
internal/tui/tui.go           bubbletea UI (in-process engine calls)
```

## Build & run

```console
go build -o gh-copilot-config .
./gh-copilot-config list
```

Or install as a `gh` extension from the repo root:

```console
gh extension install .
```

Environment:

- `CC_PROFILES` overrides the profiles directory (default
  `$XDG_CONFIG_HOME/gh-copilot-config/profiles`).
- `CC_NO_TUI=1` forces CLI mode even in a terminal.

Requires Go 1.25+ to build. At runtime needs only `gh`; `sqlite3`, `diff`, and
`lsof` are used opportunistically and degrade gracefully.
