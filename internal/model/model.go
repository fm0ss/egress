package model

import "time"

type Config struct {
	Policies []PolicySpec `json:"policies"`
}

type PolicySpec struct {
	Name            string   `json:"name"`
	Region          string   `json:"region"`
	FallbackRegions []string `json:"fallback_regions,omitempty"`
	Residency       string   `json:"residency,omitempty"`
	IPClass         string   `json:"ip_class,omitempty"`
	Mode            string   `json:"mode,omitempty"`
	Destinations    []string `json:"destinations,omitempty"`
	TTLMinutes      int      `json:"ttl_minutes,omitempty"`
}

type PolicyRecord struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	Region          string    `json:"region"`
	FallbackRegions []string  `json:"fallback_regions,omitempty"`
	Residency       string    `json:"residency,omitempty"`
	IPClass         string    `json:"ip_class,omitempty"`
	Mode            string    `json:"mode,omitempty"`
	Destinations    []string  `json:"destinations,omitempty"`
	TTLMinutes      int       `json:"ttl_minutes,omitempty"`
	Fingerprint     string    `json:"fingerprint"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type CloudAccount struct {
	ID               string    `json:"id"`
	Name             string    `json:"name"`
	Provider         string    `json:"provider"`
	AWSAccountID     string    `json:"aws_account_id,omitempty"`
	AWSProfile       string    `json:"aws_profile,omitempty"`
	RoleARN          string    `json:"role_arn,omitempty"`
	PrincipalARN     string    `json:"principal_arn,omitempty"`
	ExternalID       string    `json:"external_id,omitempty"`
	CredentialSource string    `json:"credential_source,omitempty"`
	DefaultRegions   []string  `json:"default_regions,omitempty"`
	Status           string    `json:"status"`
	LastVerifiedAt   time.Time `json:"last_verified_at,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type Attachment struct {
	WorkloadID string    `json:"workload_id"`
	PolicyID   string    `json:"policy_id"`
	AttachedAt time.Time `json:"attached_at"`
}

type Lease struct {
	ID         string           `json:"id"`
	WorkloadID string           `json:"workload_id"`
	PolicyID   string           `json:"policy_id"`
	Region     string           `json:"region"`
	Location   string           `json:"location,omitempty"`
	GatewayID  string           `json:"gateway_id"`
	PublicIP   string           `json:"public_ip"`
	Endpoint   string           `json:"endpoint"`
	AccessMode string           `json:"access_mode"`
	Connection ConnectionBundle `json:"connection"`
	Provider   string           `json:"provider,omitempty"`
	AccountID  string           `json:"account_id,omitempty"`
	Status     string           `json:"status,omitempty"`
	Resources  CloudResources   `json:"resources,omitempty"`
	ExpiresAt  time.Time        `json:"expires_at"`
	IssuedAt   time.Time        `json:"issued_at"`
}

type CloudResources struct {
	VpcID           string `json:"vpc_id,omitempty"`
	SubnetID        string `json:"subnet_id,omitempty"`
	InstanceID      string `json:"instance_id,omitempty"`
	SecurityGroupID string `json:"security_group_id,omitempty"`
	AllocationID    string `json:"allocation_id,omitempty"`
	AssociationID   string `json:"association_id,omitempty"`
}

type ConnectionBundle struct {
	Type           string            `json:"type"`
	ProxyURL       string            `json:"proxy_url,omitempty"`
	ProxyUsername  string            `json:"proxy_username,omitempty"`
	ProxyPassword  string            `json:"proxy_password,omitempty"`
	VPNConfig      string            `json:"vpn_config,omitempty"`
	Env            map[string]string `json:"env,omitempty"`
	SetupCommand   string            `json:"setup_command,omitempty"`
	DownloadURL    string            `json:"download_url,omitempty"`
	ClientEndpoint string            `json:"client_endpoint,omitempty"`
}

type Location struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Region      string  `json:"region"`
	CountryCode string  `json:"country_code"`
	Latitude    float64 `json:"latitude"`
	Longitude   float64 `json:"longitude"`
}

type State struct {
	Version     int                     `json:"version"`
	Policies    map[string]PolicyRecord `json:"policies"`
	Accounts    []CloudAccount          `json:"accounts"`
	Attachments []Attachment            `json:"attachments"`
	Leases      []Lease                 `json:"leases"`
}

type Plan struct {
	Creates []PolicySpec   `json:"creates"`
	Updates []Change       `json:"updates"`
	Deletes []PolicyRecord `json:"deletes"`
}

type Change struct {
	Before PolicyRecord `json:"before"`
	After  PolicySpec   `json:"after"`
}

type CleanupResult struct {
	Scope                 string `json:"scope"`
	AccountID             string `json:"account_id,omitempty"`
	LeaseID               string `json:"lease_id,omitempty"`
	GatewayID             string `json:"gateway_id,omitempty"`
	Region                string `json:"region,omitempty"`
	AccessMode            string `json:"access_mode,omitempty"`
	RemovedLeaseCount     int    `json:"removed_lease_count"`
	DestroyedGatewayCount int    `json:"destroyed_gateway_count"`
	ReleasedAddressCount  int    `json:"released_address_count"`
	DeletedSecurityGroups int    `json:"deleted_security_group_count"`
	Detail                string `json:"detail,omitempty"`
}
