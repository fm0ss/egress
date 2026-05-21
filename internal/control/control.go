package control

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"egress/internal/awsprovision"
	"egress/internal/model"
)

const stateVersion = 1

var supportedLocations = []model.Location{
	{ID: "us-east-1", Name: "N. Virginia", Region: "us-east-1", CountryCode: "US", Latitude: 39.04, Longitude: -77.49},
	{ID: "us-east-2", Name: "Ohio", Region: "us-east-2", CountryCode: "US", Latitude: 40.42, Longitude: -82.91},
	{ID: "us-west-1", Name: "N. California", Region: "us-west-1", CountryCode: "US", Latitude: 37.34, Longitude: -121.89},
	{ID: "us-west-2", Name: "Oregon", Region: "us-west-2", CountryCode: "US", Latitude: 45.52, Longitude: -122.68},
	{ID: "ca-central-1", Name: "Montreal", Region: "ca-central-1", CountryCode: "CA", Latitude: 45.50, Longitude: -73.57},
	{ID: "ca-west-1", Name: "Calgary", Region: "ca-west-1", CountryCode: "CA", Latitude: 51.05, Longitude: -114.07},
	{ID: "mx-central-1", Name: "Queretaro", Region: "mx-central-1", CountryCode: "MX", Latitude: 20.59, Longitude: -100.39},
	{ID: "sa-east-1", Name: "Sao Paulo", Region: "sa-east-1", CountryCode: "BR", Latitude: -23.55, Longitude: -46.63},
	{ID: "eu-west-1", Name: "Dublin", Region: "eu-west-1", CountryCode: "IE", Latitude: 53.35, Longitude: -6.26},
	{ID: "eu-west-2", Name: "London", Region: "eu-west-2", CountryCode: "GB", Latitude: 51.51, Longitude: -0.13},
	{ID: "eu-west-3", Name: "Paris", Region: "eu-west-3", CountryCode: "FR", Latitude: 48.86, Longitude: 2.35},
	{ID: "eu-central-1", Name: "Frankfurt", Region: "eu-central-1", CountryCode: "DE", Latitude: 50.11, Longitude: 8.68},
	{ID: "eu-central-2", Name: "Zurich", Region: "eu-central-2", CountryCode: "CH", Latitude: 47.38, Longitude: 8.54},
	{ID: "eu-north-1", Name: "Stockholm", Region: "eu-north-1", CountryCode: "SE", Latitude: 59.33, Longitude: 18.07},
	{ID: "eu-south-1", Name: "Milan", Region: "eu-south-1", CountryCode: "IT", Latitude: 45.46, Longitude: 9.19},
	{ID: "eu-south-2", Name: "Spain", Region: "eu-south-2", CountryCode: "ES", Latitude: 40.42, Longitude: -3.70},
	{ID: "af-south-1", Name: "Cape Town", Region: "af-south-1", CountryCode: "ZA", Latitude: -33.92, Longitude: 18.42},
	{ID: "me-south-1", Name: "Bahrain", Region: "me-south-1", CountryCode: "BH", Latitude: 26.07, Longitude: 50.56},
	{ID: "me-central-1", Name: "UAE", Region: "me-central-1", CountryCode: "AE", Latitude: 25.20, Longitude: 55.27},
	{ID: "il-central-1", Name: "Tel Aviv", Region: "il-central-1", CountryCode: "IL", Latitude: 32.09, Longitude: 34.78},
	{ID: "ap-south-1", Name: "Mumbai", Region: "ap-south-1", CountryCode: "IN", Latitude: 19.08, Longitude: 72.88},
	{ID: "ap-south-2", Name: "Hyderabad", Region: "ap-south-2", CountryCode: "IN", Latitude: 17.39, Longitude: 78.49},
	{ID: "ap-east-1", Name: "Hong Kong", Region: "ap-east-1", CountryCode: "HK", Latitude: 22.32, Longitude: 114.17},
	{ID: "ap-northeast-1", Name: "Tokyo", Region: "ap-northeast-1", CountryCode: "JP", Latitude: 35.68, Longitude: 139.69},
	{ID: "ap-northeast-2", Name: "Seoul", Region: "ap-northeast-2", CountryCode: "KR", Latitude: 37.57, Longitude: 126.98},
	{ID: "ap-northeast-3", Name: "Osaka", Region: "ap-northeast-3", CountryCode: "JP", Latitude: 34.69, Longitude: 135.50},
	{ID: "ap-southeast-1", Name: "Singapore", Region: "ap-southeast-1", CountryCode: "SG", Latitude: 1.35, Longitude: 103.82},
	{ID: "ap-southeast-2", Name: "Sydney", Region: "ap-southeast-2", CountryCode: "AU", Latitude: -33.87, Longitude: 151.21},
	{ID: "ap-southeast-3", Name: "Jakarta", Region: "ap-southeast-3", CountryCode: "ID", Latitude: -6.21, Longitude: 106.85},
	{ID: "ap-southeast-4", Name: "Melbourne", Region: "ap-southeast-4", CountryCode: "AU", Latitude: -37.81, Longitude: 144.96},
}

var supportedRegionSet = func() map[string]struct{} {
	regions := make(map[string]struct{}, len(supportedLocations))
	for _, location := range supportedLocations {
		regions[location.Region] = struct{}{}
	}
	return regions
}()

var (
	provisionGateway       = awsprovision.Provision
	destroyGateway         = awsprovision.DestroyGateway
	cleanupAccountGateways = awsprovision.CleanupAccountResources
)

func SupportedRegions() []string {
	regions := make([]string, 0, len(supportedRegionSet))
	for region := range supportedRegionSet {
		regions = append(regions, region)
	}
	sort.Strings(regions)
	return regions
}

func SupportedLocations() []model.Location {
	locations := make([]model.Location, len(supportedLocations))
	copy(locations, supportedLocations)
	return locations
}

func LoadConfig(path string) (model.Config, error) {
	var cfg model.Config
	raw, err := os.ReadFile(path)
	if err != nil {
		return cfg, fmt.Errorf("read config: %w", err)
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return cfg, fmt.Errorf("parse config: %w", err)
	}
	if err := ValidateConfig(cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func ValidateConfig(cfg model.Config) error {
	seen := map[string]struct{}{}
	for _, policy := range cfg.Policies {
		if policy.Name == "" {
			return fmt.Errorf("policy name is required")
		}
		if _, ok := seen[policy.Name]; ok {
			return fmt.Errorf("duplicate policy name %q", policy.Name)
		}
		seen[policy.Name] = struct{}{}
		if policy.Region == "" {
			return fmt.Errorf("policy %q must define region", policy.Name)
		}
		if !isSupportedRegion(policy.Region) {
			return fmt.Errorf("policy %q uses unsupported region %q", policy.Name, policy.Region)
		}
		if policy.TTLMinutes < 0 {
			return fmt.Errorf("policy %q ttl_minutes must be positive", policy.Name)
		}
	}
	return nil
}

func LoadState(path string) (model.State, error) {
	state := model.State{
		Version:     stateVersion,
		Policies:    map[string]model.PolicyRecord{},
		Accounts:    []model.CloudAccount{},
		Attachments: []model.Attachment{},
		Leases:      []model.Lease{},
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return state, nil
		}
		return state, fmt.Errorf("read state: %w", err)
	}
	if err := json.Unmarshal(raw, &state); err != nil {
		return state, fmt.Errorf("parse state: %w", err)
	}
	if state.Policies == nil {
		state.Policies = map[string]model.PolicyRecord{}
	}
	if state.Accounts == nil {
		state.Accounts = []model.CloudAccount{}
	}
	if state.Attachments == nil {
		state.Attachments = []model.Attachment{}
	}
	if state.Leases == nil {
		state.Leases = []model.Lease{}
	}
	if state.Version == 0 {
		state.Version = stateVersion
	}
	return pruneExpiredLeases(state, time.Now().UTC()), nil
}

func SaveState(path string, state model.State) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir state dir: %w", err)
	}
	state = pruneExpiredLeases(state, time.Now().UTC())
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		return fmt.Errorf("write state: %w", err)
	}
	return nil
}

func Plan(cfg model.Config, state model.State) model.Plan {
	desired := map[string]model.PolicySpec{}
	for _, policy := range cfg.Policies {
		desired[policy.Name] = normalizePolicy(policy)
	}

	plan := model.Plan{}
	for _, policy := range cfg.Policies {
		current, ok := state.Policies[policy.Name]
		if !ok {
			plan.Creates = append(plan.Creates, normalizePolicy(policy))
			continue
		}
		if current.Fingerprint != fingerprintPolicy(policy) {
			plan.Updates = append(plan.Updates, model.Change{
				Before: current,
				After:  normalizePolicy(policy),
			})
		}
	}

	for name, current := range state.Policies {
		if _, ok := desired[name]; !ok {
			plan.Deletes = append(plan.Deletes, current)
		}
	}

	sort.Slice(plan.Creates, func(i, j int) bool { return plan.Creates[i].Name < plan.Creates[j].Name })
	sort.Slice(plan.Updates, func(i, j int) bool { return plan.Updates[i].Before.Name < plan.Updates[j].Before.Name })
	sort.Slice(plan.Deletes, func(i, j int) bool { return plan.Deletes[i].Name < plan.Deletes[j].Name })
	return plan
}

func Apply(cfg model.Config, state model.State, now time.Time) model.State {
	next := model.State{
		Version:     stateVersion,
		Policies:    map[string]model.PolicyRecord{},
		Accounts:    append([]model.CloudAccount(nil), state.Accounts...),
		Attachments: append([]model.Attachment(nil), state.Attachments...),
		Leases:      append([]model.Lease(nil), state.Leases...),
	}

	for _, policy := range cfg.Policies {
		policy = normalizePolicy(policy)
		current, ok := state.Policies[policy.Name]
		if ok {
			next.Policies[policy.Name] = model.PolicyRecord{
				ID:              current.ID,
				Name:            policy.Name,
				Region:          policy.Region,
				FallbackRegions: policy.FallbackRegions,
				Residency:       policy.Residency,
				IPClass:         policy.IPClass,
				Mode:            policy.Mode,
				Destinations:    policy.Destinations,
				TTLMinutes:      policy.TTLMinutes,
				Fingerprint:     fingerprintPolicy(policy),
				CreatedAt:       current.CreatedAt,
				UpdatedAt:       now.UTC(),
			}
			continue
		}

		next.Policies[policy.Name] = model.PolicyRecord{
			ID:              "policy_" + stableID(policy.Name),
			Name:            policy.Name,
			Region:          policy.Region,
			FallbackRegions: policy.FallbackRegions,
			Residency:       policy.Residency,
			IPClass:         policy.IPClass,
			Mode:            policy.Mode,
			Destinations:    policy.Destinations,
			TTLMinutes:      policy.TTLMinutes,
			Fingerprint:     fingerprintPolicy(policy),
			CreatedAt:       now.UTC(),
			UpdatedAt:       now.UTC(),
		}
	}

	validPolicyIDs := map[string]struct{}{}
	for _, policy := range next.Policies {
		validPolicyIDs[policy.ID] = struct{}{}
	}

	filteredAttachments := next.Attachments[:0]
	for _, attachment := range next.Attachments {
		if _, ok := validPolicyIDs[attachment.PolicyID]; ok {
			filteredAttachments = append(filteredAttachments, attachment)
		}
	}
	next.Attachments = filteredAttachments

	filteredLeases := next.Leases[:0]
	for _, lease := range next.Leases {
		if _, ok := validPolicyIDs[lease.PolicyID]; ok && lease.ExpiresAt.After(now.UTC()) {
			filteredLeases = append(filteredLeases, lease)
		}
	}
	next.Leases = filteredLeases

	return next
}

func UpsertPolicy(state model.State, policy model.PolicySpec, now time.Time) (model.State, model.PolicyRecord, string, error) {
	cfg := model.Config{Policies: []model.PolicySpec{policy}}
	if err := ValidateConfig(cfg); err != nil {
		return state, model.PolicyRecord{}, "", err
	}

	next := model.State{
		Version:     state.Version,
		Policies:    copyPolicies(state.Policies),
		Accounts:    append([]model.CloudAccount(nil), state.Accounts...),
		Attachments: append([]model.Attachment(nil), state.Attachments...),
		Leases:      append([]model.Lease(nil), state.Leases...),
	}

	policy = normalizePolicy(policy)
	current, ok := next.Policies[policy.Name]
	action := "created"
	if ok {
		action = "updated"
		next.Policies[policy.Name] = model.PolicyRecord{
			ID:              current.ID,
			Name:            policy.Name,
			Region:          policy.Region,
			FallbackRegions: policy.FallbackRegions,
			Residency:       policy.Residency,
			IPClass:         policy.IPClass,
			Mode:            policy.Mode,
			Destinations:    policy.Destinations,
			TTLMinutes:      policy.TTLMinutes,
			Fingerprint:     fingerprintPolicy(policy),
			CreatedAt:       current.CreatedAt,
			UpdatedAt:       now.UTC(),
		}
		return next, next.Policies[policy.Name], action, nil
	}

	next.Policies[policy.Name] = model.PolicyRecord{
		ID:              "policy_" + stableID(policy.Name),
		Name:            policy.Name,
		Region:          policy.Region,
		FallbackRegions: policy.FallbackRegions,
		Residency:       policy.Residency,
		IPClass:         policy.IPClass,
		Mode:            policy.Mode,
		Destinations:    policy.Destinations,
		TTLMinutes:      policy.TTLMinutes,
		Fingerprint:     fingerprintPolicy(policy),
		CreatedAt:       now.UTC(),
		UpdatedAt:       now.UTC(),
	}
	return next, next.Policies[policy.Name], action, nil
}

func UpsertCloudAccount(state model.State, account model.CloudAccount, now time.Time) (model.State, model.CloudAccount, string, error) {
	account, err := normalizeCloudAccount(account)
	if err != nil {
		return state, model.CloudAccount{}, "", err
	}

	next := model.State{
		Version:     state.Version,
		Policies:    copyPolicies(state.Policies),
		Accounts:    append([]model.CloudAccount(nil), state.Accounts...),
		Attachments: append([]model.Attachment(nil), state.Attachments...),
		Leases:      append([]model.Lease(nil), state.Leases...),
	}

	action := "created"
	for i := range next.Accounts {
		if next.Accounts[i].ID == account.ID || next.Accounts[i].Name == account.Name {
			action = "updated"
			account.ID = next.Accounts[i].ID
			account.CreatedAt = next.Accounts[i].CreatedAt
			account.UpdatedAt = now.UTC()
			next.Accounts[i] = account
			return next, account, action, nil
		}
	}

	account.ID = "acct_" + stableID(account.Provider+"_"+account.Name)
	account.CreatedAt = now.UTC()
	account.UpdatedAt = now.UTC()
	next.Accounts = append(next.Accounts, account)
	sort.Slice(next.Accounts, func(i, j int) bool { return next.Accounts[i].Name < next.Accounts[j].Name })
	return next, account, action, nil
}

func Attach(state model.State, workloadID, policyRef string, now time.Time) (model.State, model.Attachment, error) {
	if workloadID == "" {
		return state, model.Attachment{}, fmt.Errorf("workload id is required")
	}
	policy, err := resolvePolicy(state, policyRef)
	if err != nil {
		return state, model.Attachment{}, err
	}

	attachment := model.Attachment{
		WorkloadID: workloadID,
		PolicyID:   policy.ID,
		AttachedAt: now.UTC(),
	}

	replaced := false
	for i := range state.Attachments {
		if state.Attachments[i].WorkloadID == workloadID {
			state.Attachments[i] = attachment
			replaced = true
			break
		}
	}
	if !replaced {
		state.Attachments = append(state.Attachments, attachment)
	}

	return state, attachment, nil
}

func Lease(state model.State, workloadID string, accessMode string, now time.Time) (model.State, model.Lease, error) {
	if workloadID == "" {
		return state, model.Lease{}, fmt.Errorf("workload id is required")
	}

	var attachment *model.Attachment
	for i := range state.Attachments {
		if state.Attachments[i].WorkloadID == workloadID {
			attachment = &state.Attachments[i]
			break
		}
	}
	if attachment == nil {
		return state, model.Lease{}, fmt.Errorf("workload %q is not attached to any policy", workloadID)
	}

	policy, err := policyByID(state, attachment.PolicyID)
	if err != nil {
		return state, model.Lease{}, err
	}

	if accessMode == "" {
		accessMode = "proxy"
	}
	for _, lease := range state.Leases {
		if lease.WorkloadID == workloadID && lease.PolicyID == policy.ID && lease.AccessMode == accessMode && lease.ExpiresAt.After(now.UTC()) {
			return state, lease, nil
		}
	}

	ttl := 60 * time.Minute
	if policy.TTLMinutes > 0 {
		ttl = time.Duration(policy.TTLMinutes) * time.Minute
	}

	ip, gatewayID := allocateIP(state, policy.Region)
	locationName := locationNameForRegion(policy.Region)
	lease := model.Lease{
		ID:         "lease_" + stableID(workloadID+"_"+now.UTC().Format(time.RFC3339Nano)),
		WorkloadID: workloadID,
		PolicyID:   policy.ID,
		Region:     policy.Region,
		Location:   locationName,
		GatewayID:  gatewayID,
		PublicIP:   ip,
		Endpoint:   fmt.Sprintf("%s.egress.local:51820", gatewayID),
		AccessMode: accessMode,
		Connection: buildConnectionBundle(workloadID, accessMode, gatewayID, ip),
		IssuedAt:   now.UTC(),
		ExpiresAt:  now.UTC().Add(ttl),
	}

	state.Leases = append(state.Leases, lease)
	state = pruneExpiredLeases(state, now.UTC())
	return state, lease, nil
}

func ProvisionAccess(state model.State, accountRef, locationID, accessMode, workloadID string, now time.Time) (model.State, model.Lease, error) {
	location, err := resolveLocation(locationID)
	if err != nil {
		return state, model.Lease{}, err
	}
	if accessMode == "" {
		accessMode = "proxy"
	}
	if accessMode != "proxy" && accessMode != "vpn" {
		return state, model.Lease{}, fmt.Errorf("unsupported access mode %q", accessMode)
	}
	if workloadID == "" {
		workloadID = fmt.Sprintf("%s-%s-%s", accessMode, location.ID, stableID(now.UTC().Format(time.RFC3339Nano)))
	}
	account, err := resolveProvisionAccount(state, accountRef)
	if err != nil {
		return state, model.Lease{}, err
	}

	policy := model.PolicySpec{
		Name:         fmt.Sprintf("auto-%s-%s", location.Region, accessMode),
		Region:       location.Region,
		Mode:         "on_demand",
		IPClass:      "shared",
		TTLMinutes:   60,
		Residency:    strings.ToLower(location.CountryCode),
		Destinations: nil,
	}

	next, _, _, err := UpsertPolicy(state, policy, now)
	if err != nil {
		return state, model.Lease{}, err
	}
	next, _, err = Attach(next, workloadID, policy.Name, now)
	if err != nil {
		return state, model.Lease{}, err
	}

	policyRecord, err := resolvePolicy(next, policy.Name)
	if err != nil {
		return state, model.Lease{}, err
	}

	leaseID := "lease_" + stableID(workloadID+"_"+now.UTC().Format(time.RFC3339Nano))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	provisioned, err := provisionGateway(ctx, account, location, accessMode, workloadID, leaseID, now.UTC().Add(time.Duration(policy.TTLMinutes)*time.Minute))
	if err != nil {
		return state, model.Lease{}, err
	}
	lease := provisioned.Lease
	lease.PolicyID = policyRecord.ID
	lease.GatewayID = lease.Resources.InstanceID
	lease.IssuedAt = now.UTC()
	if lease.ExpiresAt.IsZero() {
		lease.ExpiresAt = now.UTC().Add(time.Duration(policy.TTLMinutes) * time.Minute)
	}
	next.Leases = append(next.Leases, lease)
	next = pruneExpiredLeases(next, now.UTC())
	return next, lease, nil
}

func DestroyLease(state model.State, leaseID string, now time.Time) (model.State, model.CleanupResult, error) {
	leaseID = strings.TrimSpace(leaseID)
	if leaseID == "" {
		return state, model.CleanupResult{}, fmt.Errorf("lease id is required")
	}

	var target *model.Lease
	for i := range state.Leases {
		if state.Leases[i].ID == leaseID {
			target = &state.Leases[i]
			break
		}
	}
	if target == nil {
		return state, model.CleanupResult{}, fmt.Errorf("lease %q not found", leaseID)
	}

	result := model.CleanupResult{
		Scope:      "lease",
		AccountID:  target.AccountID,
		LeaseID:    target.ID,
		GatewayID:  target.GatewayID,
		Region:     target.Region,
		AccessMode: target.AccessMode,
	}

	if target.Provider == "aws" && target.AccountID != "" {
		account, err := accountByID(state, target.AccountID)
		if err != nil {
			return state, model.CleanupResult{}, err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		summary, err := destroyGateway(ctx, account, *target)
		if err != nil {
			return state, model.CleanupResult{}, err
		}
		result.DestroyedGatewayCount = summary.DestroyedGatewayCount
		result.ReleasedAddressCount = summary.ReleasedAddressCount
		result.DeletedSecurityGroups = summary.DeletedSecurityGroups
	}

	filtered := state.Leases[:0]
	for _, lease := range state.Leases {
		if sameGatewayLease(lease, *target) {
			result.RemovedLeaseCount++
			continue
		}
		filtered = append(filtered, lease)
	}
	state.Leases = filtered
	state = pruneExpiredLeases(state, now.UTC())
	result.Detail = fmt.Sprintf("removed %d lease(s) and cleaned up gateway %s", result.RemovedLeaseCount, result.GatewayID)
	return state, result, nil
}

func CleanupResources(state model.State, accountRef string, now time.Time) (model.State, model.CleanupResult, error) {
	result := model.CleanupResult{Scope: "all"}
	accounts, err := cleanupAccountsForRef(state, accountRef)
	if err != nil {
		return state, model.CleanupResult{}, err
	}
	if len(accounts) == 1 {
		result.Scope = "account"
		result.AccountID = accounts[0].ID
	}

	for _, account := range accounts {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		summary, err := cleanupAccountGateways(ctx, account)
		cancel()
		if err != nil {
			return state, model.CleanupResult{}, err
		}
		result.DestroyedGatewayCount += summary.DestroyedGatewayCount
		result.ReleasedAddressCount += summary.ReleasedAddressCount
		result.DeletedSecurityGroups += summary.DeletedSecurityGroups
	}

	kept := state.Leases[:0]
	for _, lease := range state.Leases {
		if lease.Provider == "aws" && (accountRef == "" || accountMatches(accounts, lease.AccountID)) {
			result.RemovedLeaseCount++
			continue
		}
		kept = append(kept, lease)
	}
	state.Leases = kept
	state = pruneExpiredLeases(state, now.UTC())
	result.Detail = fmt.Sprintf("removed %d aws lease(s), destroyed %d gateway(s)", result.RemovedLeaseCount, result.DestroyedGatewayCount)
	return state, result, nil
}

func CleanupAllResources(state model.State, now time.Time) (model.State, model.CleanupResult, error) {
	return CleanupResources(state, "", now)
}

func pruneExpiredLeases(state model.State, now time.Time) model.State {
	filtered := state.Leases[:0]
	for _, lease := range state.Leases {
		if lease.ExpiresAt.After(now) {
			if lease.AccessMode == "" {
				lease.AccessMode = "proxy"
			}
			if lease.Location == "" {
				lease.Location = locationNameForRegion(lease.Region)
			}
			if lease.Connection.Type == "" {
				lease.Connection = buildConnectionBundle(lease.WorkloadID, lease.AccessMode, lease.GatewayID, lease.PublicIP)
			}
			filtered = append(filtered, lease)
		}
	}
	state.Leases = filtered
	return state
}

func resolvePolicy(state model.State, ref string) (model.PolicyRecord, error) {
	if policy, ok := state.Policies[ref]; ok {
		return policy, nil
	}
	for _, policy := range state.Policies {
		if policy.ID == ref {
			return policy, nil
		}
	}
	return model.PolicyRecord{}, fmt.Errorf("policy %q not found", ref)
}

func accountByID(state model.State, id string) (model.CloudAccount, error) {
	for _, account := range state.Accounts {
		if account.ID == id {
			return account, nil
		}
	}
	return model.CloudAccount{}, fmt.Errorf("account %q not found", id)
}

func cleanupAccountsForRef(state model.State, accountRef string) ([]model.CloudAccount, error) {
	if strings.TrimSpace(accountRef) != "" {
		account, err := resolveProvisionAccount(state, accountRef)
		if err != nil {
			return nil, err
		}
		return []model.CloudAccount{account}, nil
	}

	seen := map[string]struct{}{}
	accounts := make([]model.CloudAccount, 0)
	for _, account := range state.Accounts {
		if account.Provider != "aws" || account.CredentialSource != "aws_cli" || account.Status != "connected" {
			continue
		}
		key := account.AWSProfile + "|" + account.AWSAccountID
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		accounts = append(accounts, account)
	}
	return accounts, nil
}

func accountMatches(accounts []model.CloudAccount, accountID string) bool {
	for _, account := range accounts {
		if account.ID == accountID {
			return true
		}
	}
	return false
}

func policyByID(state model.State, id string) (model.PolicyRecord, error) {
	for _, policy := range state.Policies {
		if policy.ID == id {
			return policy, nil
		}
	}
	return model.PolicyRecord{}, fmt.Errorf("policy %q not found", id)
}

func allocateIP(state model.State, region string) (string, string) {
	if !isSupportedRegion(region) {
		return "0.0.0.0", "gw-unknown"
	}

	used := 0
	for _, lease := range state.Leases {
		if lease.Region == region {
			used++
		}
	}
	index := used % 3
	gatewayID := fmt.Sprintf("gw-%s-%02d", region, index+1)
	hash := sha256.Sum256([]byte(region))
	ip := fmt.Sprintf("198.19.%d.%d", int(hash[0]), int(hash[1])+index+1)
	return ip, gatewayID
}

func normalizePolicy(policy model.PolicySpec) model.PolicySpec {
	if policy.IPClass == "" {
		policy.IPClass = "shared"
	}
	if policy.Mode == "" {
		policy.Mode = "on_demand"
	}
	if policy.TTLMinutes == 0 {
		policy.TTLMinutes = 60
	}
	sort.Strings(policy.FallbackRegions)
	sort.Strings(policy.Destinations)
	return policy
}

func normalizeCloudAccount(account model.CloudAccount) (model.CloudAccount, error) {
	account.Name = strings.TrimSpace(account.Name)
	account.Provider = strings.ToLower(strings.TrimSpace(account.Provider))
	account.AWSAccountID = strings.TrimSpace(account.AWSAccountID)
	account.AWSProfile = strings.TrimSpace(account.AWSProfile)
	account.RoleARN = strings.TrimSpace(account.RoleARN)
	account.PrincipalARN = strings.TrimSpace(account.PrincipalARN)
	account.ExternalID = strings.TrimSpace(account.ExternalID)
	account.CredentialSource = strings.ToLower(strings.TrimSpace(account.CredentialSource))
	account.Status = strings.ToLower(strings.TrimSpace(account.Status))
	if account.Status == "" {
		account.Status = "connected"
	}
	if account.Name == "" {
		return account, fmt.Errorf("account name is required")
	}
	if account.Provider != "aws" {
		return account, fmt.Errorf("provider %q is not supported yet", account.Provider)
	}
	if account.AWSProfile == "" && account.AWSAccountID == "" {
		return account, fmt.Errorf("aws_account_id is required for aws accounts")
	}
	if account.AWSProfile != "" {
		if account.CredentialSource == "" {
			account.CredentialSource = "aws_cli"
		}
		if account.AWSAccountID == "" {
			return account, fmt.Errorf("aws_account_id is required for aws cli accounts")
		}
	} else if account.RoleARN == "" && account.PrincipalARN == "" {
		return account, fmt.Errorf("role_arn or principal_arn is required for aws accounts")
	}
	sort.Strings(account.DefaultRegions)
	for _, region := range account.DefaultRegions {
		if !isSupportedRegion(region) {
			return account, fmt.Errorf("unsupported default region %q", region)
		}
	}
	return account, nil
}

func isSupportedRegion(region string) bool {
	_, ok := supportedRegionSet[strings.TrimSpace(region)]
	return ok
}

func sameGatewayLease(a, b model.Lease) bool {
	return a.Provider == b.Provider &&
		a.AccountID == b.AccountID &&
		a.Region == b.Region &&
		a.AccessMode == b.AccessMode &&
		a.GatewayID == b.GatewayID
}

func resolveLocation(id string) (model.Location, error) {
	id = strings.TrimSpace(strings.ToLower(id))
	for _, location := range supportedLocations {
		if location.ID == id || strings.ToLower(location.Region) == id {
			return location, nil
		}
	}
	return model.Location{}, fmt.Errorf("unsupported location %q", id)
}

func resolveProvisionAccount(state model.State, ref string) (model.CloudAccount, error) {
	if ref != "" {
		for _, account := range state.Accounts {
			if account.ID == ref || account.Name == ref || account.AWSProfile == ref {
				if account.CredentialSource == "aws_cli" && account.Status == "connected" {
					return account, nil
				}
				return model.CloudAccount{}, fmt.Errorf("account %q is not connected through aws cli", ref)
			}
		}
		return model.CloudAccount{}, fmt.Errorf("account %q not found", ref)
	}

	for i := len(state.Accounts) - 1; i >= 0; i-- {
		account := state.Accounts[i]
		if account.Provider == "aws" && account.CredentialSource == "aws_cli" && account.Status == "connected" {
			return account, nil
		}
	}
	return model.CloudAccount{}, fmt.Errorf("real provisioning requires a connected aws cli account")
}

func locationNameForRegion(region string) string {
	for _, location := range supportedLocations {
		if location.Region == region {
			return location.Name
		}
	}
	return region
}

func buildConnectionBundle(workloadID, accessMode, gatewayID, ip string) model.ConnectionBundle {
	password := stableID(workloadID + "_" + gatewayID)
	if accessMode == "vpn" {
		return model.ConnectionBundle{
			Type:           "vpn",
			ClientEndpoint: fmt.Sprintf("%s.egress.local:51820", gatewayID),
			DownloadURL:    fmt.Sprintf("https://downloads.egress.local/%s/%s.conf", gatewayID, stableID(workloadID)),
			SetupCommand:   fmt.Sprintf("wg-quick up <(curl -fsSL https://downloads.egress.local/%s/%s.conf)", gatewayID, stableID(workloadID)),
			VPNConfig:      fmt.Sprintf("[Interface]\nPrivateKey = <generated-for-%s>\nAddress = 10.24.0.10/32\nDNS = 1.1.1.1\n\n[Peer]\nPublicKey = <gateway-%s>\nAllowedIPs = 0.0.0.0/0\nEndpoint = %s.egress.local:51820\nPersistentKeepalive = 25\n", stableID(workloadID), gatewayID, gatewayID),
		}
	}
	return model.ConnectionBundle{
		Type:           "proxy",
		ProxyURL:       fmt.Sprintf("http://%s:%s@%s.egress.local:3128", workloadID, password, gatewayID),
		ProxyUsername:  workloadID,
		ProxyPassword:  password,
		ClientEndpoint: fmt.Sprintf("%s.egress.local:3128", gatewayID),
		SetupCommand:   fmt.Sprintf("export HTTPS_PROXY=http://%s:%s@%s.egress.local:3128", workloadID, password, gatewayID),
		Env: map[string]string{
			"HTTPS_PROXY": fmt.Sprintf("http://%s:%s@%s.egress.local:3128", workloadID, password, gatewayID),
			"HTTP_PROXY":  fmt.Sprintf("http://%s:%s@%s.egress.local:3128", workloadID, password, gatewayID),
			"NO_PROXY":    "169.254.169.254,localhost,127.0.0.1",
		},
	}
}

func fingerprintPolicy(policy model.PolicySpec) string {
	policy = normalizePolicy(policy)
	raw, _ := json.Marshal(policy)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func stableID(input string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(input))))
	return hex.EncodeToString(sum[:])[:10]
}

func copyPolicies(src map[string]model.PolicyRecord) map[string]model.PolicyRecord {
	dst := make(map[string]model.PolicyRecord, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}
