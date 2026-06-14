#!/usr/bin/env bash
# Declarative asset manifest for gh-copilot-config.
# Sourced by the entrypoint. Defines WHAT gets saved/applied and HOW.
#
# Each managed asset is one record in the MANIFEST array, pipe-delimited:
#   name|type|surface|live|profile_rel|extra
#
#   name        stable id (for diff/status output)
#   type        file | dir | json-keys | db-snapshot | history
#   surface     cli | app | vscode-code | vscode-insiders | history  (grouping)
#   live        absolute path of the live asset
#   profile_rel path under profiles/<profile>/
#   extra       type-specific:
#                 json-keys  -> ERE matching managed top-level keys
#                 (suffix " optional" on any record = skipped unless --all;
#                  optional assets are only WRITTEN, never deleted from live)
#
#   db-snapshot  backup-only. A consistent, WAL-checkpointed copy of a live
#                SQLite DB is written into the profile on `save`, but it is
#                NEVER restored on `apply` (the live DB is install-global state
#                we won't clobber) and never reported as drift. Pure safety net.
#
#   history      OPT-IN, heavy. The GitHub app / CLI session history (session
#                DBs + on-disk per-session state, ~hundreds of MB). Skipped
#                entirely unless --with-history is passed, and never counted as
#                drift. With --with-history: `save` makes a full copy into the
#                profile; `apply <p>` restores it (replaces live); `clean`
#                REMOVES it from live so the app recreates an empty set on next
#                launch. Whole-file/dir copy, no in-place DB surgery. Because
#                the app holds these DBs open, history writes require the app
#                and Copilot CLI to be quit first (enforced via a lock check).
#
# Paths may contain spaces (VS Code), so records are split on '|' only.

# --- live roots ---
CC_COPILOT="${HOME}/.copilot"
CC_AGENTS="${HOME}/.agents"
CC_VSCODE="${HOME}/Library/Application Support/Code/User"
CC_VSCODE_INS="${HOME}/Library/Application Support/Code - Insiders/User"

# Top-level VS Code settings keys we consider "Copilot" (everything else is
# the user's editor config and is left untouched).
CC_VSCODE_KEY_RE='^(github\.copilot|chat|mcp)'

# shellcheck disable=SC2034  # CC_* and MANIFEST are consumed by the entrypoint
MANIFEST=(
  # --- Copilot CLI (~/.copilot) ---
  "cli-instructions|file|cli|${CC_COPILOT}/copilot-instructions.md|cli/copilot-instructions.md|"
  "cli-instructions-dir|dir|cli|${CC_COPILOT}/instructions|cli/instructions|"
  # All user-scope skills on disk (custom + bundled Anthropic example skills).
  # The 3 true built-ins live inside the CLI binary, not on disk, so they are
  # never captured or at risk here.
  "cli-skills|dir|cli|${CC_COPILOT}/skills|cli/skills|"
  "cli-extensions|dir|cli|${CC_COPILOT}/extensions|cli/extensions|"
  "cli-hooks|dir|cli|${CC_COPILOT}/hooks|cli/hooks|"
  "cli-installed-plugins|dir|cli|${CC_COPILOT}/installed-plugins|cli/installed-plugins|"
  "cli-mcp|file|cli|${CC_COPILOT}/mcp-config.json|cli/mcp-config.json|"
  "cli-settings|file|cli|${CC_COPILOT}/settings.json|cli/settings.json|"
  "cli-permissions|file|cli|${CC_COPILOT}/permissions-config.json|cli/permissions-config.json|"
  # Personal custom agents (user-profile scope), if present.
  "cli-agents|dir|cli|${CC_COPILOT}/agents|cli/agents|"

  # --- Cross-tool personal agent config (~/.agents) ---
  # The agent-skills spec also defines a tool-neutral personal location at
  # ~/.agents/skills (used by `find-skills` and skills installed from GitHub
  # repos, e.g. azure-postgres). .skill-lock.json records each skill's source
  # for reinstall/provenance, so it is captured alongside the skills.
  "agents-skills|dir|agents|${CC_AGENTS}/skills|agents/skills|"
  "agents-skill-lock|file|agents|${CC_AGENTS}/.skill-lock.json|agents/.skill-lock.json|"

  # --- GitHub Copilot.app (Tauri) ---
  "app-settings|file|app|${CC_COPILOT}/m-settings.json|app/m-settings.json|"
  "app-mcp|file|app|${CC_COPILOT}/m-mcp-servers.json|app/m-mcp-servers.json|"

  # App + CLI shared SQLite DB: holds scheduled automations/workflows, projects,
  # workspaces, settings, app_state. Backed up for safety, never restored.
  "app-data-db|db-snapshot|app|${CC_COPILOT}/data.db|app/data.db|"

  # --- VS Code (stable) ---
  "code-settings|json-keys|vscode-code|${CC_VSCODE}/settings.json|vscode/code/settings.copilot.json|${CC_VSCODE_KEY_RE}"
  "code-mcp|file|vscode-code|${CC_VSCODE}/mcp.json|vscode/code/mcp.json|"
  "code-prompts|dir|vscode-code|${CC_VSCODE}/prompts|vscode/code/prompts|"
  "code-keybindings|file|vscode-code|${CC_VSCODE}/keybindings.json|vscode/code/keybindings.json|optional"

  # --- VS Code (Insiders) ---
  "ins-settings|json-keys|vscode-insiders|${CC_VSCODE_INS}/settings.json|vscode/code-insiders/settings.copilot.json|${CC_VSCODE_KEY_RE}"
  "ins-mcp|file|vscode-insiders|${CC_VSCODE_INS}/mcp.json|vscode/code-insiders/mcp.json|"
  "ins-prompts|dir|vscode-insiders|${CC_VSCODE_INS}/prompts|vscode/code-insiders/prompts|"
  "ins-keybindings|file|vscode-insiders|${CC_VSCODE_INS}/keybindings.json|vscode/code-insiders/keybindings.json|optional"

  # --- Session history (OPT-IN: --with-history only; heavy, ~hundreds of MB) ---
  # Full backup of the GitHub app / Copilot CLI session history. On clean these
  # are removed so the app recreates an empty set; apply restores them. The app
  # and CLI must be quit first (lock check enforces this). See the `history`
  # type docs at the top of this file.
  "hist-data-db|history|history|${CC_COPILOT}/data.db|history/data.db|"
  "hist-session-store|history|history|${CC_COPILOT}/session-store.db|history/session-store.db|"
  "hist-session-state|history|history|${CC_COPILOT}/session-state|history/session-state|"
  "hist-chats|history|history|${CC_COPILOT}/chats|history/chats|"
)
