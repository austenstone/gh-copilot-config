# gh-copilot-config

A [`gh` CLI extension](https://docs.github.com/en/github-cli/github-cli/creating-github-cli-extensions)
to **save**, **restore**, and **create** named profiles of your GitHub Copilot
customizations, including a one-command **clean** toggle that gives you a vanilla
workspace with zero customizations.

Copilot customizations are scattered across three surfaces. This tool manages all of
them as a single declarative profile:

| Surface | Location |
|---|---|
| **Copilot CLI** | `~/.copilot/` (instructions, skills, extensions, hooks, plugins, MCP, settings) |
| **GitHub Copilot.app** (Tauri) | `~/.copilot/m-settings.json`, `~/.copilot/m-mcp-servers.json`, and a safety snapshot of `~/.copilot/data.db` (scheduled automations/workflows, projects, workspaces) |
| **VS Code** (Code + Insiders) | `settings.json` (Copilot keys only), `mcp.json`, `prompts/` |

See the official [customization cheat sheet](https://docs.github.com/en/copilot/reference/customization-cheat-sheet)
for context on what each surface does.

## In action

```console
$ gh copilot-config save default     # capture your current setup, once
saved live config -> profile 'default'

$ gh copilot-config clean            # wipe to vanilla Copilot
↳ safety snapshot: profiles/_autosave/20260612-071425
applied profile 'clean'

$ gh copilot-config list
  default
* clean
last non-clean: default

$ gh copilot-config on                # restore your last non-clean profile
applied profile 'default'
```

Curious what an apply will touch? Add `--dry-run` to print every planned
write/removal without changing a thing:

```console
$ gh copilot-config clean --dry-run
[dry-run] autosnapshot live -> _autosave/20260612-071425
[dry-run] drop cli-instructions (absent live)
[dry-run] sync cli-skills allowlist
[dry-run] snapshot app-data-db (app/data.db)
[dry-run] skip app-data-db (backup-only, never restored)
...
```

## Requirements

The extension is plain Bash plus a couple of standard tools, so it runs on **macOS and
Linux** (and WSL). You need:

- [`gh`](https://cli.github.com/) — the GitHub CLI (host for the extension)
- `bash` 4+ — the entrypoint and helpers
- `node` — runs `lib/jsonc.mjs` for comment-preserving VS Code settings edits
  (the `jsonc-parser` dep is vendored in `node_modules/`, so there is no install step)
- `rsync` — directory sync for `dir` assets
- `sqlite3` — `VACUUM INTO` snapshots of the Copilot app's `data.db`

## Install

```bash
# from a clone
gh extension install .

# or, once published
gh extension install austenstone/gh-copilot-config
```

It runs as `gh copilot-config <command>`.

## Usage

```text
gh copilot-config <command> [args] [--dry-run] [--all]

list                  List profiles (* = active)
status                Show active profile and drift vs live
save <name>           Snapshot current live config into profile <name>
apply <name>          Apply profile <name> to live (alias: restore)
clean                 Apply the empty 'clean' profile (alias: off)
on [name]             Re-apply last non-clean profile (or <name>/default)
new <name> [--from b] Create a profile (empty, or copied from <b>)
diff [name]           Diff live config against profile (default: active)

--dry-run             Print planned changes without touching disk
--all                 Include assets flagged optional (e.g. keybindings)
```

### Multiple setups

```bash
gh copilot-config new work --from default   # branch a new profile
gh copilot-config save work                 # snapshot live into it
gh copilot-config apply work                # switch to it
```

## How it works

A declarative [`manifest.sh`](manifest.sh) maps each managed asset to a
`{surface, live path, profile path, type}` record. Type is one of:

- `file` — whole file (e.g. `mcp-config.json`)
- `dir` — whole directory (e.g. `instructions/`, `prompts/`)
- `json-keys` — a key subset merged/stripped from a shared file
- `skill-list` — an explicit allowlist of *custom* skill dirs (shipped builtins
  are never touched)
- `db-snapshot` — **backup-only** consistent copy of a live SQLite DB (e.g.
  `data.db`, which holds the GitHub app's scheduled automations/workflows). Captured
  on `save` via `VACUUM INTO`, but **never restored** on `apply` and never counted as
  drift, the live DB is install-global state the tool won't clobber.

Apply is uniform, which makes "clean" fall out for free: for each managed asset, if
it exists in the target profile it is written to the live location; if it does **not**,
it is removed from live. An empty profile therefore == vanilla Copilot.

### VS Code settings are handled surgically

`settings.json` is shared with all your editor config, so only Copilot keys
(`github.copilot*`, `chat*`, `mcp*`) are extracted/merged/stripped. Your other
editor settings stay put. The tool is JSONC-aware: **comments above your settings are
preserved** on save and apply.

> **Caveat:** a comment on the *same line* as a managed key's value
> (e.g. `"chat.agent.enabled": true, // note`) is dropped. Comments on their own
> line above a key (the common case) are preserved.

## Where your profiles live

The **tool** (this repo) and your **profile data** are deliberately decoupled. Profiles
are your personal Copilot config, so they live **outside** this repo, by default at:

```text
~/.config/gh-copilot-config/profiles      # override with $CC_PROFILES
```

This means:

- This repo stays **code-only** and safe to share or publish. No personal config rides along.
- Your profiles survive `gh extension remove`, reinstalls, and `git clean` in the tool repo.
- The profiles dir is a great spot for its own **private** git repo (dotfiles pattern) so you
  get version history and an off-machine backup. It is git-init'd locally on first run; add a
  **private** remote yourself if you want it backed up:

  ```bash
  cd "${CC_PROFILES:-$HOME/.config/gh-copilot-config/profiles}"
  gh repo create <you>/copilot-profiles --private --source=. --push
  ```

> Keep any profiles remote **private** — profiles contain your instructions, skills, and
> MCP config. (No live tokens are stored: MCP secrets are referenced via `${env:…}` /
> `${input:…}` placeholders, never captured.)

## Safety

- **Saving never silently overwrites a profile.** `save <name>` onto an existing
  profile asks for confirmation first; pass `--force` (or `-y`) to overwrite. In a
  non-interactive shell it refuses unless `--force` is given, so scripts and
  automations can't clobber a profile by accident.
- **Auto-snapshot before every apply.** Live state is copied to
  `<profiles>/_autosave/<timestamp>/` first, so nothing is ever lost.
- **Automations are backed up, never auto-restored.** The GitHub app's scheduled
  workflows live in `~/.copilot/data.db`; that DB is snapshotted into the profile for
  safety but is never written back on `apply` (so toggling profiles can't disrupt or
  duplicate running automations). Restore by hand if you ever need to.
- **Secrets and other runtime state are never backed up:** `mcp-oauth-config/` (OAuth
  tokens), sessions, session-state, `session-store.db`, logs, chats, caches, workspaces,
  repos, worktrees, and other volatile state are all excluded. (`data.db` is the one
  DB captured, backup-only, for the automations it holds.)
- **`--dry-run`** prints planned writes/removals without touching disk.

## Repo layout

```text
gh-copilot-config       # extension entrypoint (bash)
manifest.sh             # declarative asset map
lib/
  common.sh             # save/apply/snapshot helpers
  jsonc.mjs             # comment-preserving JSONC extract/apply (jsonc-parser)
node_modules/           # vendored jsonc-parser (gh extensions have no install step)

# profile DATA lives outside the repo (default ~/.config/gh-copilot-config/profiles):
#   default/   your captured setup (cli/ app/ vscode/)
#   clean/     empty -> applying it = vanilla
#   _autosave/ safety snapshots (local only)
```

## Contributing

Issues and pull requests are welcome at
[austenstone/gh-copilot-config](https://github.com/austenstone/gh-copilot-config).

- The tool is the three shell files above plus `lib/jsonc.mjs`; profile data is never
  part of the repo (see [Where your profiles live](#where-your-profiles-live)).
- Use `--dry-run` while developing to see planned writes/removals without touching disk.
- Adding support for a new asset is usually a one-line entry in
  [`manifest.sh`](manifest.sh) — `apply`, `clean`, `diff`, and `save` all flow from it.

## Version

Current version: **0.1.0** (see [`package.json`](package.json)).
