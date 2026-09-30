#!/usr/bin/env bash
set -euo pipefail

REPO_URL="${REPO_URL:-https://github.com/Gugun09/mini-panel}"
REPO_REF="${REPO_REF:-main}"
RELEASE_VERSION="${RELEASE_VERSION:-latest}"
INSTALL_MODE="${INSTALL_MODE:-release}"
ARCHIVE_URL="${ARCHIVE_URL:-}"
INSTALL_ARGS=()

die() {
  echo "bootstrap: $*" >&2
  exit 1
}

log() {
  echo "==> $*" >&2
}

usage() {
  cat <<'EOF'
Mini Panel one-shot installer

Usage:
  curl -fsSL https://raw.githubusercontent.com/Gugun09/mini-panel/main/install.sh | sudo bash -s -- [options]

Options:
  --repo-url URL          GitHub repository URL. Default is baked into install.sh.
  --ref REF              Git ref to install. Default: main
  --version VERSION      Release version to install, e.g. v0.1.0. Default: latest
  --build-from-source    Build on the VPS instead of downloading a release binary.
  --admin-username USER  Admin username. Default: admin
  --admin-password PASS  Admin password. If omitted, a random password is generated.
  --panel-domain DOMAIN  Optional public domain for the panel UI.
  --app-addr ADDR        Internal listen address. Default: 127.0.0.1:8080
  --help                 Show this help.

Environment alternatives:
  REPO_URL, REPO_REF, RELEASE_VERSION, INSTALL_MODE, ADMIN_USERNAME, ADMIN_PASSWORD, PANEL_DOMAIN, APP_ADDR
EOF
}

require_root() {
  [[ "$(id -u)" -eq 0 ]] || die "run with sudo or as root"
}

require_ubuntu() {
  [[ -r /etc/os-release ]] || die "cannot detect OS"
  # shellcheck source=/dev/null
  . /etc/os-release
  [[ "${ID:-}" == "ubuntu" ]] || die "this installer currently supports Ubuntu only"
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
      --version)
        [[ $# -ge 2 ]] || die "--version requires a value"
        RELEASE_VERSION="$2"
        shift 2
        ;;
      --build-from-source)
        INSTALL_MODE="source"
        shift
        ;;
      --admin-username|--admin-password|--panel-domain|--app-addr)
        [[ $# -ge 2 ]] || die "$1 requires a value"
        INSTALL_ARGS+=("$1" "$2")
        shift 2
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
  [[ "$repo" != *YOUR_GITHUB_USERNAME* ]] || die "set --repo-url https://github.com/Gugun09/mini-panel or edit REPO_URL inside install.sh"
  repo="${repo%.git}"
  repo="${repo#https://github.com/}"
  repo="${repo#http://github.com/}"
  [[ "$repo" != "$REPO_URL" ]] || die "REPO_URL must be a GitHub HTTPS URL or set ARCHIVE_URL"

  if [[ "$ref" == refs/* ]]; then
    printf 'https://codeload.github.com/%s/tar.gz/%s\n' "$repo" "$ref"
  else
    printf 'https://codeload.github.com/%s/tar.gz/refs/heads/%s\n' "$repo" "$ref"
  fi
}

repo_slug() {
  local repo="$1"
  repo="${repo%.git}"
  repo="${repo#https://github.com/}"
  repo="${repo#http://github.com/}"
  [[ "$repo" == */* ]] || die "REPO_URL must be a GitHub HTTPS URL"
  printf '%s\n' "$repo"
}

linux_arch() {
  case "$(uname -m)" in
    x86_64|amd64)
      printf 'amd64\n'
      ;;
    aarch64|arm64)
      printf 'arm64\n'
      ;;
    *)
      die "unsupported CPU architecture: $(uname -m)"
      ;;
  esac
}

release_file_url() {
  local slug="$1"
  local version="$2"
  local file="$3"
  if [[ "$version" == "latest" ]]; then
    printf 'https://github.com/%s/releases/latest/download/%s\n' "$slug" "$file"
  else
    printf 'https://github.com/%s/releases/download/%s/%s\n' "$slug" "$version" "$file"
  fi
}

release_asset_url() {
  local slug="$1"
  local version="$2"
  local arch="$3"
  release_file_url "$slug" "$version" "mini-panel-linux-${arch}.tar.gz"
}

download_release_binary() {
  local workdir archive bindir arch slug url bin asset checksum_url
  workdir="$(mktemp -d)"
  archive="$workdir/mini-panel.tar.gz"
  bindir="$workdir/bin"
  mkdir -p "$bindir"

  slug="$(repo_slug "$REPO_URL")"
  arch="$(linux_arch)"
  asset="mini-panel-linux-${arch}.tar.gz"
  url="$(release_asset_url "$slug" "$RELEASE_VERSION" "$arch")"

  log "Downloading Mini Panel release binary from $url"
  if ! curl -fsSL "$url" -o "$archive"; then
    die "release binary not found. Create a GitHub release tag first, or rerun with --build-from-source"
  fi

  # Verify the tarball against the checksums published with the release.
  # Without this, a compromised release asset would be installed silently.
  checksum_url="$(release_file_url "$slug" "$RELEASE_VERSION" "checksums.txt")"
  log "Verifying checksum from $checksum_url"
  if ! curl -fsSL "$checksum_url" -o "$workdir/checksums.txt"; then
    die "checksum file not found; refusing to install an unverified release binary"
  fi
  if ! awk -v f="$asset" '$2 == f' "$workdir/checksums.txt" | (cd "$workdir" && sha256sum -c - >/dev/null); then
    die "checksum verification failed for $asset; refusing to install"
  fi
  log "Checksum OK"

  tar -xzf "$archive" -C "$bindir"
  bin="$bindir/mini-panel"
  [[ -x "$bin" ]] || die "release archive does not contain executable mini-panel"
  printf '%s\n' "$bin"
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
  local prebuilt_bin=""
  srcdir="$(download_source)"
  [[ -x "$srcdir/scripts/install-ubuntu.sh" || -f "$srcdir/scripts/install-ubuntu.sh" ]] || die "downloaded source is missing scripts/install-ubuntu.sh"

  if [[ "$INSTALL_MODE" == "release" ]]; then
    prebuilt_bin="$(download_release_binary)"
  elif [[ "$INSTALL_MODE" != "source" ]]; then
    die "INSTALL_MODE must be release or source"
  fi

  log "Running Ubuntu installer"
  PREBUILT_BIN="$prebuilt_bin" bash "$srcdir/scripts/install-ubuntu.sh" "${INSTALL_ARGS[@]}"
}

main "$@"
