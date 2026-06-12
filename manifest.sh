#!/usr/bin/env bash
# Declarative asset manifest for gh-copilot-config.
# Sourced by the entrypoint. Defines WHAT gets saved/applied and HOW.
#
# Each managed asset is one record in the MANIFEST array, pipe-delimited:
#   name|type|surface|live|profile_rel|extra
#
#   name        stable id (for diff/status output)
#   type        file | dir | json-keys | skill-list | db-snapshot
#   surface     cli | app | vscode-code | vscode-insiders   (grouping only)
#   live        absolute path of the live asset
#   profile_rel path under profiles/<profile>/
#   extra       type-specific:
#                 json-keys  -> ERE matching managed top-level keys
#                 skill-list -> space-separated allowlist of skill dir names
#                 (suffix " optional" on any record = skipped unless --all;
#                  optional assets are only WRITTEN, never deleted from live)
#
#   db-snapshot  backup-only. A consistent, WAL-checkpointed copy of a live
#                SQLite DB is written into the profile on `save`, but it is
#                NEVER restored on `apply` (the live DB is install-global state
#                we won't clobber) and never reported as drift. Pure safety net.
#
# Paths may contain spaces (VS Code), so records are split on '|' only.

# --- live roots ---
CC_COPILOT="${HOME}/.copilot"
CC_VSCODE="${HOME}/Library/Application Support/Code/User"
CC_VSCODE_INS="${HOME}/Library/Application Support/Code - Insiders/User"

# Top-level VS Code settings keys we consider "Copilot" (everything else is
# the user's editor config and is left untouched).
CC_VSCODE_KEY_RE='^(github\.copilot|chat|mcp)'

# Custom CLI skills we own. Builtin/shipped skills are intentionally excluded
# so we never clobber them.
CC_CLI_SKILLS='about-austen fgpat-deeplink gemini-image gemini-search github-media'

# shellcheck disable=SC2034  # CC_* and MANIFEST are consumed by the entrypoint
MANIFEST=(
  # --- Copilot CLI (~/.copilot) ---
  "cli-instructions|file|cli|${CC_COPILOT}/copilot-instructions.md|cli/copilot-instructions.md|"
  "cli-instructions-dir|dir|cli|${CC_COPILOT}/instructions|cli/instructions|"
  "cli-skills|skill-list|cli|${CC_COPILOT}/skills|cli/skills|${CC_CLI_SKILLS}"
  "cli-extensions|dir|cli|${CC_COPILOT}/extensions|cli/extensions|"
  "cli-hooks|dir|cli|${CC_COPILOT}/hooks|cli/hooks|"
  "cli-installed-plugins|dir|cli|${CC_COPILOT}/installed-plugins|cli/installed-plugins|"
  "cli-mcp|file|cli|${CC_COPILOT}/mcp-config.json|cli/mcp-config.json|"
  "cli-settings|file|cli|${CC_COPILOT}/settings.json|cli/settings.json|"
  "cli-permissions|file|cli|${CC_COPILOT}/permissions-config.json|cli/permissions-config.json|"

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
)
