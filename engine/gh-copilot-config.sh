#!/usr/bin/env bash
# gh-copilot-config — save, restore, and toggle GitHub Copilot customizations
# across the Copilot CLI (~/.copilot), the Copilot app (Tauri), and VS Code.
#
# Profiles are directories under profiles/. "clean" is just an empty profile,
# so applying it yields a vanilla Copilot. Every apply auto-snapshots the live
# state first, so nothing is ever lost.
set -euo pipefail

CC_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
# Profiles hold your personal config data, so they live OUTSIDE the tool repo
# by default (decoupled from the shareable code). Override with CC_PROFILES.
CC_PROFILES="${CC_PROFILES:-${XDG_CONFIG_HOME:-${HOME}/.config}/gh-copilot-config/profiles}"

# shellcheck source=manifest.sh
source "${CC_DIR}/manifest.sh"
# shellcheck source=lib/common.sh
source "${CC_DIR}/lib/common.sh"
# shellcheck source=lib/ui.sh
source "${CC_DIR}/lib/ui.sh"

# Ensure the profiles store exists and always has an empty "clean" profile,
# so a fresh install (with no migrated data) can still toggle to vanilla.
mkdir -p "${CC_PROFILES}/clean"
[[ -e "${CC_PROFILES}/clean/.keep" ]] || : >"${CC_PROFILES}/clean/.keep"

DRY_RUN=0
INCLUDE_OPTIONAL=0
INCLUDE_HISTORY=0
FORCE=0

usage() {
  cat <<'EOF'
gh copilot-config — manage Copilot customization profiles

USAGE
  gh copilot-config <command> [args] [--dry-run] [--all] [--force] [--with-history]

COMMANDS
  list [--sort K] [--reverse]  List profiles (* = active). Sort K: created|modified|name
  status                     Show active profile and drift vs live
  save <name>                Snapshot current live config into profile <name>
  apply <name>               Apply profile <name> to live (alias: restore)
  clean                      Apply the empty 'clean' profile (alias: off)
  on [name]                  Re-apply last non-clean profile (or <name>/default)
  new <name> [--from <b>]     Create a profile (empty, or copied from <b>)
  rm <name>                  Delete a profile (alias: delete)
  diff [name]                Diff live config against profile (default: active)

FLAGS
  --dry-run                  Print planned changes without touching disk
  --all                      Include assets flagged optional (e.g. keybindings)
  --force, -y                Skip confirmation prompts (e.g. overwriting a profile)
  --with-history             Also back up / restore / clear GitHub app + CLI
                             session history (heavy, ~hundreds of MB). Requires
                             the GitHub app and Copilot CLI to be quit first.

NOTES
  • Saving onto an existing profile asks first; pass --force to overwrite,
    and in a non-interactive shell it refuses unless --force is given.
  • Every apply first auto-snapshots live -> profiles/_autosave/<timestamp>.
  • OAuth tokens, logs, caches and other runtime state are never touched.
  • Session history is NEVER touched without --with-history. With it, clean
    removes history (app recreates empty) and apply restores it; changes take
    effect after relaunching the app/CLI.
  • VS Code: only Copilot keys (github.copilot*, chat*, mcp*) are managed;
    your other editor settings and comments are preserved.
  • Run a bare `gh copilot-config` in a terminal for the interactive TUI;
    set CC_NO_TUI=1 to force plain output. Every named command stays plain CLI.
EOF
}

# ---- autosnapshot live state before a destructive apply ------------------
cc_autosnapshot() {
  local ts dir keep keep_hist
  ts="$(date +%Y%m%d-%H%M%S)"
  dir="${CC_PROFILES}/_autosave/${ts}"
  keep="${INCLUDE_OPTIONAL}"
  keep_hist="${INCLUDE_HISTORY}"
  INCLUDE_OPTIONAL=1
  INCLUDE_HISTORY=0   # never balloon autosnapshots with ~hundreds of MB of history
  cc_do "autosnapshot live -> _autosave/${ts}" mkdir -p "${dir}"
  cc_each save "${dir}"
  INCLUDE_OPTIONAL="${keep}"
  INCLUDE_HISTORY="${keep_hist}"
  [[ "${DRY_RUN}" == "1" ]] || cc_info "↳ safety snapshot: profiles/_autosave/${ts}"
}

cmd_list() {
  local sort_key="created" reverse=0 porcelain=0
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --sort)      sort_key="${2:-created}"; shift 2 ;;
      --sort=*)    sort_key="${1#*=}"; shift ;;
      --reverse|-r) reverse=1; shift ;;
      --porcelain) porcelain=1; shift ;;
      *)           shift ;;
    esac
  done

  local active last
  active="$(cc_active)"
  last="$(cc_last)"

  # Machine-readable: TSV name<TAB>createdEpoch<TAB>mtimeEpoch<TAB>sizeKB<TAB>active<TAB>last
  if [[ "${porcelain}" == "1" ]]; then
    local p b m kb a l
    while IFS= read -r p; do
      [[ -n "${p}" ]] || continue
      read -r b m kb < <(cc_profile_meta "${p}")
      a=0; [[ "${p}" == "${active}" ]] && a=1
      l=0; [[ "${p}" == "${last}" ]] && l=1
      printf '%s\t%s\t%s\t%s\t%s\t%s\n' "${p}" "${b}" "${m}" "${kb}" "${a}" "${l}"
    done < <(cc_list_profiles)
    return 0
  fi

  # Collect "sortEpoch|active|name|birth|mtime|sizeKB" rows.
  local -a rows=()
  local p b m kb key act namew=7
  while IFS= read -r p; do
    [[ -n "${p}" ]] || continue
    read -r b m kb < <(cc_profile_meta "${p}")
    case "${sort_key}" in
      modified) key="${m}" ;;
      name)     key="0" ;;
      *)        key="${b}" ;;
    esac
    act=0; [[ "${p}" == "${active}" ]] && act=1
    rows+=("${key}|${act}|${p}|${b}|${m}|${kb}")
    (( ${#p} > namew )) && namew="${#p}"
  done < <(cc_list_profiles)

  if [[ ${#rows[@]} -eq 0 ]]; then
    cc_warn "no profiles yet — create one with: gh copilot-config save <name>"
    return 0
  fi

  local sorted
  if [[ "${sort_key}" == "name" ]]; then
    sorted="$(printf '%s\n' "${rows[@]}" | sort -t'|' -k3,3)"
  else
    sorted="$(printf '%s\n' "${rows[@]}" | sort -t'|' -k1,1n)"
  fi
  if [[ "${reverse}" == "1" ]]; then
    sorted="$(printf '%s\n' "${sorted}" | { tail -r 2>/dev/null || tac; })"
  fi

  local header body='' line mark created modified size
  header="$(printf '  %-*s  %-10s  %-10s  %6s' "${namew}" "PROFILE" "CREATED" "MODIFIED" "SIZE")"
  while IFS='|' read -r key act p b m kb; do
    [[ -n "${p}" ]] || continue
    mark='  '; [[ "${act}" == "1" ]] && mark='* '
    created="$(cc_fmt_date "${b}")"
    modified="$(cc_fmt_date "${m}")"
    size="$(cc_human_size "${kb}")"
    line="$(printf '%s%-*s  %-10s  %-10s  %6s' "${mark}" "${namew}" "${p}" "${created}" "${modified}" "${size}")"
    if [[ "${act}" == "1" ]]; then
      body+="${_C_GRN}${line}${_C_RST}"$'\n'
    else
      body+="${line}"$'\n'
    fi
  done <<<"${sorted}"

  cc_render_list "${header}" "${body}" "${last}"
  return 0
}

cmd_save() {
  local name="${1:-}"
  if [[ -z "${name}" ]] && cc_tui; then
    name="$(gum input --header 'Save current config as:' --placeholder 'profile name')" || true
  fi
  [[ -n "${name}" ]] || cc_die "save: need a profile name"
  [[ "${name}" == _* ]] && cc_die "save: names starting with _ are reserved"
  if cc_profile_exists "${name}"; then
    cc_confirm "save: profile '${name}' already exists, overwrite it?" \
      || cc_die "save aborted (profile '${name}' left unchanged)."
  fi
  local dir; dir="$(cc_profile_dir "${name}")"
  cc_do "create profile dir ${name}" mkdir -p "${dir}"
  cc_each save "${dir}"
  cc_ok "saved live config -> profile '${name}'"
}

cmd_apply() {
  local name="${1:-}"
  if [[ -z "${name}" ]] && cc_tui; then
    name="$(cc_pick_profile 'Apply which profile?')" || cc_die "apply: cancelled"
  fi
  [[ -n "${name}" ]] || cc_die "apply: need a profile name"
  cc_profile_exists "${name}" || cc_die "apply: no such profile '${name}'"

  # --with-history performs destructive whole-file ops on the session DBs the
  # app/CLI hold open. Guard hard: refuse while they're running (unless forced),
  # and confirm before clearing live history.
  if [[ "${INCLUDE_HISTORY}" == "1" && "${DRY_RUN}" != "1" ]]; then
    if cc_history_locked && [[ "${FORCE}" != "1" ]]; then
      cc_die "session DBs are open. Quit the GitHub app and Copilot CLI, then re-run from a plain terminal (or pass --force to override — risky)."
    fi
    local prof_has_hist=0 rec
    for rec in "${MANIFEST[@]}"; do
      cc_parse "${rec}"
      cc_is_history || continue
      [[ -e "$(cc_profile_dir "${name}")/${R_PROFREL}" ]] && { prof_has_hist=1; break; }
    done
    if [[ "${prof_has_hist}" == "1" ]]; then
      cc_confirm "apply --with-history: this REPLACES your live session history with profile '${name}'. Continue?" \
        || cc_die "apply aborted (live history left unchanged)."
    elif [[ "${name}" == "clean" ]]; then
      cc_confirm "clean --with-history: this DELETES your live session history (the app will recreate an empty set). Continue?" \
        || cc_die "clean aborted (live history left unchanged)."
    fi
  fi

  [[ "${name}" == "clean" ]] && CC_APPLY_CLEAN=1 || CC_APPLY_CLEAN=0
  cc_autosnapshot
  cc_each apply "$(cc_profile_dir "${name}")"
  cc_set_active "${name}"
  cc_ok "applied profile '${name}'"
  [[ "${INCLUDE_HISTORY}" == "1" ]] && cc_info "↳ relaunch the GitHub app / Copilot CLI for history changes to take effect."
  return 0
}

cmd_clean() { cmd_apply clean; }

cmd_on() {
  local name="${1:-}"
  if [[ -z "${name}" ]]; then
    name="$(cc_last)"; [[ -z "${name}" ]] && name="default"
  fi
  cmd_apply "${name}"
}

cmd_new() {
  local name="" base="" empty=1
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --from) base="${2:-}"; empty=0; shift 2 ;;
      --empty) empty=1; shift ;;
      *) name="$1"; shift ;;
    esac
  done
  [[ -n "${name}" ]] || cc_die "new: need a profile name"
  [[ "${name}" == _* ]] && cc_die "new: names starting with _ are reserved"
  cc_profile_exists "${name}" && cc_die "new: profile '${name}' already exists"
  local dir; dir="$(cc_profile_dir "${name}")"
  if [[ "${empty}" == "0" ]]; then
    cc_profile_exists "${base}" || cc_die "new: base profile '${base}' not found"
    cc_do "copy '${base}' -> '${name}'" cp -a "$(cc_profile_dir "${base}")" "${dir}"
    cc_ok "created profile '${name}' from '${base}'"
  else
    cc_do "create empty profile '${name}'" mkdir -p "${dir}"
    cc_do "mark keep" touch "${dir}/.keep"
    cc_ok "created empty profile '${name}'"
  fi
}

cmd_rm() {
  local name="${1:-}"
  if [[ -z "${name}" ]] && cc_tui; then
    name="$(cc_pick_profile 'Delete which profile?')" || cc_die "rm: cancelled"
  fi
  [[ -n "${name}" ]] || cc_die "rm: need a profile name"
  [[ "${name}" == "clean" ]] && cc_die "rm: refusing to delete the built-in 'clean' profile"
  [[ "${name}" == _* ]] && cc_die "rm: names starting with _ are reserved"
  cc_profile_exists "${name}" || cc_die "rm: no such profile '${name}'"
  cc_confirm "rm: permanently delete profile '${name}'?" \
    || cc_die "rm aborted (profile '${name}' left unchanged)."
  local dir; dir="$(cc_profile_dir "${name}")"
  cc_do "delete profile '${name}'" rm -rf "${dir:?}"
  [[ "$(cc_active)" == "${name}" ]] && cc_do "clear active marker" rm -f "${CC_PROFILES}/.active"
  [[ "$(cc_last)" == "${name}" ]] && cc_do "clear last marker" rm -f "${CC_PROFILES}/.last"
  cc_ok "deleted profile '${name}'"
}

# Snapshot live into a temp dir and diff it against a profile.
cmd_diff() {
  local name="${1:-}"
  [[ -n "${name}" ]] || name="$(cc_active)"
  if [[ -z "${name}" ]] && cc_tui; then
    name="$(cc_pick_profile 'Diff against which profile?')" || cc_die "diff: cancelled"
  fi
  [[ -n "${name}" ]] || cc_die "diff: no active profile; pass a name"
  cc_profile_exists "${name}" || cc_die "diff: no such profile '${name}'"
  local tmp out rc=0
  tmp="$(mktemp -d)"
  out="$(mktemp)"
  local keep="${DRY_RUN}"; DRY_RUN=0
  cc_each save "${tmp}"
  DRY_RUN="${keep}"
  # db-snapshot assets are backup-only and the live DB mutates constantly, so a
  # saved snapshot can never byte-match a fresh one. Always exclude them.
  local -a ex=(--exclude '.keep')
  local rec
  for rec in "${MANIFEST[@]}"; do
    cc_parse "${rec}"
    { [[ "${R_TYPE}" == "db-snapshot" ]] || cc_is_history; } \
      && ex+=(--exclude "$(basename "${R_PROFREL}")")
  done
  # Without --all the live snapshot skips optional assets, so exclude those
  # paths from the profile side too, otherwise diff falsely reports them gone.
  if [[ "${INCLUDE_OPTIONAL}" != "1" ]]; then
    for rec in "${MANIFEST[@]}"; do
      cc_parse "${rec}"
      cc_is_optional && ex+=(--exclude "$(basename "${R_PROFREL}")")
    done
  fi
  if diff -ruN "${ex[@]}" "$(cc_profile_dir "${name}")" "${tmp}" >"${out}" 2>&1; then
    cc_ok "no drift: live matches profile '${name}'"
  else
    cc_warn "drift vs profile '${name}' (- profile, + live):"
    sed -e "s#$(cc_profile_dir "${name}")#<profile:${name}>#g" -e "s#${tmp}#<live>#g" "${out}"
    rc=1
  fi
  rm -rf "${tmp}" "${out}"
  return "${rc}"
}

cmd_status() {
  local active last
  active="$(cc_active)"; last="$(cc_last)"
  cc_info "active profile : ${active:-<none>}"
  cc_log  "last non-clean : ${last:-<none>}"
  cc_log  "profiles dir   : ${CC_PROFILES}"
  if [[ -n "${active}" ]] && cc_profile_exists "${active}"; then
    if cmd_diff "${active}" >/dev/null 2>&1; then
      cc_ok "live is in sync with '${active}'"
    else
      cc_warn "live has drifted from '${active}' (run: gh copilot-config diff)"
    fi
  fi
  return 0
}

main() {
  local args=()
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --dry-run) DRY_RUN=1; shift ;;
      --all) INCLUDE_OPTIONAL=1; shift ;;
      --with-history) INCLUDE_HISTORY=1; shift ;;
      --force|-y|--yes) FORCE=1; shift ;;
      -h|--help|help) usage; exit 0 ;;
      *) args+=("$1"); shift ;;
    esac
  done
  if [[ ${#args[@]} -eq 0 ]]; then
    if cc_tui; then cc_menu; exit $?; fi
    usage; exit 0
  fi

  local cmd="${args[0]}"; args=("${args[@]:1}")
  case "${cmd}" in
    list)            cmd_list "${args[@]:-}" ;;
    status)          cmd_status ;;
    save)            cmd_save "${args[@]:-}" ;;
    apply|restore)   cmd_apply "${args[@]:-}" ;;
    clean|off)       cmd_clean ;;
    on)              cmd_on "${args[@]:-}" ;;
    new)             cmd_new "${args[@]:-}" ;;
    rm|delete)       cmd_rm "${args[@]:-}" ;;
    diff)            cmd_diff "${args[@]:-}" ;;
    *) cc_die "unknown command '${cmd}' (try: gh copilot-config help)" ;;
  esac
}

main "$@"
