package control

import (
	"context"
	"testing"
	"time"

	"egress/internal/awsprovision"
	"egress/internal/model"
)

func TestPlanApplyLeaseLifecycle(t *testing.T) {
	cfg := model.Config{
		Policies: []model.PolicySpec{
			{
				Name:         "ci-us",
				Region:       "us-east-1",
				Destinations: []string{"api.openai.com"},
				TTLMinutes:   30,
			},
		},
	}

	state := model.State{Policies: map[string]model.PolicyRecord{}}
	plan := Plan(cfg, state)
	if len(plan.Creates) != 1 {
		t.Fatalf("expected one create, got %d", len(plan.Creates))
	}

	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	state = Apply(cfg, state, now)
	if len(state.Policies) != 1 {
		t.Fatalf("expected one policy, got %d", len(state.Policies))
	}

	var err error
	state, _, err = Attach(state, "buildkite/job/123", "ci-us", now)
	if err != nil {
		t.Fatalf("attach failed: %v", err)
	}

	state, lease, err := Lease(state, "buildkite/job/123", "proxy", now)
	if err != nil {
		t.Fatalf("lease failed: %v", err)
	}
	if lease.Region != "us-east-1" {
		t.Fatalf("expected us-east-1 region, got %s", lease.Region)
	}
	if lease.PublicIP == "" {
		t.Fatal("expected public ip")
	}
	if lease.Connection.ProxyURL == "" {
		t.Fatal("expected proxy connection details")
	}
}

func TestUpsertCloudAccount(t *testing.T) {
	now := time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC)
	state := model.State{Policies: map[string]model.PolicyRecord{}}

	account := model.CloudAccount{
		Name:           "prod-aws",
		Provider:       "aws",
		AWSAccountID:   "123456789012",
		RoleARN:        "arn:aws:iam::123456789012:role/EgressControlPlane",
		DefaultRegions: []string{"us-east-1", "eu-west-1"},
	}

	next, created, action, err := UpsertCloudAccount(state, account, now)
	if err != nil {
		t.Fatalf("upsert account failed: %v", err)
	}
	if action != "created" {
		t.Fatalf("expected created action, got %s", action)
	}
	if created.ID == "" {
		t.Fatal("expected account id")
	}
	if len(next.Accounts) != 1 {
		t.Fatalf("expected one account, got %d", len(next.Accounts))
	}
}

func TestUpsertCloudAccountFromAWSCLIProfile(t *testing.T) {
	now := time.Date(2026, 2, 4, 4, 5, 6, 0, time.UTC)
	state := model.State{Policies: map[string]model.PolicyRecord{}}

	account := model.CloudAccount{
		Name:             "developer",
		Provider:         "aws",
		AWSAccountID:     "123456789012",
		AWSProfile:       "developer",
		PrincipalARN:     "arn:aws:sts::123456789012:assumed-role/Admin/developer",
		CredentialSource: "aws_cli",
	}

	next, created, action, err := UpsertCloudAccount(state, account, now)
	if err != nil {
		t.Fatalf("upsert cli account failed: %v", err)
	}
	if action != "created" {
		t.Fatalf("expected created action, got %s", action)
	}
	if created.AWSProfile != "developer" {
		t.Fatalf("expected aws profile to be preserved, got %s", created.AWSProfile)
	}
	if created.CredentialSource != "aws_cli" {
		t.Fatalf("expected aws_cli source, got %s", created.CredentialSource)
	}
	if len(next.Accounts) != 1 {
		t.Fatalf("expected one account, got %d", len(next.Accounts))
	}
}

func TestProvisionAccessRequiresConnectedAWSCLIAccount(t *testing.T) {
	now := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	state := model.State{
		Policies:    map[string]model.PolicyRecord{},
		Accounts:    []model.CloudAccount{},
		Attachments: []model.Attachment{},
		Leases:      []model.Lease{},
	}

	_, _, err := ProvisionAccess(state, "", "frankfurt", "vpn", "", now)
	if err == nil {
		t.Fatal("expected provisioning to fail without a connected aws cli account")
	}
}

func TestDestroyLeaseRemovesSharedGatewayLeases(t *testing.T) {
	originalDestroy := destroyGateway
	t.Cleanup(func() { destroyGateway = originalDestroy })
	destroyGateway = func(ctx context.Context, account model.CloudAccount, lease model.Lease) (awsprovision.CleanupSummary, error) {
		return awsprovision.CleanupSummary{DestroyedGatewayCount: 1}, nil
	}

	now := time.Date(2026, 4, 5, 6, 7, 8, 0, time.UTC)
	state := model.State{
		Policies: map[string]model.PolicyRecord{},
		Accounts: []model.CloudAccount{
			{ID: "acct_1", Name: "dev", Provider: "aws", AWSProfile: "dev", AWSAccountID: "123", CredentialSource: "aws_cli", Status: "connected"},
		},
		Leases: []model.Lease{
			{ID: "lease_1", Provider: "aws", AccountID: "acct_1", Region: "us-east-1", AccessMode: "vpn", GatewayID: "i-123", ExpiresAt: now.Add(time.Hour)},
			{ID: "lease_2", Provider: "aws", AccountID: "acct_1", Region: "us-east-1", AccessMode: "vpn", GatewayID: "i-123", ExpiresAt: now.Add(time.Hour)},
			{ID: "lease_3", Provider: "aws", AccountID: "acct_1", Region: "us-east-1", AccessMode: "proxy", GatewayID: "i-999", ExpiresAt: now.Add(time.Hour)},
		},
	}

	next, result, err := DestroyLease(state, "lease_1", now)
	if err != nil {
		t.Fatalf("destroy lease failed: %v", err)
	}
	if result.RemovedLeaseCount != 2 {
		t.Fatalf("expected 2 removed leases, got %d", result.RemovedLeaseCount)
	}
	if len(next.Leases) != 1 || next.Leases[0].ID != "lease_3" {
		t.Fatalf("expected only unrelated lease to remain, got %#v", next.Leases)
	}
}

func TestCleanupAllResourcesRemovesAWSLeases(t *testing.T) {
	originalCleanup := cleanupAccountGateways
	t.Cleanup(func() { cleanupAccountGateways = originalCleanup })
	cleanupAccountGateways = func(ctx context.Context, account model.CloudAccount) (awsprovision.CleanupSummary, error) {
		return awsprovision.CleanupSummary{DestroyedGatewayCount: 2}, nil
	}

	now := time.Date(2026, 4, 5, 6, 7, 8, 0, time.UTC)
	state := model.State{
		Policies: map[string]model.PolicyRecord{},
		Accounts: []model.CloudAccount{
			{ID: "acct_1", Name: "dev", Provider: "aws", AWSProfile: "dev", AWSAccountID: "123", CredentialSource: "aws_cli", Status: "connected"},
			{ID: "acct_2", Name: "manual", Provider: "aws", RoleARN: "arn:aws:iam::123:role/X", AWSAccountID: "123", Status: "connected"},
		},
		Leases: []model.Lease{
			{ID: "lease_1", Provider: "aws", AccountID: "acct_1", ExpiresAt: now.Add(time.Hour)},
			{ID: "lease_2", Provider: "", ExpiresAt: now.Add(time.Hour)},
		},
	}

	next, result, err := CleanupAllResources(state, now)
	if err != nil {
		t.Fatalf("cleanup all failed: %v", err)
	}
	if result.RemovedLeaseCount != 1 {
		t.Fatalf("expected 1 removed aws lease, got %d", result.RemovedLeaseCount)
	}
	if len(next.Leases) != 1 || next.Leases[0].ID != "lease_2" {
		t.Fatalf("expected non-aws lease to remain, got %#v", next.Leases)
	}
}

func TestCleanupResourcesTargetsSelectedAccount(t *testing.T) {
	originalCleanup := cleanupAccountGateways
	t.Cleanup(func() { cleanupAccountGateways = originalCleanup })
	called := ""
	cleanupAccountGateways = func(ctx context.Context, account model.CloudAccount) (awsprovision.CleanupSummary, error) {
		called = account.ID
		return awsprovision.CleanupSummary{DestroyedGatewayCount: 1}, nil
	}

	now := time.Date(2026, 4, 5, 6, 7, 8, 0, time.UTC)
	state := model.State{
		Policies: map[string]model.PolicyRecord{},
		Accounts: []model.CloudAccount{
			{ID: "acct_1", Name: "dev-a", Provider: "aws", AWSProfile: "dev-a", AWSAccountID: "123", CredentialSource: "aws_cli", Status: "connected"},
			{ID: "acct_2", Name: "dev-b", Provider: "aws", AWSProfile: "dev-b", AWSAccountID: "456", CredentialSource: "aws_cli", Status: "connected"},
		},
		Leases: []model.Lease{
			{ID: "lease_1", Provider: "aws", AccountID: "acct_1", ExpiresAt: now.Add(time.Hour)},
			{ID: "lease_2", Provider: "aws", AccountID: "acct_2", ExpiresAt: now.Add(time.Hour)},
		},
	}

	next, result, err := CleanupResources(state, "acct_2", now)
	if err != nil {
		t.Fatalf("cleanup resources failed: %v", err)
	}
	if called != "acct_2" {
		t.Fatalf("expected cleanup to target acct_2, got %s", called)
	}
	if result.AccountID != "acct_2" {
		t.Fatalf("expected result account id acct_2, got %s", result.AccountID)
	}
	if len(next.Leases) != 1 || next.Leases[0].AccountID != "acct_1" {
		t.Fatalf("expected acct_1 lease to remain, got %#v", next.Leases)
	}
}
