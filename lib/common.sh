#!/usr/bin/env bash
# Shared helpers for gh-copilot-config. Sourced by the entrypoint.
# Relies on: CC_DIR (repo root), CC_PROFILES, manifest already sourced,
# DRY_RUN (0/1), INCLUDE_OPTIONAL (0/1).

# ---- logging ------------------------------------------------------------
if [[ -t 1 ]]; then
  _C_DIM=$'\033[2m'; _C_RED=$'\033[31m'; _C_GRN=$'\033[32m'
  _C_YEL=$'\033[33m'; _C_BLU=$'\033[34m'; _C_RST=$'\033[0m'
else
  _C_DIM=''; _C_RED=''; _C_GRN=''; _C_YEL=''; _C_BLU=''; _C_RST=''
fi
cc_log()  { printf '%s\n' "$*"; }
cc_info() { printf '%s%s%s\n' "${_C_BLU}" "$*" "${_C_RST}"; }
cc_ok()   { printf '%s%s%s\n' "${_C_GRN}" "$*" "${_C_RST}"; }
cc_warn() { printf '%s%s%s\n' "${_C_YEL}" "$*" "${_C_RST}" >&2; }
cc_die()  { printf '%s%s%s\n' "${_C_RED}" "$*" "${_C_RST}" >&2; exit 1; }

# ---- mutation wrapper (honors --dry-run) --------------------------------
# cc_do "human description" command args...
cc_do() {
  local desc="$1"; shift
  if [[ "${DRY_RUN}" == "1" ]]; then
    printf '%s[dry-run]%s %s\n' "${_C_DIM}" "${_C_RST}" "${desc}"
    return 0
  fi
  "$@"
}

# ---- rsync a directory faithfully (exclude VCS/OS noise) ----------------
cc_copy_dir() {
  local src="$1" dest="$2"
  mkdir -p "${dest}"
  rsync -a --delete \
    --exclude '.git/' --exclude '.DS_Store' \
    "${src}/" "${dest}/"
}

# ---- record parsing -----------------------------------------------------
# Splits a MANIFEST record into the named globals R_*.
cc_parse() {
  # shellcheck disable=SC2034  # R_SURFACE is grouping metadata, not yet consumed
  IFS='|' read -r R_NAME R_TYPE R_SURFACE R_LIVE R_PROFREL R_EXTRA <<<"$1"
}

cc_is_optional() { [[ "${R_EXTRA}" == "optional" ]]; }

# Should this record be processed given INCLUDE_OPTIONAL?
cc_skip_optional() {
  cc_is_optional && [[ "${INCLUDE_OPTIONAL}" != "1" ]]
}

# ---- SAVE: live -> profile dir ($1) -------------------------------------
cc_save_asset() {
  local pdir="$1" prof="$1/${R_PROFREL}"
  case "${R_TYPE}" in
    file)
      if [[ -e "${R_LIVE}" ]]; then
        cc_do "save ${R_NAME} (${R_PROFREL})" _cc_cp_file "${R_LIVE}" "${prof}"
      else
        cc_do "drop ${R_NAME} (absent live)" rm -f "${prof}"
      fi
      ;;
    dir)
      if [[ -d "${R_LIVE}" ]]; then
        cc_do "save ${R_NAME}/ (${R_PROFREL})" cc_copy_dir "${R_LIVE}" "${prof}"
      else
        cc_do "drop ${R_NAME}/ (absent live)" rm -rf "${prof}"
      fi
      ;;
    json-keys)
      if [[ -f "${R_LIVE}" ]]; then
        cc_do "extract ${R_NAME} keys (${R_PROFREL})" _cc_extract "${R_LIVE}" "${prof}" "${R_EXTRA}"
      else
        cc_do "drop ${R_NAME} (absent live)" rm -f "${prof}"
      fi
      ;;
    db-snapshot)
      if [[ -e "${R_LIVE}" ]]; then
        cc_do "snapshot ${R_NAME} (${R_PROFREL})" _cc_db_snapshot "${R_LIVE}" "${prof}"
      else
        cc_do "drop ${R_NAME} (absent live)" rm -f "${prof}"
      fi
      ;;
    *) cc_die "unknown asset type: ${R_TYPE}" ;;
  esac
}

_cc_cp_file() { mkdir -p "$(dirname "$2")"; cp -p "$1" "$2"; }

# Consistent, single-file snapshot of a live SQLite DB. `VACUUM INTO` reads a
# coherent transaction even while the app holds the DB open, and writes one
# standalone file with no -wal/-shm sidecars. Falls back to a plain copy only
# if sqlite3 is unavailable.
_cc_db_snapshot() {
  local src="$1" dest="$2"
  mkdir -p "$(dirname "${dest}")"
  if command -v sqlite3 >/dev/null 2>&1; then
    rm -f "${dest}" "${dest}-wal" "${dest}-shm"
    sqlite3 "${src}" "VACUUM INTO '${dest}'"
  else
    cp -p "${src}" "${dest}"
  fi
}

# Extract managed keys to profile; if none, represent as absent (no file).
_cc_extract() {
  local live="$1" prof="$2" re="$3" tmp
  mkdir -p "$(dirname "${prof}")"
  tmp="$(mktemp)"
  node "${CC_DIR}/lib/jsonc.mjs" extract "${live}" "${re}" >"${tmp}"
  if [[ "$(tr -d '[:space:]' <"${tmp}")" == "{}" ]]; then
    rm -f "${prof}" "${tmp}"
  else
    mv "${tmp}" "${prof}"
  fi
}

# ---- APPLY: profile dir ($1) -> live ------------------------------------
cc_apply_asset() {
  local pdir="$1" prof="$1/${R_PROFREL}"
  case "${R_TYPE}" in
    file)
      if [[ -e "${prof}" ]]; then
        cc_do "write ${R_NAME} -> ${R_LIVE}" _cc_cp_file "${prof}" "${R_LIVE}"
      elif [[ -e "${R_LIVE}" ]] && ! cc_is_optional; then
        cc_do "remove ${R_NAME} (${R_LIVE})" rm -f "${R_LIVE}"
      elif [[ -e "${R_LIVE}" ]]; then
        cc_do "keep ${R_NAME} (optional, absent in profile)" true
      fi
      ;;
    dir)
      if [[ -d "${prof}" ]]; then
        cc_do "write ${R_NAME}/ -> ${R_LIVE}" cc_copy_dir "${prof}" "${R_LIVE}"
      elif [[ -d "${R_LIVE}" ]] && ! cc_is_optional; then
        cc_do "remove ${R_NAME}/ (${R_LIVE})" rm -rf "${R_LIVE}"
      elif [[ -d "${R_LIVE}" ]]; then
        cc_do "keep ${R_NAME}/ (optional, absent in profile)" true
      fi
      ;;
    json-keys)
      if [[ -f "${prof}" ]]; then
        cc_do "merge ${R_NAME} keys -> ${R_LIVE}" \
          node "${CC_DIR}/lib/jsonc.mjs" apply "${R_LIVE}" "${prof}" "${R_EXTRA}"
      elif [[ -f "${R_LIVE}" ]]; then
        cc_do "strip ${R_NAME} keys (${R_LIVE})" \
          node "${CC_DIR}/lib/jsonc.mjs" apply "${R_LIVE}" NONE "${R_EXTRA}"
      fi
      ;;
    db-snapshot)
      cc_do "skip ${R_NAME} (backup-only, never restored)" true
      ;;
    *) cc_die "unknown asset type: ${R_TYPE}" ;;
  esac
}

# ---- iterate over manifest ----------------------------------------------
# cc_each save|apply <profile_dir>
cc_each() {
  local action="$1" pdir="$2" rec
  for rec in "${MANIFEST[@]}"; do
    cc_parse "${rec}"
    cc_skip_optional && continue
    case "${action}" in
      save)  cc_save_asset "${pdir}" ;;
      apply) cc_apply_asset "${pdir}" ;;
      *) cc_die "cc_each: bad action ${action}" ;;
    esac
  done
}

# ---- profile bookkeeping ------------------------------------------------
cc_active()      { cat "${CC_PROFILES}/.active" 2>/dev/null || true; }
cc_last()        { cat "${CC_PROFILES}/.last" 2>/dev/null || true; }
cc_set_active()  {
  [[ "${DRY_RUN}" == "1" ]] && return 0
  printf '%s\n' "$1" >"${CC_PROFILES}/.active"
  [[ "$1" != "clean" ]] && printf '%s\n' "$1" >"${CC_PROFILES}/.last"
  return 0
}
cc_profile_dir() { printf '%s/%s' "${CC_PROFILES}" "$1"; }
cc_profile_exists() { [[ -d "$(cc_profile_dir "$1")" ]]; }

# Prompt for y/N confirmation before a destructive action. Bypassed by FORCE=1
# and by dry-run (nothing is written). In a non-interactive shell it refuses
# rather than silently proceeding, so automation can't clobber a profile.
cc_confirm() {
  local prompt="$1" ans
  [[ "${DRY_RUN}" == "1" || "${FORCE:-0}" == "1" ]] && return 0
  if [[ ! -t 0 ]]; then
    cc_die "${prompt} Refusing without confirmation (re-run with --force / -y)."
  fi
  printf '%s%s [y/N] %s' "${_C_YEL}" "${prompt}" "${_C_RST}" >&2
  read -r ans
  [[ "${ans}" =~ ^[Yy]([Ee][Ss])?$ ]]
}

cc_list_profiles() {
  local p
  for p in "${CC_PROFILES}"/*/; do
    [[ -d "${p}" ]] || continue
    p="$(basename "${p}")"
    [[ "${p}" == "_autosave" ]] && continue
    printf '%s\n' "${p}"
  done
}
