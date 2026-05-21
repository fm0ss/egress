package localvpn

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"egress/internal/model"
)

type Result struct {
	Status string `json:"status"`
	Detail string `json:"detail"`
}

type Status struct {
	Connected bool   `json:"connected"`
	Detail    string `json:"detail"`
}

func Connect(ctx context.Context, lease model.Lease) (Result, error) {
	if lease.AccessMode != "vpn" {
		return Result{}, fmt.Errorf("lease %q is not a vpn lease", lease.ID)
	}
	if strings.TrimSpace(lease.Connection.VPNConfig) == "" {
		return Result{}, fmt.Errorf("lease %q has no vpn config", lease.ID)
	}

	tempFile, err := os.CreateTemp("", "egress-wg0-*.conf")
	if err != nil {
		return Result{}, fmt.Errorf("create temp wireguard config: %w", err)
	}
	tempPath := tempFile.Name()
	defer os.Remove(tempPath)
	defer tempFile.Close()

	if _, err := tempFile.WriteString(lease.Connection.VPNConfig); err != nil {
		return Result{}, fmt.Errorf("write temp wireguard config: %w", err)
	}

	configPath, err := stageConfig(tempPath)
	if err != nil {
		return Result{}, err
	}
	defer os.Remove(configPath)

	wrapperPath := wrapperBinaryPath()
	output, runErr := run(ctx, "sudo", "-n", wrapperPath, configPath)
	if runErr != nil {
		return Result{}, fmt.Errorf("sudo -n %s %s: %s", wrapperPath, configPath, strings.TrimSpace(output))
	}

	show, err := run(ctx, "sudo", "-n", "wg", "show")
	if err != nil {
		return Result{Status: "connected", Detail: "wg0 created, but could not verify with wg show"}, nil
	}
	return Result{
		Status: "connected",
		Detail: strings.TrimSpace(show),
	}, nil
}

func Disconnect(ctx context.Context) (Result, error) {
	before := Inspect(ctx)
	output, runErr := run(ctx, "sudo", "-n", disconnectWrapperBinaryPath())
	if runErr != nil {
		return Result{}, fmt.Errorf("sudo -n %s: %s", disconnectWrapperBinaryPath(), strings.TrimSpace(output))
	}
	detail := strings.TrimSpace(output)
	if detail == "" {
		if before.Connected {
			detail = "wg0 disconnected on this machine"
		} else {
			detail = "wg0 was already inactive on this machine"
		}
	}
	return Result{
		Status: "disconnected",
		Detail: detail,
	}, nil
}

func Inspect(ctx context.Context) Status {
	show, err := run(ctx, "wg", "show", "wg0")
	if err != nil {
		return Status{
			Connected: false,
			Detail:    "wg0 is not active on this machine",
		}
	}

	detail := strings.TrimSpace(show)
	if detail == "" {
		return Status{
			Connected: false,
			Detail:    "wg0 exists but no peer status is available",
		}
	}

	return Status{
		Connected: true,
		Detail:    detail,
	}
}

func stageConfig(tempPath string) (string, error) {
	dir := configStageDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create config stage dir: %w", err)
	}
	dst := filepath.Join(dir, "wg0.conf")
	raw, err := os.ReadFile(tempPath)
	if err != nil {
		return "", fmt.Errorf("read staged temp config: %w", err)
	}
	if err := os.WriteFile(dst, raw, 0o600); err != nil {
		return "", fmt.Errorf("write staged config: %w", err)
	}
	return dst, nil
}

func wrapperBinaryPath() string {
	if path := strings.TrimSpace(os.Getenv("EGRESS_WG_APPLY_WRAPPER")); path != "" {
		return path
	}
	return "/usr/local/libexec/egress-apply-wg0"
}

func disconnectWrapperBinaryPath() string {
	if path := strings.TrimSpace(os.Getenv("EGRESS_WG_DOWN_WRAPPER")); path != "" {
		return path
	}
	return "/usr/local/libexec/egress-down-wg0"
}

func configStageDir() string {
	if dir := strings.TrimSpace(os.Getenv("EGRESS_WG_STAGE_DIR")); dir != "" {
		return dir
	}
	return "/var/lib/egressd"
}

func run(ctx context.Context, cmd string, args ...string) (string, error) {
	command := exec.CommandContext(ctx, cmd, args...)
	command.Env = os.Environ()
	output, err := command.CombinedOutput()
	return string(output), err
}
