#!/usr/bin/env bash
set -euo pipefail

APP_NAME="${APP_NAME:-mini-panel}"
APP_USER="${APP_USER:-mini-panel}"
APP_HOME="${APP_HOME:-/var/lib/mini-panel}"
APP_BIN="${APP_BIN:-/usr/local/bin/mini-panel}"
APP_LIB="${APP_LIB:-/usr/local/lib/mini-panel}"
APP_SHARE="${APP_SHARE:-/usr/local/share/mini-panel}"
APP_ETC="${APP_ETC:-/etc/mini-panel}"
APP_DB="${APP_DB:-$APP_HOME/mini-panel.db}"
APP_UPLOAD_DIR="${APP_UPLOAD_DIR:-$APP_HOME/uploads}"
APP_ROOT_DIR="${APP_ROOT_DIR:-$APP_HOME/apps}"
APP_BACKUP_DIR="${APP_BACKUP_DIR:-$APP_HOME/backups}"
APP_CRON_DIR="${APP_CRON_DIR:-$APP_HOME/cron}"
APP_WORKER_DIR="${APP_WORKER_DIR:-$APP_HOME/workers}"
APP_UPDATE_DIR="${APP_UPDATE_DIR:-$APP_HOME/update}"
APP_LOG_DIR="${APP_LOG_DIR:-/var/log/mini-panel}"
ASSUME_YES="false"
KEEP_DATABASES="false"
KEEP_APPS="false"
KEEP_BACKUPS="false"
REMOVE_PACKAGES="false"

die() {
  echo "uninstall: $*" >&2
  exit 1
}

log() {
  echo "==> $*"
}

warn() {
  echo "WARN: $*" >&2
}

usage() {
  cat <<'EOF'
Mini Panel Ubuntu uninstaller

Usage:
  sudo bash scripts/uninstall-ubuntu.sh --yes [options]

Options:
  --yes               Required for non-interactive purge.
  --keep-databases    Do not drop MariaDB databases/users recorded by Mini Panel.
  --keep-apps         Keep /var/lib/mini-panel/apps.
  --keep-backups      Keep /var/lib/mini-panel/backups.
  --remove-packages   Also apt purge packages installed by the Mini Panel installer.
  --help              Show this help.
EOF
}

parse_args() {
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --yes)
        ASSUME_YES="true"
        shift
        ;;
      --keep-databases)
        KEEP_DATABASES="true"
        shift
        ;;
      --keep-apps)
        KEEP_APPS="true"
        shift
        ;;
      --keep-backups)
        KEEP_BACKUPS="true"
        shift
        ;;
      --remove-packages)
        REMOVE_PACKAGES="true"
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

require_root() {
  [[ "$(id -u)" -eq 0 ]] || die "run as root: sudo bash scripts/uninstall-ubuntu.sh --yes"
}

require_ubuntu() {
  [[ -r /etc/os-release ]] || die "cannot detect OS"
  # shellcheck source=/dev/null
  . /etc/os-release
  [[ "${ID:-}" == "ubuntu" ]] || die "this uninstaller currently supports Ubuntu only"
}

confirm_uninstall() {
  if [[ "$ASSUME_YES" == "true" ]]; then
    return 0
  fi
  if [[ ! -t 0 ]]; then
    die "refusing non-interactive uninstall without --yes"
  fi
  echo "This will purge Mini Panel services, config, app data, backups, Nginx vhosts, cron files, and panel database metadata."
  echo "Type UNINSTALL to continue:"
  local answer
  read -r answer
  [[ "$answer" == "UNINSTALL" ]] || die "confirmation did not match"
}

valid_ident() {
  local value="$1"
  [[ "$value" =~ ^[A-Za-z0-9_]{1,48}$ ]]
}

systemctl_if_available() {
  command -v systemctl >/dev/null 2>&1 || return 0
  systemctl "$@" >/dev/null 2>&1 || true
}

stop_known_services() {
  log "Stopping Mini Panel services"
  systemctl_if_available disable --now mini-panel.service
  systemctl_if_available disable --now mini-panel-adminer.service
  systemctl_if_available disable --now mini-panel-update.service

  local unit unit_name
  for unit in /etc/systemd/system/mini-panel-app-*.service /etc/systemd/system/mini-panel-worker-*.service; do
    [[ -e "$unit" ]] || continue
    if ! grep -Fxq "# managed-by: mini-panel" "$unit"; then
      warn "skipping unmanaged systemd unit: $unit"
      continue
    fi
    unit_name="$(basename "$unit")"
    systemctl_if_available disable --now "$unit_name"
  done
}

remove_systemd_units() {
  log "Removing systemd units"
  local unit
  rm -f /etc/systemd/system/mini-panel.service
  rm -f /etc/systemd/system/mini-panel-adminer.service
  rm -f /etc/systemd/system/mini-panel-update.service
  for unit in /etc/systemd/system/mini-panel-app-*.service /etc/systemd/system/mini-panel-worker-*.service; do
    [[ -e "$unit" ]] || continue
    if grep -Fxq "# managed-by: mini-panel" "$unit"; then
      rm -f "$unit"
    else
      warn "skipping unmanaged systemd unit: $unit"
    fi
  done
  systemctl_if_available daemon-reload
  systemctl_if_available reset-failed
}

remove_cron_files() {
  log "Removing Mini Panel cron files"
  local file
  for file in /etc/cron.d/mini-panel-*; do
    [[ -e "$file" ]] || continue
    if grep -Fxq "# managed-by: mini-panel" "$file"; then
      rm -f "$file"
    else
      warn "skipping unmanaged cron file: $file"
    fi
  done
}

remove_nginx_configs() {
  log "Removing Mini Panel Nginx configs"
  local rollback
  rollback="$(mktemp -d)"

  local file enabled backup_name
  if [[ -f /etc/nginx/sites-available/mini-panel-admin.conf ]]; then
    if grep -Fxq "# managed-by: mini-panel-installer" /etc/nginx/sites-available/mini-panel-admin.conf; then
      cp -a /etc/nginx/sites-available/mini-panel-admin.conf "$rollback/mini-panel-admin.conf"
      rm -f /etc/nginx/sites-enabled/mini-panel-admin.conf
      rm -f /etc/nginx/sites-available/mini-panel-admin.conf
    else
      warn "skipping unmanaged panel Nginx config"
    fi
  fi

  for file in /etc/nginx/sites-available/mini-panel-*.conf; do
    [[ -e "$file" ]] || continue
    if ! grep -Fxq "# managed-by: mini-panel" "$file"; then
      warn "skipping unmanaged Nginx config: $file"
      continue
    fi
    backup_name="$(basename "$file")"
    cp -a "$file" "$rollback/$backup_name"
    enabled="/etc/nginx/sites-enabled/$backup_name"
    rm -f "$enabled"
    rm -f "$file"
  done

  if command -v nginx >/dev/null 2>&1; then
    if ! nginx -t; then
      warn "nginx validation failed after removing Mini Panel configs; restoring removed configs"
      for file in "$rollback"/*.conf; do
        [[ -e "$file" ]] || continue
        install -o root -g root -m 0644 "$file" "/etc/nginx/sites-available/$(basename "$file")"
        ln -sfn "/etc/nginx/sites-available/$(basename "$file")" "/etc/nginx/sites-enabled/$(basename "$file")"
      done
      rm -rf "$rollback"
      die "nginx config test failed"
    fi
    systemctl_if_available reload nginx
  fi
  rm -rf "$rollback"
}

drop_recorded_databases() {
  [[ "$KEEP_DATABASES" != "true" ]] || {
    log "Keeping recorded MariaDB databases"
    return 0
  }
  [[ -f "$APP_DB" ]] || return 0
  command -v sqlite3 >/dev/null 2>&1 || {
    warn "sqlite3 not found; cannot inspect recorded databases"
    return 0
  }
  command -v mariadb >/dev/null 2>&1 || {
    warn "mariadb client not found; cannot drop recorded databases"
    return 0
  }

  log "Dropping MariaDB databases recorded by Mini Panel"
  local rows db_name db_user
  rows="$(sqlite3 -separator '|' "$APP_DB" "SELECT name, username FROM databases;" 2>/dev/null || true)"
  [[ -n "$rows" ]] || return 0
  while IFS='|' read -r db_name db_user; do
    [[ -n "$db_name" && -n "$db_user" ]] || continue
    if ! valid_ident "$db_name" || ! valid_ident "$db_user"; then
      warn "skipping invalid recorded database/user: $db_name/$db_user"
      continue
    fi
    mariadb <<SQL
DROP DATABASE IF EXISTS \`$db_name\`;
DROP USER IF EXISTS '$db_user'@'localhost';
FLUSH PRIVILEGES;
SQL
  done <<<"$rows"
}

remove_files() {
  log "Removing Mini Panel files"
  rm -f "$APP_BIN"
  rm -rf "$APP_LIB"
  rm -rf "$APP_SHARE"
  rm -rf "$APP_ETC"
  rm -rf "$APP_LOG_DIR"

  if [[ "$KEEP_APPS" != "true" && "$KEEP_BACKUPS" != "true" ]]; then
    rm -rf "$APP_HOME"
    return 0
  fi

  rm -f "$APP_DB" "$APP_DB-shm" "$APP_DB-wal"
  rm -rf "$APP_UPLOAD_DIR" "$APP_CRON_DIR" "$APP_WORKER_DIR" "$APP_UPDATE_DIR"
  [[ "$KEEP_APPS" == "true" ]] || rm -rf "$APP_ROOT_DIR"
  [[ "$KEEP_BACKUPS" == "true" ]] || rm -rf "$APP_BACKUP_DIR"
}

remove_user() {
  if [[ "$KEEP_APPS" == "true" || "$KEEP_BACKUPS" == "true" ]]; then
    log "Keeping user $APP_USER because app or backup data was kept"
    return 0
  fi
  if id -u "$APP_USER" >/dev/null 2>&1; then
    log "Removing user $APP_USER"
    userdel "$APP_USER" >/dev/null 2>&1 || warn "could not remove user $APP_USER"
  fi
  if getent group "$APP_USER" >/dev/null 2>&1; then
    groupdel "$APP_USER" >/dev/null 2>&1 || true
  fi
}

remove_sudoers() {
  log "Removing sudoers"
  rm -f /etc/sudoers.d/mini-panel
}

remove_packages() {
  [[ "$REMOVE_PACKAGES" == "true" ]] || return 0
  log "Removing packages installed by Mini Panel"
  apt-get purge -y nginx sqlite3 git cron ufw php-fpm php-cli php-mysql php-mbstring php-xml php-curl php-zip composer certbot python3-certbot-nginx mariadb-server nodejs || true
  apt-get autoremove -y || true
}

print_summary() {
  echo
  echo "Mini Panel uninstalled."
  if [[ "$KEEP_APPS" == "true" ]]; then
    echo "Kept apps: $APP_ROOT_DIR"
  fi
  if [[ "$KEEP_BACKUPS" == "true" ]]; then
    echo "Kept backups: $APP_BACKUP_DIR"
  fi
  if [[ "$KEEP_DATABASES" == "true" ]]; then
    echo "Kept recorded MariaDB databases."
  fi
  echo
}

main() {
  parse_args "$@"
  require_root
  require_ubuntu
  confirm_uninstall
  stop_known_services
  drop_recorded_databases
  remove_cron_files
  remove_nginx_configs
  remove_systemd_units
  remove_sudoers
  remove_files
  remove_user
  remove_packages
  print_summary
}

main "$@"
