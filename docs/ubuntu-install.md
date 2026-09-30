# Ubuntu Install Notes

## GitHub one-shot install

Create a GitHub release first. The GitHub Actions workflow builds Linux
`amd64` and `arm64` binaries and attaches them to the release.

```bash
git tag v0.1.0
git push origin v0.1.0
```

After the release workflow finishes, use the root `install.sh` script like a
typical hosting panel installer. The VPS downloads the prebuilt release binary;
it does not build the Go project.

The admin password must be at least 12 characters.

To expose the panel directly on the VPS public IP without HTTPS:

```bash
curl -fsSL https://raw.githubusercontent.com/Gugun09/mini-panel/main/install.sh | sudo bash -s -- \
  --admin-password 'change-this-long-password' \
  --app-addr 0.0.0.0:8080
```

Then open `http://YOUR_VPS_IP:8080/login`. Make sure the VPS firewall or cloud
firewall allows TCP port `8080`.

```bash
curl -fsSL https://raw.githubusercontent.com/Gugun09/mini-panel/main/install.sh | sudo bash -s -- \
  --admin-password 'change-this-long-password'
```

With a public panel domain:

```bash
curl -fsSL https://raw.githubusercontent.com/Gugun09/mini-panel/main/install.sh | sudo bash -s -- \
  --admin-password 'change-this-long-password' \
  --panel-domain panel.example.com
```

Optional: install a specific branch or tag:

```bash
curl -fsSL https://raw.githubusercontent.com/Gugun09/mini-panel/main/install.sh | sudo bash -s -- \
  --ref main \
  --version v0.1.0 \
  --admin-password 'change-this-long-password' \
  --panel-domain panel.example.com
```

For development only, force compiling on the VPS:

```bash
curl -fsSL https://raw.githubusercontent.com/Gugun09/mini-panel/main/install.sh | sudo bash -s -- \
  --build-from-source \
  --admin-password 'change-this-long-password'
```

## Local source install

If this project is still only on your local machine, package and copy it to the
VPS first.

From PowerShell in `D:\mini-panel`:

```powershell
tar --exclude=.dev --exclude=mini-panel-src.tar.gz -czf mini-panel-src.tar.gz .
scp .\mini-panel-src.tar.gz root@YOUR_VPS_IP:/root/
```

Then on the VPS:

```bash
mkdir -p /root/mini-panel
tar -xzf /root/mini-panel-src.tar.gz -C /root/mini-panel
cd /root/mini-panel
```

Run this from the project root on the Ubuntu VPS:

```bash
sudo bash scripts/install-ubuntu.sh --admin-password 'change-this-long-password'
```

If you already have a domain for the panel admin UI:

```bash
sudo bash scripts/install-ubuntu.sh \
  --admin-password 'change-this-long-password' \
  --panel-domain panel.example.com
```

If `PANEL_DOMAIN` is not set, the app listens on `127.0.0.1:8080`. Access it
with an SSH tunnel:

```bash
ssh -L 8080:127.0.0.1:8080 root@YOUR_VPS_IP
```

Then open `http://127.0.0.1:8080/login`.

Make sure the DNS `A` record for `PANEL_DOMAIN` points to the VPS before using
the public panel domain.

The installer does this:

- Installs Nginx, SQLite, sudo, and Go if Go is missing.
- Builds and installs `/usr/local/bin/mini-panel`.
- Installs `/usr/local/lib/mini-panel/domainctl`.
- Creates the `mini-panel` system user and `/var/lib/mini-panel`.
- Writes `/etc/mini-panel/mini-panel.env`.
- Creates `/etc/sudoers.d/mini-panel`.
- Creates and starts `mini-panel.service`.
- Creates the first admin user, unless it already exists.

Useful commands:

```bash
sudo systemctl status mini-panel --no-pager
sudo journalctl -u mini-panel -f
sudo systemctl restart mini-panel
```

## Uninstall

To purge Mini Panel from an Ubuntu VPS:

```bash
curl -fsSL https://raw.githubusercontent.com/Gugun09/mini-panel/main/uninstall.sh | sudo bash -s -- --yes
```

The uninstaller removes Mini Panel systemd services, app/worker services,
managed Nginx vhosts, panel cron files, sudoers, installed binaries/helpers,
panel config, `/var/lib/mini-panel`, and `/var/log/mini-panel`. It also drops
MariaDB databases and users recorded in the Mini Panel SQLite database.

Safety behavior:

- `--yes` is required for non-interactive uninstall.
- Nginx domain configs are removed only when they contain the Mini Panel marker.
- OS packages are not removed by default.

Optional flags:

```bash
sudo bash scripts/uninstall-ubuntu.sh --yes --keep-databases
sudo bash scripts/uninstall-ubuntu.sh --yes --keep-apps --keep-backups
sudo bash scripts/uninstall-ubuntu.sh --yes --remove-packages
```

## Go binary deployment

Upload a compiled Linux binary from the panel using `Add Site`, then choose
runtime `Go` and source `Binary Upload`.

The binary should listen on the port entered in the panel. Recommended pattern
for Go apps:

```go
port := os.Getenv("PORT")
if port == "" {
    port = "9001"
}
http.ListenAndServe(":"+port, handler)
```

Build example:

```bash
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o app .
```

The panel installs it under `/var/lib/mini-panel/apps/<domain>/app`, creates a
`mini-panel-app-<domain>.service` systemd unit, and creates the Nginx reverse
proxy automatically.

## Node and Next.js deployment

Use `Add Site`, then choose runtime `Node.js` and source `Git Repository`.

Typical Next.js values:

```text
Domain: web.example.com
Git URL: https://github.com/user/app.git
Branch: main
Port Aplikasi: 3000
Install Command: npm install
Build Command: npm run build
Start Command: npm start
```

For Next.js, make sure `npm start` respects the `PORT` environment variable:

```json
{
  "scripts": {
    "build": "next build",
    "start": "next start -p $PORT"
  }
}
```

The installer installs Node.js 24.x from NodeSource for modern framework
compatibility.

## Static website deployment

Use `Add Site`, then choose runtime `Static` and source `Blank Site` to create
a normal website domain first. The panel creates
`/var/lib/mini-panel/apps/<domain>/public/index.html`, installs an Nginx static
vhost, and makes the domain available in the `Files` page.

## Static and PHP upload deployment

Use `Add Site`, choose runtime `Static` or `PHP`, then source `Upload ZIP`.

Static example:

```text
Tipe Site: static
Domain: site.example.com
ZIP Project: site.zip
```

PHP example:

```text
Tipe Site: php
Domain: php.example.com
ZIP Project: php-site.zip
```

The ZIP is extracted safely under `/var/lib/mini-panel/apps/<domain>/public`.
The installer includes PHP-FPM and common PHP extensions.

## PHP and Laravel Git deployment

Use `Add Site`, then choose runtime `PHP` and source `Git Repository`.

Laravel example:

```text
Domain: app.example.com
Git URL: https://github.com/user/laravel-app.git
Branch: main
Document Root: public
Install Command: composer install --no-dev --optimize-autoloader
```

The panel clones the repo under `/var/lib/mini-panel/apps/<domain>/source`,
runs Composer if `composer.json` exists, and creates an Nginx PHP-FPM vhost.
Use the `Files` page to edit `.env` after deploy. Database creation is handled
from the `Databases` page. Framework-specific commands such as
`php artisan key:generate` and migrations are still run manually for now.

## SSL

After a domain is active and the DNS `A` record points to the VPS, use the
`SSL` action in the domain table. Enter an email address for Let's Encrypt. The
installer includes Certbot and the Nginx Certbot plugin.

## Domain detail and logs

Click a domain name in `Domains` to open its detail page. It includes Nginx log
lines for that domain. Node/Next.js and Go binary domains also show app logs
from the panel-managed systemd service and provide start, stop, and restart
actions.

## Deploy jobs

Deployments submitted through `Add Site` run as background jobs. The panel
redirects to a job page that shows status and command output. Use the `Jobs`
page to review recent deploy attempts and failures.

## Databases

The installer includes MariaDB. Use the `Databases` page to create/delete a
database and local database user.

```text
Database Name: app_db
Database User: app_user
Password: long-random-password
```

The created user receives privileges only for that database on `localhost`.

## File manager

Use the `Files` page for domains deployed by the panel. It supports:

- Browsing folders inside the managed app directory.
- Editing UTF-8 text files up to 1 MB, including `.env`.
- Uploading files up to 32 MB.
- Creating folders.
- Deleting files or empty folders.

The file manager is intentionally scoped to
`/var/lib/mini-panel/apps/<domain>` and its framework-specific subfolders. It
does not expose arbitrary VPS paths.

## Manual install

These commands assume the built binary is copied to `/usr/local/bin/mini-panel`
and the service user is named `mini-panel`.

```bash
sudo apt update
sudo apt install -y nginx sqlite3

sudo useradd --system --home /var/lib/mini-panel --create-home --shell /usr/sbin/nologin mini-panel
sudo install -d -o mini-panel -g mini-panel /var/lib/mini-panel
sudo install -d -o root -g root -m 0755 /usr/local/lib/mini-panel

sudo install -o root -g root -m 0755 scripts/domainctl /usr/local/lib/mini-panel/domainctl
sudo tee /etc/sudoers.d/mini-panel >/dev/null <<'EOF'
mini-panel ALL=(root) NOPASSWD: /usr/local/lib/mini-panel/domainctl add *, /usr/local/lib/mini-panel/domainctl delete *
EOF
sudo chmod 0440 /etc/sudoers.d/mini-panel
sudo visudo -cf /etc/sudoers.d/mini-panel

sudo -u mini-panel MINIPANEL_ADMIN_PASSWORD='change-this-long-password' \
  /usr/local/bin/mini-panel create-admin \
  -db /var/lib/mini-panel/mini-panel.db \
  -username admin
```

Example systemd service:

```ini
[Unit]
Description=Mini Panel
After=network.target nginx.service

[Service]
User=mini-panel
Group=mini-panel
Environment=MINIPANEL_DB=/var/lib/mini-panel/mini-panel.db
Environment=MINIPANEL_ADDR=127.0.0.1:8080
Environment=MINIPANEL_SESSION_KEY=replace-with-32-random-bytes-or-longer
ExecStart=/usr/local/bin/mini-panel serve
Restart=on-failure

[Install]
WantedBy=multi-user.target
```

Expose the panel through an Nginx reverse proxy and HTTPS before opening it to
the public internet.
