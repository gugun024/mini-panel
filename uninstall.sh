#!/usr/bin/env bash
set -euo pipefail

REPO_URL="${REPO_URL:-https://github.com/Gugun09/mini-panel}"
REPO_REF="${REPO_REF:-main}"
ARCHIVE_URL="${ARCHIVE_URL:-}"
UNINSTALL_ARGS=()

die() {
  echo "bootstrap uninstall: $*" >&2
  exit 1
}

log() {
  echo "==> $*" >&2
}

usage() {
  cat <<'EOF'
Mini Panel one-shot uninstaller

Usage:
  curl -fsSL https://raw.githubusercontent.com/Gugun09/mini-panel/main/uninstall.sh | sudo bash -s -- --yes [options]

Options:
  --repo-url URL       GitHub repository URL. Default is baked into uninstall.sh.
  --ref REF           Git ref to download. Default: main
  --yes               Required for non-interactive uninstall.
  --keep-databases    Do not drop MariaDB databases/users recorded by Mini Panel.
  --keep-apps         Keep /var/lib/mini-panel/apps.
  --keep-backups      Keep /var/lib/mini-panel/backups.
  --remove-packages   Also apt purge packages installed by the Mini Panel installer.
  --help              Show this help.

Environment alternatives:
  REPO_URL, REPO_REF, ARCHIVE_URL
EOF
}

require_root() {
  [[ "$(id -u)" -eq 0 ]] || die "run with sudo or as root"
}

require_ubuntu() {
  [[ -r /etc/os-release ]] || die "cannot detect OS"
  # shellcheck source=/dev/null
  . /etc/os-release
  [[ "${ID:-}" == "ubuntu" ]] || die "this uninstaller currently supports Ubuntu only"
}

parse_args() {
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --repo-url)
        [[ $# -ge 2 ]] || die "--repo-url requires a value"
        REPO_URL="$2"
        shift 2
        ;;
      --ref)
        [[ $# -ge 2 ]] || die "--ref requires a value"
        REPO_REF="$2"
        shift 2
        ;;
      --yes|--keep-databases|--keep-apps|--keep-backups|--remove-packages)
        UNINSTALL_ARGS+=("$1")
        shift
        ;;
      --help|-h)
        usage
        exit 0
        ;;
      *)
        die "unknown option: $1"
        ;;
    esac
  done
}

install_bootstrap_deps() {
  local missing=()
  for bin in curl tar gzip; do
    if ! command -v "$bin" >/dev/null 2>&1; then
      missing+=("$bin")
    fi
  done

  if [[ ${#missing[@]} -gt 0 ]]; then
    log "Installing bootstrap dependencies"
    apt-get update
    apt-get install -y ca-certificates curl tar gzip
  fi
}

github_archive_url() {
  local repo="$1"
  local ref="$2"
  repo="${repo%.git}"
  repo="${repo#https://github.com/}"
  repo="${repo#http://github.com/}"
  [[ "$repo" == */* ]] || die "REPO_URL must be a GitHub URL"

  if [[ "$ref" == refs/* ]]; then
    printf 'https://codeload.github.com/%s/tar.gz/%s\n' "$repo" "$ref"
  else
    printf 'https://codeload.github.com/%s/tar.gz/refs/heads/%s\n' "$repo" "$ref"
  fi
}

download_source() {
  local workdir archive srcdir
  workdir="$(mktemp -d)"
  archive="$workdir/source.tar.gz"
  srcdir="$workdir/source"
  mkdir -p "$srcdir"

  if [[ -z "$ARCHIVE_URL" ]]; then
    ARCHIVE_URL="$(github_archive_url "$REPO_URL" "$REPO_REF")"
  fi

  log "Downloading Mini Panel from $ARCHIVE_URL"
  curl -fsSL "$ARCHIVE_URL" -o "$archive"
  tar -xzf "$archive" -C "$srcdir" --strip-components=1
  printf '%s\n' "$srcdir"
}

main() {
  parse_args "$@"
  require_root
  require_ubuntu
  install_bootstrap_deps

  local srcdir
  srcdir="$(download_source)"
  [[ -f "$srcdir/scripts/uninstall-ubuntu.sh" ]] || die "downloaded source is missing scripts/uninstall-ubuntu.sh"

  log "Running Ubuntu uninstaller"
  bash "$srcdir/scripts/uninstall-ubuntu.sh" "${UNINSTALL_ARGS[@]}"
}

main "$@"
