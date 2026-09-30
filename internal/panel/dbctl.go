package panel

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

type DBController struct {
	Path    string
	UseSudo bool
}

func (d DBController) Create(ctx context.Context, name, user, password string) error {
	return d.run(ctx, "create", name, user, password)
}

func (d DBController) Delete(ctx context.Context, name, user string) error {
	return d.run(ctx, "delete", name, user)
}

func (d DBController) Backup(ctx context.Context, name, user, password string) (string, error) {
	return d.runOutput(ctx, "backup", name, user, password)
}

func (d DBController) Restore(ctx context.Context, name, user, password, sourcePath string) error {
	return d.run(ctx, "restore", name, user, password, sourcePath)
}

func (d DBController) run(ctx context.Context, args ...string) error {
	_, err := d.runOutput(ctx, args...)
	return err
}

func (d DBController) RunOutput(ctx context.Context, args ...string) (string, error) {
	return d.runOutput(ctx, args...)
}

func (d DBController) runOutput(ctx context.Context, args ...string) (string, error) {
	if d.Path == "" {
		return "", fmt.Errorf("dbctl path is required")
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
		return "", fmt.Errorf("dbctl %v failed: %w: %s", args, err, string(out))
	}
	return strings.TrimSpace(string(out)), nil
}
