package awscli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

var ErrUnavailable = errors.New("aws cli not installed")

type ExportedCredentials struct {
	AccessKeyID     string `json:"AccessKeyId"`
	SecretAccessKey string `json:"SecretAccessKey"`
	SessionToken    string `json:"SessionToken"`
	Expiration      string `json:"Expiration"`
}

type CallerIdentity struct {
	Account string `json:"Account"`
	ARN     string `json:"Arn"`
	UserID  string `json:"UserId"`
}

type ImportResult struct {
	Profile     string
	Identity    CallerIdentity
	Credentials ExportedCredentials
	ExportEnv   string
}

func IsUnavailable(err error) bool {
	return errors.Is(err, ErrUnavailable)
}

func ListProfiles(ctx context.Context) ([]string, error) {
	output, err := run(ctx, "configure", "list-profiles")
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(output), "\n")
	profiles := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			profiles = append(profiles, line)
		}
	}
	return profiles, nil
}

func ImportProfile(ctx context.Context, profile string) (ImportResult, error) {
	var result ImportResult
	profile = strings.TrimSpace(profile)
	if profile == "" {
		return result, fmt.Errorf("aws profile is required")
	}

	identityRaw, err := run(ctx, "sts", "get-caller-identity", "--profile", profile, "--output", "json")
	if err != nil {
		return result, err
	}
	if err := json.Unmarshal([]byte(identityRaw), &result.Identity); err != nil {
		return result, fmt.Errorf("parse caller identity: %w", err)
	}

	credProcessRaw, err := run(ctx, "configure", "export-credentials", "--profile", profile, "--format", "process")
	if err != nil {
		return result, err
	}
	if err := json.Unmarshal([]byte(credProcessRaw), &result.Credentials); err != nil {
		return result, fmt.Errorf("parse exported credentials: %w", err)
	}

	envRaw, err := run(ctx, "configure", "export-credentials", "--profile", profile, "--format", "env")
	if err != nil {
		return result, err
	}

	result.Profile = profile
	result.ExportEnv = strings.TrimSpace(envRaw)
	return result, nil
}

func DefaultTimeoutContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 5*time.Second)
}

func run(ctx context.Context, args ...string) (string, error) {
	binary, err := findBinary()
	if err != nil {
		return "", err
	}
	return Run(ctx, binary, args...)
}

func Run(ctx context.Context, binary string, args ...string) (string, error) {
	if len(args) == 0 || args[0] != "--no-cli-pager" {
		args = append([]string{"--no-cli-pager"}, args...)
	}
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Env = append(os.Environ(), "AWS_PAGER=", "PAGER=")
	output, err := cmd.CombinedOutput()
	if err != nil {
		var notFound *exec.Error
		if errors.As(err, &notFound) {
			return "", ErrUnavailable
		}
		return "", fmt.Errorf("aws %s: %s", strings.Join(args, " "), strings.TrimSpace(string(output)))
	}
	return string(output), nil
}

func findBinary() (string, error) {
	if configured := strings.TrimSpace(os.Getenv("EGRESS_AWS_CLI")); configured != "" {
		if _, err := os.Stat(configured); err == nil {
			return configured, nil
		}
	}
	if bundled := filepath.Join(".", "aws", "dist", "aws"); fileExists(bundled) {
		return bundled, nil
	}
	binary, err := exec.LookPath("aws")
	if err == nil {
		return binary, nil
	}
	return "", ErrUnavailable
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
