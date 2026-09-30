package panel

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

type DomainController struct {
	Path    string
	UseSudo bool
}

func (d DomainController) Add(ctx context.Context, domain, upstream string) error {
	return d.run(ctx, "add", domain, upstream)
}

func (d DomainController) CreateStatic(ctx context.Context, domain string) error {
	return d.run(ctx, "create-static", domain)
}

func (d DomainController) Delete(ctx context.Context, domain string) error {
	return d.run(ctx, "delete", domain)
}

func (d DomainController) InstallGo(ctx context.Context, domain, sourcePath string, port int) error {
	return d.run(ctx, "install-go", domain, sourcePath, fmt.Sprintf("%d", port))
}

func (d DomainController) InstallNode(ctx context.Context, domain, gitURL, branch string, port int, installCommand, buildCommand, startCommand string) error {
	return d.run(ctx, "install-node", domain, gitURL, branch, fmt.Sprintf("%d", port), installCommand, buildCommand, startCommand)
}

func (d DomainController) InstallNodeZip(ctx context.Context, domain, sourcePath string, port int, installCommand, buildCommand, startCommand string) error {
	return d.run(ctx, "install-node-zip", domain, sourcePath, fmt.Sprintf("%d", port), installCommand, buildCommand, startCommand)
}

func (d DomainController) InstallZip(ctx context.Context, siteType, domain, sourcePath string) error {
	return d.run(ctx, "install-zip", siteType, domain, sourcePath)
}

func (d DomainController) InstallPHPGit(ctx context.Context, domain, gitURL, branch, publicDir, installCommand string) error {
	return d.run(ctx, "install-php-git", domain, gitURL, branch, publicDir, installCommand)
}

func (d DomainController) IssueSSL(ctx context.Context, domain, email string) error {
	return d.run(ctx, "issue-ssl", domain, email)
}

func (d DomainController) Service(ctx context.Context, action, domain string) error {
	return d.run(ctx, "service", action, domain)
}

func (d DomainController) RunCommand(ctx context.Context, domain, workdir, command string) (string, error) {
	return d.runOutput(ctx, "run-command", domain, workdir, command)
}

func (d DomainController) ApplyEnv(ctx context.Context, domain, sourcePath, mode string) error {
	return d.run(ctx, "apply-env", domain, sourcePath, mode)
}

func (d DomainController) SSLStatus(ctx context.Context, domain string) (string, error) {
	return d.runOutput(ctx, "ssl-status", domain)
}

func (d DomainController) Backup(ctx context.Context, domain string) (string, error) {
	return d.runOutput(ctx, "backup", domain)
}

func (d DomainController) Restore(ctx context.Context, domain, sourcePath string) error {
	return d.run(ctx, "restore", domain, sourcePath)
}

func (d DomainController) DeleteBackup(ctx context.Context, sourcePath string) error {
	return d.run(ctx, "delete-backup", sourcePath)
}

func (d DomainController) CreateCron(ctx context.Context, id int64, domain, schedule, workdir, command string) error {
	return d.run(ctx, "cron-create", fmt.Sprintf("%d", id), domain, schedule, workdir, command)
}

func (d DomainController) DeleteCron(ctx context.Context, id int64, domain string) error {
	return d.run(ctx, "cron-delete", fmt.Sprintf("%d", id), domain)
}

func (d DomainController) CreateWorker(ctx context.Context, id int64, domain, name, workdir, command string) error {
	return d.run(ctx, "worker-create", fmt.Sprintf("%d", id), domain, name, workdir, command)
}

func (d DomainController) WorkerService(ctx context.Context, action string, id int64, domain, name string) error {
	return d.run(ctx, "worker-service", action, fmt.Sprintf("%d", id), domain, name)
}

func (d DomainController) DeleteWorker(ctx context.Context, id int64, domain, name string) error {
	return d.run(ctx, "worker-delete", fmt.Sprintf("%d", id), domain, name)
}

func (d DomainController) PanelUpdate(ctx context.Context, version, repoURL string) error {
	return d.run(ctx, "panel-update", version, repoURL)
}

func (d DomainController) FirewallStatus(ctx context.Context) (string, error) {
	return d.runOutput(ctx, "firewall-status")
}

func (d DomainController) FirewallEnable(ctx context.Context) error {
	return d.run(ctx, "firewall-enable")
}

func (d DomainController) FirewallAllow(ctx context.Context, port int, protocol string) error {
	return d.run(ctx, "firewall-allow", fmt.Sprintf("%d", port), protocol)
}

func (d DomainController) FirewallDelete(ctx context.Context, port int, protocol string) error {
	return d.run(ctx, "firewall-delete", fmt.Sprintf("%d", port), protocol)
}

func (d DomainController) Output(ctx context.Context, args ...string) (string, error) {
	out, err := d.runOutput(ctx, args...)
	return strings.TrimSpace(out), err
}

func (d DomainController) RunOutput(ctx context.Context, args ...string) (string, error) {
	return d.runOutput(ctx, args...)
}

func (d DomainController) run(ctx context.Context, args ...string) error {
	_, err := d.runOutput(ctx, args...)
	return err
}

func (d DomainController) runOutput(ctx context.Context, args ...string) (string, error) {
	if d.Path == "" {
		return "", fmt.Errorf("domainctl path is required")
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()

	name := d.Path
	cmdArgs := args
	if d.UseSudo {
		name = "sudo"
		cmdArgs = append([]string{"-n", d.Path}, args...)
	}

	cmd := exec.CommandContext(ctx, name, cmdArgs...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("domainctl %v failed: %w: %s", args, err, string(out))
	}
	return string(out), nil
}
