#!/usr/bin/env bash
# Optional TUI layer for gh-copilot-config, powered by charmbracelet/gum.
# Everything here degrades gracefully: with no gum, no TTY, or CC_NO_TUI=1 the
# callers fall back to the plain bash paths. Sourced AFTER common.sh, so it can
# wrap cc_confirm and reuse cc_list_profiles / cmd_* at call time.

cc_has_gum() { command -v gum >/dev/null 2>&1; }

# Is the interactive TUI usable right now? Needs gum, an interactive stdout,
# and no explicit opt-out.
cc_tui() {
  [[ "${CC_NO_TUI:-0}" == "1" ]] && return 1
  [[ -t 1 ]] || return 1
  cc_has_gum
}

# Render the profile table: bordered + colored when gum is around, plain
# aligned text otherwise. $1=header row, $2=body (newline-joined rows),
# $3=last non-clean name (may be empty).
cc_render_list() {
  local header="$1" body="$2" last="$3"
  body="${body%$'\n'}"
  if cc_tui; then
    gum style --border rounded --border-foreground 212 --padding '0 1' \
      "${header}"$'\n'"${body}"
  else
    printf '%s\n%s\n' "${header}" "${body}"
  fi
  [[ -n "${last}" ]] && cc_log "${_C_DIM}last non-clean: ${last}${_C_RST}"
  return 0
}

# Interactively pick an existing profile. Echoes the choice, non-zero if
# cancelled or none exist. $1 = header label.
cc_pick_profile() {
  local header="${1:-Select a profile}" names
  names="$(cc_list_profiles)"
  [[ -n "${names}" ]] || return 1
  printf '%s\n' "${names}" | gum choose --header "${header}"
}

# Top-level interactive menu shown when invoked with no command in a TUI.
cc_menu() {
  local choice
  choice="$(printf '%s\n' list status apply clean on save new diff help \
    | gum choose --header 'gh copilot-config')" || return 0
  case "${choice}" in
    list)   cmd_list ;;
    status) cmd_status ;;
    apply)  cmd_apply ;;
    clean)  cmd_clean ;;
    on)     cmd_on ;;
    save)   cmd_save ;;
    new)    cmd_new "$(gum input --header 'New profile name:' --placeholder name)" ;;
    diff)   cmd_diff ;;
    help)   usage ;;
    *)      return 0 ;;
  esac
}
