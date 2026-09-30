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
APP_ADDR="${APP_ADDR:-127.0.0.1:8080}"
APP_ADMINER_ADDR="${APP_ADMINER_ADDR:-127.0.0.1:8079}"
APP_ADMINER_URL="${APP_ADMINER_URL:-http://$APP_ADMINER_ADDR}"
APP_UPLOAD_DIR="${APP_UPLOAD_DIR:-$APP_HOME/uploads}"
APP_ROOT_DIR="${APP_ROOT_DIR:-$APP_HOME/apps}"
ADMIN_USERNAME="${ADMIN_USERNAME:-admin}"
ADMIN_PASSWORD="${ADMIN_PASSWORD:-${MINIPANEL_ADMIN_PASSWORD:-}}"
PANEL_DOMAIN="${PANEL_DOMAIN:-}"
PREBUILT_BIN="${PREBUILT_BIN:-}"

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

die() {
  echo "install: $*" >&2
  exit 1
}

log() {
  echo "==> $*"
}

usage() {
  cat <<'EOF'
Mini Panel Ubuntu installer

Usage:
  sudo bash scripts/install-ubuntu.sh [options]

Options:
  --admin-username USER  Admin username. Default: admin
  --admin-password PASS  Admin password. If omitted, a random password is generated.
  --panel-domain DOMAIN  Optional public domain for the panel UI.
  --app-addr ADDR        Internal listen address. Default: 127.0.0.1:8080
  --prebuilt-bin PATH    Install this compiled mini-panel binary instead of building.
  --help                 Show this help.
EOF
}

parse_args() {
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --admin-username)
        [[ $# -ge 2 ]] || die "--admin-username requires a value"
        ADMIN_USERNAME="$2"
        shift 2
        ;;
      --admin-password)
        [[ $# -ge 2 ]] || die "--admin-password requires a value"
        ADMIN_PASSWORD="$2"
        shift 2
        ;;
      --panel-domain)
        [[ $# -ge 2 ]] || die "--panel-domain requires a value"
        PANEL_DOMAIN="$2"
        shift 2
        ;;
      --app-addr)
        [[ $# -ge 2 ]] || die "--app-addr requires a value"
        APP_ADDR="$2"
        shift 2
        ;;
      --prebuilt-bin)
        [[ $# -ge 2 ]] || die "--prebuilt-bin requires a value"
        PREBUILT_BIN="$2"
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

require_root() {
  [[ "$(id -u)" -eq 0 ]] || die "run as root: sudo bash scripts/install-ubuntu.sh"
}

require_ubuntu() {
  [[ -r /etc/os-release ]] || die "cannot detect OS"
  # shellcheck source=/dev/null
  . /etc/os-release
  [[ "${ID:-}" == "ubuntu" ]] || die "this installer currently supports Ubuntu only"
}

random_secret() {
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -base64 48
  else
    od -An -N48 -tx1 /dev/urandom | tr -d ' \n'
    echo
  fi
}

valid_domain() {
  local domain="$1"
  [[ ${#domain} -le 253 ]] || return 1
  [[ "$domain" == *.* ]] || return 1
  [[ "$domain" =~ ^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+$ ]]
}

validate_inputs() {
	[[ "$ADMIN_USERNAME" =~ ^[A-Za-z0-9_.-]+$ ]] || die "ADMIN_USERNAME may only contain letters, numbers, dot, underscore, and hyphen"
	[[ "$APP_ADDR" =~ ^(127\.0\.0\.1|0\.0\.0\.0):[1-9][0-9]{0,4}$ ]] || die "APP_ADDR must look like 127.0.0.1:<port> or 0.0.0.0:<port>"
	if [[ -n "$ADMIN_PASSWORD" && ${#ADMIN_PASSWORD} -lt 12 ]]; then
		die "admin password must be at least 12 characters"
	fi
	if [[ -n "$PANEL_DOMAIN" ]]; then
		PANEL_DOMAIN="${PANEL_DOMAIN,,}"
		valid_domain "$PANEL_DOMAIN" || die "PANEL_DOMAIN must be a valid lowercase hostname like panel.example.com"
	fi
}

install_packages() {
  log "Installing packages"
  apt-get update
  apt-get install -y ca-certificates curl gnupg nginx sqlite3 sudo git cron ufw python3 php-fpm php-cli php-mysql php-mbstring php-xml php-curl php-zip composer certbot python3-certbot-nginx mariadb-server
  install_nodejs
  if [[ -z "$PREBUILT_BIN" ]] && ! command -v go >/dev/null 2>&1; then
    apt-get install -y golang-go
  fi
}

install_nodejs() {
  local major="0"
  if command -v node >/dev/null 2>&1; then
    major="$(node -v | sed -E 's/^v([0-9]+).*/\1/')"
  fi
  if [[ "$major" =~ ^[0-9]+$ && "$major" -ge 24 ]] && command -v npm >/dev/null 2>&1; then
    return
  fi
  log "Installing Node.js 24.x"
  curl -fsSL https://deb.nodesource.com/setup_24.x | bash -
  apt-get install -y nodejs
}

install_user_and_dirs() {
  log "Creating service user and directories"
  if ! id -u "$APP_USER" >/dev/null 2>&1; then
    useradd --system --home "$APP_HOME" --create-home --shell /usr/sbin/nologin "$APP_USER"
  fi
  install -d -o "$APP_USER" -g "$APP_USER" -m 0750 "$APP_HOME"
  install -d -o "$APP_USER" -g "$APP_USER" -m 0750 "$APP_UPLOAD_DIR"
  install -d -o "$APP_USER" -g "$APP_USER" -m 0750 "$APP_ROOT_DIR"
  install -d -o root -g root -m 0755 "$APP_LIB"
  install -d -o root -g root -m 0755 "$APP_SHARE"
  install -d -o root -g root -m 0750 "$APP_ETC"
  install -d -o root -g "$APP_USER" -m 0750 "$APP_ETC/apps"
  if id -u www-data >/dev/null 2>&1; then
    usermod -a -G "$APP_USER" www-data
  fi
}

install_binary() {
  log "Installing mini-panel binary"
  local tmp_bin
  tmp_bin="$(mktemp)"

  if [[ -n "$PREBUILT_BIN" ]]; then
    [[ -x "$PREBUILT_BIN" ]] || die "prebuilt binary is not executable: $PREBUILT_BIN"
    cp "$PREBUILT_BIN" "$tmp_bin"
  elif [[ -x "$ROOT_DIR/mini-panel" ]]; then
    cp "$ROOT_DIR/mini-panel" "$tmp_bin"
  elif [[ -f "$ROOT_DIR/cmd/mini-panel/main.go" ]]; then
    command -v go >/dev/null 2>&1 || die "Go is required to build from source"
    (cd "$ROOT_DIR" && go build -trimpath -ldflags="-s -w" -o "$tmp_bin" ./cmd/mini-panel)
  else
    die "expected prebuilt binary, source tree, or executable ./mini-panel"
  fi

  install -o root -g root -m 0755 "$tmp_bin" "$APP_BIN"
  rm -f "$tmp_bin"
}

install_domainctl() {
  log "Installing Nginx domain helper"
  [[ -f "$ROOT_DIR/scripts/domainctl" ]] || die "scripts/domainctl not found"
  [[ -f "$ROOT_DIR/scripts/dbctl" ]] || die "scripts/dbctl not found"
  install -o root -g root -m 0755 "$ROOT_DIR/scripts/domainctl" "$APP_LIB/domainctl"
  install -o root -g root -m 0755 "$ROOT_DIR/scripts/dbctl" "$APP_LIB/dbctl"

  cat >/etc/sudoers.d/mini-panel <<EOF
$APP_USER ALL=(root) NOPASSWD: $APP_LIB/domainctl add *, $APP_LIB/domainctl delete *
$APP_USER ALL=(root) NOPASSWD: $APP_LIB/domainctl create-static *
$APP_USER ALL=(root) NOPASSWD: $APP_LIB/domainctl install-go *
$APP_USER ALL=(root) NOPASSWD: $APP_LIB/domainctl install-node *
$APP_USER ALL=(root) NOPASSWD: $APP_LIB/domainctl install-node-zip *
$APP_USER ALL=(root) NOPASSWD: $APP_LIB/domainctl install-zip *
$APP_USER ALL=(root) NOPASSWD: $APP_LIB/domainctl install-php-git *
$APP_USER ALL=(root) NOPASSWD: $APP_LIB/domainctl issue-ssl *
$APP_USER ALL=(root) NOPASSWD: $APP_LIB/domainctl ssl-status *
$APP_USER ALL=(root) NOPASSWD: $APP_LIB/domainctl service *
$APP_USER ALL=(root) NOPASSWD: $APP_LIB/domainctl run-command *
$APP_USER ALL=(root) NOPASSWD: $APP_LIB/domainctl apply-env *
$APP_USER ALL=(root) NOPASSWD: $APP_LIB/domainctl backup *
$APP_USER ALL=(root) NOPASSWD: $APP_LIB/domainctl restore *
$APP_USER ALL=(root) NOPASSWD: $APP_LIB/domainctl delete-backup *
$APP_USER ALL=(root) NOPASSWD: $APP_LIB/domainctl cron-create *
$APP_USER ALL=(root) NOPASSWD: $APP_LIB/domainctl cron-delete *
$APP_USER ALL=(root) NOPASSWD: $APP_LIB/domainctl worker-create *
$APP_USER ALL=(root) NOPASSWD: $APP_LIB/domainctl worker-service *
$APP_USER ALL=(root) NOPASSWD: $APP_LIB/domainctl worker-delete *
$APP_USER ALL=(root) NOPASSWD: $APP_LIB/domainctl panel-update *
$APP_USER ALL=(root) NOPASSWD: $APP_LIB/domainctl firewall-status
$APP_USER ALL=(root) NOPASSWD: $APP_LIB/domainctl firewall-enable
$APP_USER ALL=(root) NOPASSWD: $APP_LIB/domainctl firewall-allow *
$APP_USER ALL=(root) NOPASSWD: $APP_LIB/domainctl firewall-delete *
$APP_USER ALL=(root) NOPASSWD: $APP_LIB/domainctl logs *
$APP_USER ALL=(root) NOPASSWD: $APP_LIB/dbctl create *, $APP_LIB/dbctl delete *, $APP_LIB/dbctl backup *, $APP_LIB/dbctl restore *
EOF
  chmod 0440 /etc/sudoers.d/mini-panel
  visudo -cf /etc/sudoers.d/mini-panel >/dev/null
}

install_adminer() {
  log "Installing Adminer"
  local tmp_adminer
  tmp_adminer="$(mktemp)"
  curl -fsSL "https://www.adminer.org/latest-mysql-en.php" -o "$tmp_adminer"
  grep -qi "adminer" "$tmp_adminer" || die "downloaded Adminer file does not look valid"
  install -o root -g root -m 0644 "$tmp_adminer" "$APP_SHARE/adminer.php"
  rm -f "$tmp_adminer"

  cat >/etc/systemd/system/mini-panel-adminer.service <<EOF
[Unit]
Description=Mini Panel Adminer
After=network.target mariadb.service

[Service]
User=$APP_USER
Group=$APP_USER
WorkingDirectory=$APP_SHARE
ExecStart=/usr/bin/php -S $APP_ADMINER_ADDR $APP_SHARE/adminer.php
Restart=on-failure
RestartSec=3
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=full
ProtectHome=true

[Install]
WantedBy=multi-user.target
EOF

  systemctl daemon-reload
  systemctl enable mini-panel-adminer >/dev/null
}

install_environment() {
  log "Writing environment file"
  local session_key secure_cookie
  session_key="${MINIPANEL_SESSION_KEY:-$(random_secret)}"
  # The panel is served over HTTPS through Nginx when a panel domain is set,
  # so the session cookie can (and should) carry the Secure flag. Direct
  # IP:port installs stay on plain HTTP, where Secure would break login.
  secure_cookie="false"
  [[ -n "${PANEL_DOMAIN:-}" ]] && secure_cookie="true"

  cat >"$APP_ETC/mini-panel.env" <<EOF
MINIPANEL_DB=$APP_DB
MINIPANEL_ADDR=$APP_ADDR
MINIPANEL_DOMAINCTL=$APP_LIB/domainctl
MINIPANEL_DBCTL=$APP_LIB/dbctl
MINIPANEL_DOMAINCTL_SUDO=true
MINIPANEL_SECURE_COOKIE=$secure_cookie
MINIPANEL_SESSION_KEY=$session_key
MINIPANEL_UPLOAD_DIR=$APP_UPLOAD_DIR
MINIPANEL_APP_ROOT=$APP_ROOT_DIR
MINIPANEL_ADMINER_URL=$APP_ADMINER_URL
EOF
  chown root:root "$APP_ETC/mini-panel.env"
  chmod 0600 "$APP_ETC/mini-panel.env"
}

install_service() {
  log "Installing systemd service"
  cat >/etc/systemd/system/mini-panel.service <<EOF
[Unit]
Description=Mini Panel
After=network.target nginx.service

[Service]
User=$APP_USER
Group=$APP_USER
EnvironmentFile=$APP_ETC/mini-panel.env
ExecStart=$APP_BIN serve
Restart=on-failure
RestartSec=3

[Install]
WantedBy=multi-user.target
EOF

  systemctl daemon-reload
  systemctl enable mini-panel >/dev/null
}

create_admin() {
  log "Ensuring admin account exists"
  if [[ -f "$APP_DB" ]] && sqlite3 "$APP_DB" "SELECT 1 FROM admins WHERE username = '$ADMIN_USERNAME' LIMIT 1;" 2>/dev/null | grep -q 1; then
    echo "Admin '$ADMIN_USERNAME' already exists; skipping password creation."
    return
  fi

  if [[ -z "$ADMIN_PASSWORD" ]]; then
    ADMIN_PASSWORD="$(random_secret)"
    GENERATED_ADMIN_PASSWORD="true"
  else
    GENERATED_ADMIN_PASSWORD="false"
  fi

  install -d -o "$APP_USER" -g "$APP_USER" -m 0750 "$(dirname "$APP_DB")"
  sudo -u "$APP_USER" MINIPANEL_ADMIN_PASSWORD="$ADMIN_PASSWORD" "$APP_BIN" create-admin \
    -db "$APP_DB" \
    -username "$ADMIN_USERNAME"
}

secure_database_files() {
  for db_file in "$APP_DB" "$APP_DB-shm" "$APP_DB-wal"; do
    if [[ -e "$db_file" ]]; then
      chown "$APP_USER:$APP_USER" "$db_file"
      chmod 0600 "$db_file"
    fi
  done
}

install_panel_nginx() {
  [[ -n "$PANEL_DOMAIN" ]] || return 0
  log "Installing Nginx reverse proxy for $PANEL_DOMAIN"

  local conf="/etc/nginx/sites-available/mini-panel-admin.conf"
  cat >"$conf" <<EOF
# managed-by: mini-panel-installer
server {
    listen 80;
    listen [::]:80;
    server_name $PANEL_DOMAIN;

    location / {
        proxy_pass http://$APP_ADDR;
        proxy_http_version 1.1;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
    }
}
EOF

  ln -sfn "$conf" /etc/nginx/sites-enabled/mini-panel-admin.conf
  nginx -t
}

start_services() {
  log "Starting services"
  nginx -t
  systemctl enable --now cron
  systemctl restart mini-panel-adminer
  systemctl restart nginx
  systemctl restart mini-panel
}

print_summary() {
  echo
  echo "Mini Panel installed."
  echo "Service: systemctl status mini-panel --no-pager"
  echo "Local URL: http://$APP_ADDR/login"
  if [[ -n "$PANEL_DOMAIN" ]]; then
    echo "Panel URL: http://$PANEL_DOMAIN/login"
  else
    echo "SSH tunnel: ssh -L 8080:$APP_ADDR root@YOUR_VPS_IP"
  fi
  echo "Username: $ADMIN_USERNAME"
  if [[ "${GENERATED_ADMIN_PASSWORD:-false}" == "true" ]]; then
    echo "Generated password: $ADMIN_PASSWORD"
  else
    echo "Password: the ADMIN_PASSWORD you provided"
  fi
  echo
}

main() {
  parse_args "$@"
  require_root
  require_ubuntu
  validate_inputs
  install_packages
  install_user_and_dirs
  install_binary
  install_domainctl
  install_adminer
  install_environment
  install_service
  create_admin
  secure_database_files
  install_panel_nginx
  start_services
  print_summary
}

main "$@"
