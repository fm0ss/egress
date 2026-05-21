package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"strings"
	"time"

	"egress/internal/awscli"
	"egress/internal/control"
	"egress/internal/localvpn"
	"egress/internal/model"
	"egress/internal/server"
)

const defaultStatePath = ".egress/state.json"

func Run(args []string) error {
	if len(args) == 0 {
		return usageError("")
	}

	switch args[0] {
	case "locations":
		return runLocations(args[1:])
	case "accounts":
		return runAccounts(args[1:])
	case "import-aws-cli":
		return runImportAWSCLI(args[1:])
	case "plan":
		return runPlan(args[1:])
	case "apply":
		return runApply(args[1:])
	case "attach":
		return runAttach(args[1:])
	case "lease":
		return runLease(args[1:])
	case "provision":
		return runProvision(args[1:])
	case "connect-local":
		return runConnectLocal(args[1:])
	case "disconnect-local":
		return runDisconnectLocal(args[1:])
	case "local-status":
		return runLocalStatus(args[1:])
	case "destroy-lease":
		return runDestroyLease(args[1:])
	case "cleanup-all":
		return runCleanupAll(args[1:])
	case "state":
		return runState(args[1:])
	case "serve":
		return runServe(args[1:])
	case "help":
		return usageError("")
	default:
		return usageError(fmt.Sprintf("unknown command %q", args[0]))
	}
}

func runLocations(args []string) error {
	fs := flag.NewFlagSet("locations", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}

	payload, err := json.MarshalIndent(control.SupportedLocations(), "", "  ")
	if err != nil {
		return err
	}
	fmt.Printf("%s\n", payload)
	return nil
}

func runAccounts(args []string) error {
	fs := flag.NewFlagSet("accounts", flag.ContinueOnError)
	statePath := fs.String("state", defaultStatePath, "path to state file")
	if err := fs.Parse(args); err != nil {
		return err
	}

	state, err := control.LoadState(*statePath)
	if err != nil {
		return err
	}
	payload, err := json.MarshalIndent(state.Accounts, "", "  ")
	if err != nil {
		return err
	}
	fmt.Printf("%s\n", payload)
	return nil
}

func runImportAWSCLI(args []string) error {
	fs := flag.NewFlagSet("import-aws-cli", flag.ContinueOnError)
	profile := fs.String("profile", "", "aws profile name")
	name := fs.String("name", "", "saved account name")
	statePath := fs.String("state", defaultStatePath, "path to state file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *profile == "" {
		return fmt.Errorf("import-aws-cli requires -profile")
	}

	ctx, cancel := awscli.DefaultTimeoutContext()
	defer cancel()
	imported, err := awscli.ImportProfile(ctx, *profile)
	if err != nil {
		return err
	}

	accountName := strings.TrimSpace(*name)
	if accountName == "" {
		accountName = imported.Profile
	}
	account := model.CloudAccount{
		Name:             accountName,
		Provider:         "aws",
		AWSAccountID:     imported.Identity.Account,
		AWSProfile:       imported.Profile,
		PrincipalARN:     imported.Identity.ARN,
		CredentialSource: "aws_cli",
		Status:           "connected",
		LastVerifiedAt:   time.Now().UTC(),
	}

	state, err := control.LoadState(*statePath)
	if err != nil {
		return err
	}
	next, saved, action, err := control.UpsertCloudAccount(state, account, time.Now().UTC())
	if err != nil {
		return err
	}
	if err := control.SaveState(*statePath, next); err != nil {
		return err
	}

	payload := map[string]any{
		"status":        action,
		"account":       saved,
		"principal_arn": imported.Identity.ARN,
		"expiration":    imported.Credentials.Expiration,
		"export_env":    imported.ExportEnv,
	}
	raw, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	fmt.Printf("%s\n", raw)
	return nil
}

func runPlan(args []string) error {
	fs := flag.NewFlagSet("plan", flag.ContinueOnError)
	configPath := fs.String("f", "", "path to config file")
	statePath := fs.String("state", defaultStatePath, "path to state file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *configPath == "" {
		return fmt.Errorf("plan requires -f")
	}

	cfg, err := control.LoadConfig(*configPath)
	if err != nil {
		return err
	}
	state, err := control.LoadState(*statePath)
	if err != nil {
		return err
	}

	plan := control.Plan(cfg, state)
	fmt.Print(formatPlan(plan))
	return nil
}

func runApply(args []string) error {
	fs := flag.NewFlagSet("apply", flag.ContinueOnError)
	configPath := fs.String("f", "", "path to config file")
	statePath := fs.String("state", defaultStatePath, "path to state file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *configPath == "" {
		return fmt.Errorf("apply requires -f")
	}

	cfg, err := control.LoadConfig(*configPath)
	if err != nil {
		return err
	}
	state, err := control.LoadState(*statePath)
	if err != nil {
		return err
	}

	plan := control.Plan(cfg, state)
	next := control.Apply(cfg, state, time.Now().UTC())
	if err := control.SaveState(*statePath, next); err != nil {
		return err
	}

	fmt.Print(formatPlan(plan))
	fmt.Printf("Applied %d policy resources.\n", len(next.Policies))
	return nil
}

func runAttach(args []string) error {
	fs := flag.NewFlagSet("attach", flag.ContinueOnError)
	workloadID := fs.String("workload", "", "workload identifier")
	policyRef := fs.String("policy", "", "policy name or id")
	statePath := fs.String("state", defaultStatePath, "path to state file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *workloadID == "" || *policyRef == "" {
		return fmt.Errorf("attach requires -workload and -policy")
	}

	state, err := control.LoadState(*statePath)
	if err != nil {
		return err
	}
	next, attachment, err := control.Attach(state, *workloadID, *policyRef, time.Now().UTC())
	if err != nil {
		return err
	}
	if err := control.SaveState(*statePath, next); err != nil {
		return err
	}

	fmt.Printf("Attached workload %q to policy %q.\n", attachment.WorkloadID, attachment.PolicyID)
	return nil
}

func runLease(args []string) error {
	fs := flag.NewFlagSet("lease", flag.ContinueOnError)
	workloadID := fs.String("workload", "", "workload identifier")
	accessMode := fs.String("access-mode", "proxy", "connection type: proxy or vpn")
	statePath := fs.String("state", defaultStatePath, "path to state file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *workloadID == "" {
		return fmt.Errorf("lease requires -workload")
	}

	state, err := control.LoadState(*statePath)
	if err != nil {
		return err
	}
	next, lease, err := control.Lease(state, *workloadID, *accessMode, time.Now().UTC())
	if err != nil {
		return err
	}
	if err := control.SaveState(*statePath, next); err != nil {
		return err
	}

	payload, err := json.MarshalIndent(lease, "", "  ")
	if err != nil {
		return err
	}
	fmt.Printf("%s\n", payload)
	return nil
}

func runProvision(args []string) error {
	fs := flag.NewFlagSet("provision", flag.ContinueOnError)
	accountRef := fs.String("account", "", "account id, name, or aws profile")
	locationID := fs.String("location", "", "location id or region")
	accessMode := fs.String("access-mode", "proxy", "connection type: proxy or vpn")
	workloadID := fs.String("workload", "", "workload identifier; generated if omitted")
	statePath := fs.String("state", defaultStatePath, "path to state file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *locationID == "" {
		return fmt.Errorf("provision requires -location")
	}

	state, err := control.LoadState(*statePath)
	if err != nil {
		return err
	}
	next, lease, err := control.ProvisionAccess(state, *accountRef, *locationID, *accessMode, *workloadID, time.Now().UTC())
	if err != nil {
		return err
	}
	if err := control.SaveState(*statePath, next); err != nil {
		return err
	}

	payload, err := json.MarshalIndent(lease, "", "  ")
	if err != nil {
		return err
	}
	fmt.Printf("%s\n", payload)
	return nil
}

func runConnectLocal(args []string) error {
	fs := flag.NewFlagSet("connect-local", flag.ContinueOnError)
	leaseID := fs.String("lease", "", "lease id to connect")
	useLatest := fs.Bool("latest", false, "connect the most recent vpn lease")
	statePath := fs.String("state", defaultStatePath, "path to state file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *leaseID == "" && !*useLatest {
		return fmt.Errorf("connect-local requires -lease or -latest")
	}

	state, err := control.LoadState(*statePath)
	if err != nil {
		return err
	}
	lease, err := findLease(state, *leaseID, *useLatest)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	result, err := localvpn.Connect(ctx, lease)
	if err != nil {
		return err
	}
	payload, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	fmt.Printf("%s\n", payload)
	return nil
}

func runDisconnectLocal(args []string) error {
	fs := flag.NewFlagSet("disconnect-local", flag.ContinueOnError)
	leaseID := fs.String("lease", "", "lease id to disconnect and clean up")
	useLatest := fs.Bool("latest", false, "disconnect and clean up the most recent vpn lease")
	statePath := fs.String("state", defaultStatePath, "path to state file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	result, err := localvpn.Disconnect(ctx)
	if err != nil {
		return err
	}
	payload := map[string]any{"local": result}

	if *leaseID != "" || *useLatest {
		state, err := control.LoadState(*statePath)
		if err != nil {
			return err
		}
		lease, err := findLease(state, *leaseID, *useLatest)
		if err != nil {
			return err
		}
		next, cleanup, err := control.DestroyLease(state, lease.ID, time.Now().UTC())
		if err != nil {
			return err
		}
		if err := control.SaveState(*statePath, next); err != nil {
			return err
		}
		payload["cleanup"] = cleanup
	}
	raw, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	fmt.Printf("%s\n", raw)
	return nil
}

func runDestroyLease(args []string) error {
	fs := flag.NewFlagSet("destroy-lease", flag.ContinueOnError)
	leaseID := fs.String("lease", "", "lease id to destroy")
	statePath := fs.String("state", defaultStatePath, "path to state file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *leaseID == "" {
		return fmt.Errorf("destroy-lease requires -lease")
	}
	state, err := control.LoadState(*statePath)
	if err != nil {
		return err
	}
	next, result, err := control.DestroyLease(state, *leaseID, time.Now().UTC())
	if err != nil {
		return err
	}
	if err := control.SaveState(*statePath, next); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	fmt.Printf("%s\n", raw)
	return nil
}

func runCleanupAll(args []string) error {
	fs := flag.NewFlagSet("cleanup-all", flag.ContinueOnError)
	statePath := fs.String("state", defaultStatePath, "path to state file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	state, err := control.LoadState(*statePath)
	if err != nil {
		return err
	}
	next, result, err := control.CleanupAllResources(state, time.Now().UTC())
	if err != nil {
		return err
	}
	if err := control.SaveState(*statePath, next); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	fmt.Printf("%s\n", raw)
	return nil
}

func runLocalStatus(args []string) error {
	fs := flag.NewFlagSet("local-status", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	status := localvpn.Inspect(ctx)
	payload, err := json.MarshalIndent(status, "", "  ")
	if err != nil {
		return err
	}
	fmt.Printf("%s\n", payload)
	return nil
}

func runState(args []string) error {
	fs := flag.NewFlagSet("state", flag.ContinueOnError)
	statePath := fs.String("state", defaultStatePath, "path to state file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	state, err := control.LoadState(*statePath)
	if err != nil {
		return err
	}
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	fmt.Printf("%s\n", raw)
	return nil
}

func runServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	statePath := fs.String("state", defaultStatePath, "path to state file")
	addr := fs.String("addr", "127.0.0.1:8080", "address to bind the http server")
	if err := fs.Parse(args); err != nil {
		return err
	}

	srv := server.New(server.Config{
		Addr:      *addr,
		StatePath: *statePath,
	})
	fmt.Printf("Serving Egress Console on http://%s\n", *addr)
	return srv.ListenAndServe()
}

func findLease(state model.State, leaseID string, useLatest bool) (model.Lease, error) {
	if leaseID != "" {
		for _, lease := range state.Leases {
			if lease.ID == leaseID {
				return lease, nil
			}
		}
		return model.Lease{}, fmt.Errorf("lease %q not found", leaseID)
	}

	var latest model.Lease
	found := false
	for _, lease := range state.Leases {
		if lease.AccessMode != "vpn" {
			continue
		}
		if !found || lease.IssuedAt.After(latest.IssuedAt) {
			latest = lease
			found = true
		}
	}
	if !found {
		return model.Lease{}, fmt.Errorf("no vpn lease found")
	}
	return latest, nil
}

func formatPlan(plan model.Plan) string {
	if len(plan.Creates) == 0 && len(plan.Updates) == 0 && len(plan.Deletes) == 0 {
		return "No changes.\n"
	}

	var b strings.Builder
	for _, policy := range plan.Creates {
		fmt.Fprintf(&b, "+ create policy %s (%s)\n", policy.Name, policy.Region)
	}
	for _, change := range plan.Updates {
		fmt.Fprintf(&b, "~ update policy %s (%s -> %s)\n", change.Before.Name, change.Before.Region, change.After.Region)
	}
	for _, policy := range plan.Deletes {
		fmt.Fprintf(&b, "- delete policy %s (%s)\n", policy.Name, policy.Region)
	}
	return b.String()
}

func usageError(prefix string) error {
	msg := `Usage:
  egress locations
  egress accounts [-state .egress/state.json]
  egress import-aws-cli -profile <name> [-name saved-name] [-state .egress/state.json]
  egress plan -f config.json [-state .egress/state.json]
  egress apply -f config.json [-state .egress/state.json]
  egress attach -workload <id> -policy <name|id> [-state .egress/state.json]
  egress lease -workload <id> [-state .egress/state.json]
  egress provision -location <region|location-id> [-account ref] [-access-mode proxy|vpn] [-workload id] [-state .egress/state.json]
  egress connect-local (-lease <id> | -latest) [-state .egress/state.json]
  egress disconnect-local [-lease <id> | -latest] [-state .egress/state.json]
  egress destroy-lease -lease <id> [-state .egress/state.json]
  egress cleanup-all [-state .egress/state.json]
  egress local-status
  egress state [-state .egress/state.json]
  egress serve [-addr 127.0.0.1:8080] [-state .egress/state.json]`
	if prefix == "" {
		return errors.New(msg)
	}
	return fmt.Errorf("%s\n\n%s", prefix, msg)
}
