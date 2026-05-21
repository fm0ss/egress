package awsprovision

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"egress/internal/awscli"
	"egress/internal/model"
)

type Result struct {
	AccountID string
	Lease     model.Lease
}

type CleanupSummary struct {
	DestroyedGatewayCount int
	ReleasedAddressCount  int
	DeletedSecurityGroups int
}

type VPC struct {
	VpcID string `json:"VpcId"`
}

type Subnet struct {
	SubnetID            string `json:"SubnetId"`
	AvailabilityZone    string `json:"AvailabilityZone"`
	MapPublicIPOnLaunch bool   `json:"MapPublicIpOnLaunch"`
	DefaultForAZ        bool   `json:"DefaultForAz"`
}

type Image struct {
	ImageID      string `json:"ImageId"`
	CreationDate string `json:"CreationDate"`
}

type SecurityGroup struct {
	GroupID string `json:"GroupId"`
}

type Instance struct {
	InstanceID      string `json:"InstanceId"`
	SubnetID        string `json:"SubnetId,omitempty"`
	VpcID           string `json:"VpcId,omitempty"`
	PublicIPAddress string `json:"PublicIpAddress,omitempty"`
	State           struct {
		Name string `json:"Name"`
	} `json:"State"`
	Tags []Tag `json:"Tags,omitempty"`
}

type Tag struct {
	Key   string `json:"Key"`
	Value string `json:"Value"`
}

type Address struct {
	AllocationID  string `json:"AllocationId"`
	AssociationID string `json:"AssociationId,omitempty"`
	InstanceID    string `json:"InstanceId,omitempty"`
	Tags          []Tag  `json:"Tags,omitempty"`
}

type RegionInfo struct {
	RegionName string `json:"RegionName"`
}

type gatewayMetadata struct {
	Name             string
	ProxyUsername    string
	ProxyPassword    string
	ServerPublicKey  string
	ClientPrivateKey string
	Mode             string
}

func Provision(ctx context.Context, account model.CloudAccount, location model.Location, accessMode, workloadID, leaseID string, expiresAt time.Time) (Result, error) {
	if strings.TrimSpace(account.AWSProfile) == "" {
		return Result{}, fmt.Errorf("account %q is not connected through aws cli", account.Name)
	}
	if accessMode != "proxy" && accessMode != "vpn" {
		return Result{}, fmt.Errorf("unsupported access mode %q", accessMode)
	}

	name := gatewayName(account, location.Region, accessMode)
	instance, resources, metadata, err := ensureGateway(ctx, account.AWSProfile, location.Region, accessMode, name)
	if err != nil {
		return Result{}, err
	}
	if instance.PublicIPAddress == "" {
		return Result{}, fmt.Errorf("gateway %s is running but has no public ip", instance.InstanceID)
	}

	conn := connectionFromGateway(metadata, accessMode, instance.PublicIPAddress)
	lease := model.Lease{
		ID:         leaseID,
		WorkloadID: workloadID,
		Region:     location.Region,
		Location:   location.Name,
		GatewayID:  instance.InstanceID,
		PublicIP:   instance.PublicIPAddress,
		Endpoint:   conn.ClientEndpoint,
		AccessMode: accessMode,
		Connection: conn,
		Provider:   "aws",
		AccountID:  account.ID,
		Status:     "ready",
		Resources:  resources,
		ExpiresAt:  expiresAt.UTC(),
		IssuedAt:   time.Now().UTC(),
	}

	return Result{AccountID: account.AWSAccountID, Lease: lease}, nil
}

func DestroyGateway(ctx context.Context, account model.CloudAccount, lease model.Lease) (CleanupSummary, error) {
	if strings.TrimSpace(account.AWSProfile) == "" {
		return CleanupSummary{}, fmt.Errorf("account %q is not connected through aws cli", account.Name)
	}

	summary := CleanupSummary{}
	profile := account.AWSProfile
	region := lease.Region

	if lease.Resources.AssociationID != "" {
		if err := ignoreMissing(disassociateAddress(ctx, profile, region, lease.Resources.AssociationID)); err != nil {
			return summary, err
		}
	}
	if lease.Resources.AllocationID != "" {
		if err := ignoreMissing(releaseAddress(ctx, profile, region, lease.Resources.AllocationID)); err != nil {
			return summary, err
		}
		summary.ReleasedAddressCount++
	}
	if lease.Resources.InstanceID != "" {
		if err := ignoreMissing(terminateInstance(ctx, profile, region, lease.Resources.InstanceID)); err != nil {
			return summary, err
		}
		if err := ignoreMissing(waitInstanceTerminated(ctx, profile, region, lease.Resources.InstanceID)); err != nil {
			return summary, err
		}
		summary.DestroyedGatewayCount++
	}
	if lease.Resources.SecurityGroupID != "" {
		if err := ignoreMissing(deleteSecurityGroup(ctx, profile, region, lease.Resources.SecurityGroupID)); err != nil {
			return summary, err
		}
		summary.DeletedSecurityGroups++
	}

	return summary, nil
}

func CleanupAccountResources(ctx context.Context, account model.CloudAccount) (CleanupSummary, error) {
	if strings.TrimSpace(account.AWSProfile) == "" {
		return CleanupSummary{}, fmt.Errorf("account %q is not connected through aws cli", account.Name)
	}

	regions, err := describeRegions(ctx, account.AWSProfile)
	if err != nil {
		return CleanupSummary{}, err
	}

	summary := CleanupSummary{}
	for _, region := range regions {
		regionSummary, err := cleanupRegion(ctx, account.AWSProfile, region)
		if err != nil {
			return summary, err
		}
		summary.DestroyedGatewayCount += regionSummary.DestroyedGatewayCount
		summary.ReleasedAddressCount += regionSummary.ReleasedAddressCount
		summary.DeletedSecurityGroups += regionSummary.DeletedSecurityGroups
	}
	return summary, nil
}

func ensureGateway(ctx context.Context, profile, region, accessMode, name string) (Instance, model.CloudResources, gatewayMetadata, error) {
	existing, found, err := findGatewayInstance(ctx, profile, region, name)
	if err != nil {
		return Instance{}, model.CloudResources{}, gatewayMetadata{}, err
	}
	if found {
		if existing.State.Name == "stopped" {
			if err := startInstance(ctx, profile, region, existing.InstanceID); err != nil {
				return Instance{}, model.CloudResources{}, gatewayMetadata{}, err
			}
		}
		if existing.State.Name != "running" {
			if err := waitInstanceRunning(ctx, profile, region, existing.InstanceID); err != nil {
				return Instance{}, model.CloudResources{}, gatewayMetadata{}, err
			}
		}
		refreshed, err := describeInstance(ctx, profile, region, existing.InstanceID)
		if err != nil {
			return Instance{}, model.CloudResources{}, gatewayMetadata{}, err
		}
		metadata := metadataFromTags(refreshed.Tags)
		return refreshed, model.CloudResources{
			VpcID:           refreshed.VpcID,
			SubnetID:        refreshed.SubnetID,
			InstanceID:      refreshed.InstanceID,
			SecurityGroupID: firstTagValue(refreshed.Tags, "EgressSecurityGroupId"),
		}, metadata, nil
	}

	vpc, err := defaultVPC(ctx, profile, region)
	if err != nil {
		return Instance{}, model.CloudResources{}, gatewayMetadata{}, err
	}
	subnet, err := defaultSubnet(ctx, profile, region, vpc.VpcID)
	if err != nil {
		return Instance{}, model.CloudResources{}, gatewayMetadata{}, err
	}
	imageID, err := latestUbuntuAMI(ctx, profile, region)
	if err != nil {
		return Instance{}, model.CloudResources{}, gatewayMetadata{}, err
	}

	metadata, userDataPath, err := buildGateway(accessMode, name)
	if err != nil {
		return Instance{}, model.CloudResources{}, gatewayMetadata{}, err
	}
	defer os.Remove(userDataPath)

	securityGroupID, createdSecurityGroup, err := ensureSecurityGroup(ctx, profile, region, vpc.VpcID, name, accessMode)
	if err != nil {
		return Instance{}, model.CloudResources{}, gatewayMetadata{}, err
	}

	var createdInstanceID string
	defer func() {
		if err == nil {
			return
		}
		if createdInstanceID != "" {
			_ = terminateInstance(context.Background(), profile, region, createdInstanceID)
			_ = waitInstanceTerminated(context.Background(), profile, region, createdInstanceID)
		}
		if createdSecurityGroup {
			_ = deleteSecurityGroup(context.Background(), profile, region, securityGroupID)
		}
	}()

	instance, err := runInstance(ctx, profile, region, imageID, subnet.SubnetID, securityGroupID, name, userDataPath, metadata)
	if err != nil {
		return Instance{}, model.CloudResources{}, gatewayMetadata{}, err
	}
	createdInstanceID = instance.InstanceID

	if err := waitInstanceRunning(ctx, profile, region, instance.InstanceID); err != nil {
		return Instance{}, model.CloudResources{}, gatewayMetadata{}, err
	}

	refreshed, err := describeInstance(ctx, profile, region, instance.InstanceID)
	if err != nil {
		return Instance{}, model.CloudResources{}, gatewayMetadata{}, err
	}
	if refreshed.PublicIPAddress == "" {
		return Instance{}, model.CloudResources{}, gatewayMetadata{}, fmt.Errorf("instance %s has no public ip in subnet %s", refreshed.InstanceID, refreshed.SubnetID)
	}

	return refreshed, model.CloudResources{
		VpcID:           refreshed.VpcID,
		SubnetID:        refreshed.SubnetID,
		InstanceID:      refreshed.InstanceID,
		SecurityGroupID: securityGroupID,
	}, metadata, nil
}

func buildGateway(accessMode, name string) (gatewayMetadata, string, error) {
	if accessMode == "vpn" {
		return vpnGateway(name)
	}
	return proxyGateway(name)
}

func proxyGateway(name string) (gatewayMetadata, string, error) {
	username := "egress"
	password := safeNameSuffix(name + "-proxy-secret")
	script := fmt.Sprintf(`#!/bin/bash
set -euxo pipefail
export DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get install -y tinyproxy
cat >/etc/tinyproxy/tinyproxy.conf <<'EOF'
User tinyproxy
Group tinyproxy
Port 3128
Timeout 600
DefaultErrorFile "/usr/share/tinyproxy/default.html"
StatFile "/usr/share/tinyproxy/stats.html"
LogLevel Info
PidFile "/run/tinyproxy/tinyproxy.pid"
MaxClients 100
Allow 0.0.0.0/0
ViaProxyName "egress"
BasicAuth %s %s
EOF
systemctl restart tinyproxy
systemctl enable tinyproxy
`, username, password)
	path, err := writeTempScript(script)
	if err != nil {
		return gatewayMetadata{}, "", err
	}
	return gatewayMetadata{
		Name:          name,
		Mode:          "proxy",
		ProxyUsername: username,
		ProxyPassword: password,
	}, path, nil
}

func vpnGateway(name string) (gatewayMetadata, string, error) {
	serverPriv, serverPub, err := generateWGKeypair()
	if err != nil {
		return gatewayMetadata{}, "", err
	}
	clientPriv, clientPub, err := generateWGKeypair()
	if err != nil {
		return gatewayMetadata{}, "", err
	}
	script := fmt.Sprintf(`#!/bin/bash
set -euxo pipefail
export DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get install -y wireguard iptables
sysctl -w net.ipv4.ip_forward=1
echo 'net.ipv4.ip_forward=1' >/etc/sysctl.d/99-egress.conf
mkdir -p /etc/wireguard
chmod 700 /etc/wireguard
cat >/etc/wireguard/wg0.conf <<'EOF'
[Interface]
Address = 10.24.0.1/24
ListenPort = 51820
PrivateKey = %s
PostUp = IFACE=$(ip route list default | awk '{print $5}' | head -n1); iptables -t nat -A POSTROUTING -o $IFACE -j MASQUERADE
PostDown = IFACE=$(ip route list default | awk '{print $5}' | head -n1); iptables -t nat -D POSTROUTING -o $IFACE -j MASQUERADE

[Peer]
PublicKey = %s
AllowedIPs = 10.24.0.2/32
EOF
chmod 600 /etc/wireguard/wg0.conf
systemctl enable wg-quick@wg0
systemctl restart wg-quick@wg0
`, serverPriv, clientPub)
	path, err := writeTempScript(script)
	if err != nil {
		return gatewayMetadata{}, "", err
	}
	return gatewayMetadata{
		Name:             name,
		Mode:             "vpn",
		ServerPublicKey:  serverPub,
		ClientPrivateKey: clientPriv,
	}, path, nil
}

func connectionFromGateway(metadata gatewayMetadata, accessMode, publicIP string) model.ConnectionBundle {
	if accessMode == "vpn" {
		config := fmt.Sprintf(`[Interface]
PrivateKey = %s
Address = 10.24.0.2/24
PostUp = sh -c 'sysctl -n net.ipv6.conf.all.disable_ipv6 >/run/egress-wg0-ipv6-all.before; sysctl -n net.ipv6.conf.default.disable_ipv6 >/run/egress-wg0-ipv6-default.before; sysctl -w net.ipv6.conf.all.disable_ipv6=1; sysctl -w net.ipv6.conf.default.disable_ipv6=1'
PostDown = sh -c 'ALL=0; DEF=0; test -f /run/egress-wg0-ipv6-all.before && ALL=$(cat /run/egress-wg0-ipv6-all.before); test -f /run/egress-wg0-ipv6-default.before && DEF=$(cat /run/egress-wg0-ipv6-default.before); sysctl -w net.ipv6.conf.all.disable_ipv6=$ALL; sysctl -w net.ipv6.conf.default.disable_ipv6=$DEF; rm -f /run/egress-wg0-ipv6-all.before /run/egress-wg0-ipv6-default.before'

[Peer]
PublicKey = %s
AllowedIPs = 0.0.0.0/0
Endpoint = %s:51820
PersistentKeepalive = 25
`, metadata.ClientPrivateKey, metadata.ServerPublicKey, publicIP)
		return model.ConnectionBundle{
			Type:           "vpn",
			VPNConfig:      config,
			ClientEndpoint: fmt.Sprintf("%s:51820", publicIP),
			SetupCommand:   vpnSetupCommand(config),
		}
	}

	proxyURL := fmt.Sprintf("http://%s:%s@%s:3128", metadata.ProxyUsername, metadata.ProxyPassword, publicIP)
	return model.ConnectionBundle{
		Type:           "proxy",
		ProxyURL:       proxyURL,
		ProxyUsername:  metadata.ProxyUsername,
		ProxyPassword:  metadata.ProxyPassword,
		ClientEndpoint: fmt.Sprintf("%s:3128", publicIP),
		SetupCommand:   fmt.Sprintf("export HTTPS_PROXY=%s", proxyURL),
		Env: map[string]string{
			"HTTPS_PROXY": proxyURL,
			"HTTP_PROXY":  proxyURL,
			"NO_PROXY":    "169.254.169.254,localhost,127.0.0.1",
		},
	}
}

func vpnSetupCommand(config string) string {
	return fmt.Sprintf(`sudo bash -c 'set -euo pipefail
mkdir -p /etc/wireguard
cat >/etc/wireguard/wg0.conf <<'"'"'EOF'"'"'
%s
EOF
chmod 600 /etc/wireguard/wg0.conf
if ip link show wg0 >/dev/null 2>&1; then
  wg-quick down wg0 || true
fi
wg-quick up wg0'`, strings.TrimSpace(config))
}

func defaultVPC(ctx context.Context, profile, region string) (VPC, error) {
	var payload struct {
		VPCs []VPC `json:"Vpcs"`
	}
	if err := runJSON(ctx, &payload, "ec2", "describe-vpcs", "--profile", profile, "--region", region, "--filters", "Name=isDefault,Values=true"); err != nil {
		return VPC{}, err
	}
	if len(payload.VPCs) == 0 {
		return VPC{}, fmt.Errorf("no default vpc found in %s", region)
	}
	return payload.VPCs[0], nil
}

func defaultSubnet(ctx context.Context, profile, region, vpcID string) (Subnet, error) {
	var payload struct {
		Subnets []Subnet `json:"Subnets"`
	}
	if err := runJSON(ctx, &payload, "ec2", "describe-subnets", "--profile", profile, "--region", region, "--filters", "Name=vpc-id,Values="+vpcID); err != nil {
		return Subnet{}, err
	}
	if len(payload.Subnets) == 0 {
		return Subnet{}, fmt.Errorf("no subnet found in vpc %s", vpcID)
	}
	sort.Slice(payload.Subnets, func(i, j int) bool {
		if payload.Subnets[i].DefaultForAZ != payload.Subnets[j].DefaultForAZ {
			return payload.Subnets[i].DefaultForAZ
		}
		return payload.Subnets[i].SubnetID < payload.Subnets[j].SubnetID
	})
	return payload.Subnets[0], nil
}

func latestUbuntuAMI(ctx context.Context, profile, region string) (string, error) {
	var payload struct {
		Images []Image `json:"Images"`
	}
	if err := runJSON(ctx, &payload, "ec2", "describe-images",
		"--profile", profile,
		"--region", region,
		"--owners", "099720109477",
		"--filters",
		"Name=name,Values=ubuntu/images/hvm-ssd-gp3/ubuntu-noble-24.04-amd64-server-*",
		"Name=state,Values=available",
		"Name=architecture,Values=x86_64",
	); err != nil {
		return "", err
	}
	if len(payload.Images) == 0 {
		return "", fmt.Errorf("no ubuntu ami found in %s", region)
	}
	sort.Slice(payload.Images, func(i, j int) bool { return payload.Images[i].CreationDate > payload.Images[j].CreationDate })
	return payload.Images[0].ImageID, nil
}

func findGatewayInstance(ctx context.Context, profile, region, name string) (Instance, bool, error) {
	var payload struct {
		Reservations []struct {
			Instances []Instance `json:"Instances"`
		} `json:"Reservations"`
	}
	if err := runJSON(ctx, &payload, "ec2", "describe-instances",
		"--profile", profile,
		"--region", region,
		"--filters",
		"Name=tag:Name,Values="+name,
		"Name=instance-state-name,Values=pending,running,stopping,stopped",
	); err != nil {
		return Instance{}, false, err
	}
	for _, reservation := range payload.Reservations {
		for _, instance := range reservation.Instances {
			return instance, true, nil
		}
	}
	return Instance{}, false, nil
}

func describeInstance(ctx context.Context, profile, region, instanceID string) (Instance, error) {
	var payload struct {
		Reservations []struct {
			Instances []Instance `json:"Instances"`
		} `json:"Reservations"`
	}
	if err := runJSON(ctx, &payload, "ec2", "describe-instances", "--profile", profile, "--region", region, "--instance-ids", instanceID); err != nil {
		return Instance{}, err
	}
	for _, reservation := range payload.Reservations {
		for _, instance := range reservation.Instances {
			return instance, nil
		}
	}
	return Instance{}, fmt.Errorf("instance %s not found", instanceID)
}

func ensureSecurityGroup(ctx context.Context, profile, region, vpcID, name, accessMode string) (string, bool, error) {
	groupID, err := lookupSecurityGroup(ctx, profile, region, name, vpcID)
	if err == nil && groupID != "" {
		return groupID, false, nil
	}

	var payload SecurityGroup
	if err := runJSON(ctx, &payload, "ec2", "create-security-group",
		"--profile", profile,
		"--region", region,
		"--group-name", name,
		"--description", "Shared Egress "+accessMode+" gateway",
		"--vpc-id", vpcID,
	); err != nil {
		if strings.Contains(err.Error(), "InvalidGroup.Duplicate") {
			groupID, lookupErr := lookupSecurityGroup(ctx, profile, region, name, vpcID)
			return groupID, false, lookupErr
		}
		return "", false, err
	}

	port := "3128"
	protocol := "tcp"
	if accessMode == "vpn" {
		port = "51820"
		protocol = "udp"
	}
	if _, err := awscliCommand(ctx, "ec2", "authorize-security-group-ingress",
		"--profile", profile,
		"--region", region,
		"--group-id", payload.GroupID,
		"--ip-permissions", fmt.Sprintf("IpProtocol=%s,FromPort=%s,ToPort=%s,IpRanges=[{CidrIp=0.0.0.0/0,Description=shared-egress}]", protocol, port, port),
	); err != nil && !strings.Contains(err.Error(), "InvalidPermission.Duplicate") {
		return "", true, err
	}
	return payload.GroupID, true, nil
}

func lookupSecurityGroup(ctx context.Context, profile, region, name, vpcID string) (string, error) {
	var payload struct {
		SecurityGroups []SecurityGroup `json:"SecurityGroups"`
	}
	if err := runJSON(ctx, &payload, "ec2", "describe-security-groups",
		"--profile", profile,
		"--region", region,
		"--filters",
		"Name=group-name,Values="+name,
		"Name=vpc-id,Values="+vpcID,
	); err != nil {
		return "", err
	}
	if len(payload.SecurityGroups) == 0 {
		return "", fmt.Errorf("security group %s not found", name)
	}
	return payload.SecurityGroups[0].GroupID, nil
}

func runInstance(ctx context.Context, profile, region, imageID, subnetID, securityGroupID, name, userDataPath string, metadata gatewayMetadata) (Instance, error) {
	var payload struct {
		Instances []Instance `json:"Instances"`
	}
	tags := []string{
		"{Key=Name,Value=" + name + "}",
		"{Key=EgressMode,Value=" + metadata.Mode + "}",
		"{Key=EgressSecurityGroupId,Value=" + securityGroupID + "}",
	}
	if metadata.ProxyUsername != "" {
		tags = append(tags, "{Key=EgressProxyUsername,Value="+metadata.ProxyUsername+"}")
		tags = append(tags, "{Key=EgressProxyPassword,Value="+metadata.ProxyPassword+"}")
	}
	if metadata.ServerPublicKey != "" {
		tags = append(tags, "{Key=EgressServerPublicKey,Value="+metadata.ServerPublicKey+"}")
		tags = append(tags, "{Key=EgressClientPrivateKey,Value="+metadata.ClientPrivateKey+"}")
	}

	if err := runJSON(ctx, &payload, "ec2", "run-instances",
		"--profile", profile,
		"--region", region,
		"--image-id", imageID,
		"--instance-type", "t3.micro",
		"--subnet-id", subnetID,
		"--associate-public-ip-address",
		"--security-group-ids", securityGroupID,
		"--user-data", "fileb://"+userDataPath,
		"--tag-specifications", "ResourceType=instance,Tags=["+strings.Join(tags, ",")+"]",
		"--metadata-options", "HttpTokens=required,HttpEndpoint=enabled",
		"--count", "1",
	); err != nil {
		return Instance{}, err
	}
	if len(payload.Instances) == 0 {
		return Instance{}, fmt.Errorf("run-instances returned no instance")
	}
	return payload.Instances[0], nil
}

func startInstance(ctx context.Context, profile, region, instanceID string) error {
	_, err := awscliCommand(ctx, "ec2", "start-instances", "--profile", profile, "--region", region, "--instance-ids", instanceID)
	return err
}

func waitInstanceRunning(ctx context.Context, profile, region, instanceID string) error {
	_, err := awscliCommand(ctx, "ec2", "wait", "instance-running", "--profile", profile, "--region", region, "--instance-ids", instanceID)
	return err
}

func terminateInstance(ctx context.Context, profile, region, instanceID string) error {
	_, err := awscliCommand(ctx, "ec2", "terminate-instances", "--profile", profile, "--region", region, "--instance-ids", instanceID)
	return err
}

func waitInstanceTerminated(ctx context.Context, profile, region, instanceID string) error {
	_, err := awscliCommand(ctx, "ec2", "wait", "instance-terminated", "--profile", profile, "--region", region, "--instance-ids", instanceID)
	return err
}

func deleteSecurityGroup(ctx context.Context, profile, region, securityGroupID string) error {
	_, err := awscliCommand(ctx, "ec2", "delete-security-group", "--profile", profile, "--region", region, "--group-id", securityGroupID)
	return err
}

func disassociateAddress(ctx context.Context, profile, region, associationID string) error {
	_, err := awscliCommand(ctx, "ec2", "disassociate-address", "--profile", profile, "--region", region, "--association-id", associationID)
	return err
}

func releaseAddress(ctx context.Context, profile, region, allocationID string) error {
	_, err := awscliCommand(ctx, "ec2", "release-address", "--profile", profile, "--region", region, "--allocation-id", allocationID)
	return err
}

func metadataFromTags(tags []Tag) gatewayMetadata {
	return gatewayMetadata{
		Name:             firstTagValue(tags, "Name"),
		Mode:             firstTagValue(tags, "EgressMode"),
		ProxyUsername:    firstTagValue(tags, "EgressProxyUsername"),
		ProxyPassword:    firstTagValue(tags, "EgressProxyPassword"),
		ServerPublicKey:  firstTagValue(tags, "EgressServerPublicKey"),
		ClientPrivateKey: firstTagValue(tags, "EgressClientPrivateKey"),
	}
}

func firstTagValue(tags []Tag, key string) string {
	for _, tag := range tags {
		if tag.Key == key {
			return tag.Value
		}
	}
	return ""
}

func generateWGKeypair() (string, string, error) {
	curve := ecdh.X25519()
	privateKey, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", err
	}
	return base64.StdEncoding.EncodeToString(privateKey.Bytes()), base64.StdEncoding.EncodeToString(privateKey.PublicKey().Bytes()), nil
}

func writeTempScript(contents string) (string, error) {
	file, err := os.CreateTemp("", "egress-userdata-*.sh")
	if err != nil {
		return "", err
	}
	defer file.Close()
	if _, err := file.WriteString(contents); err != nil {
		return "", err
	}
	return file.Name(), nil
}

func awscliCommand(ctx context.Context, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	return runCLI(ctx, args...)
}

func runJSON(ctx context.Context, target any, args ...string) error {
	output, err := awscliCommand(ctx, append(args, "--output", "json")...)
	if err != nil {
		return err
	}
	if err := json.Unmarshal([]byte(output), target); err != nil {
		return fmt.Errorf("parse aws cli json: %w", err)
	}
	return nil
}

func runCLI(ctx context.Context, args ...string) (string, error) {
	binary := ""
	if envBinary := strings.TrimSpace(os.Getenv("EGRESS_AWS_CLI")); envBinary != "" {
		binary = envBinary
	} else {
		binary = filepath.Join(".", "aws", "dist", "aws")
		if _, err := os.Stat(binary); err != nil {
			binary = "aws"
		}
	}
	output, err := awscli.Run(ctx, binary, args...)
	if err != nil {
		return "", err
	}
	return output, nil
}

func describeRegions(ctx context.Context, profile string) ([]string, error) {
	var payload struct {
		Regions []RegionInfo `json:"Regions"`
	}
	if err := runJSON(ctx, &payload, "ec2", "describe-regions", "--profile", profile, "--region", "us-east-1"); err != nil {
		return nil, err
	}
	regions := make([]string, 0, len(payload.Regions))
	for _, region := range payload.Regions {
		if strings.TrimSpace(region.RegionName) != "" {
			regions = append(regions, region.RegionName)
		}
	}
	sort.Strings(regions)
	return regions, nil
}

func cleanupRegion(ctx context.Context, profile, region string) (CleanupSummary, error) {
	summary := CleanupSummary{}

	instances, err := findManagedInstances(ctx, profile, region)
	if err != nil {
		return summary, err
	}
	if len(instances) > 0 {
		ids := make([]string, 0, len(instances))
		for _, instance := range instances {
			ids = append(ids, instance.InstanceID)
		}
		if _, err := awscliCommand(ctx, append([]string{"ec2", "terminate-instances", "--profile", profile, "--region", region, "--instance-ids"}, ids...)...); err != nil {
			return summary, err
		}
		if _, err := awscliCommand(ctx, append([]string{"ec2", "wait", "instance-terminated", "--profile", profile, "--region", region, "--instance-ids"}, ids...)...); err != nil {
			return summary, err
		}
		summary.DestroyedGatewayCount += len(ids)
	}

	addresses, err := findManagedAddresses(ctx, profile, region)
	if err != nil {
		return summary, err
	}
	for _, address := range addresses {
		if address.AssociationID != "" {
			if err := ignoreMissing(disassociateAddress(ctx, profile, region, address.AssociationID)); err != nil {
				return summary, err
			}
		}
		if address.AllocationID != "" {
			if err := ignoreMissing(releaseAddress(ctx, profile, region, address.AllocationID)); err != nil {
				return summary, err
			}
			summary.ReleasedAddressCount++
		}
	}

	groupIDs, err := findManagedSecurityGroups(ctx, profile, region)
	if err != nil {
		return summary, err
	}
	for _, groupID := range groupIDs {
		if err := ignoreMissing(deleteSecurityGroup(ctx, profile, region, groupID)); err != nil {
			return summary, err
		}
		summary.DeletedSecurityGroups++
	}

	return summary, nil
}

func findManagedInstances(ctx context.Context, profile, region string) ([]Instance, error) {
	var payload struct {
		Reservations []struct {
			Instances []Instance `json:"Instances"`
		} `json:"Reservations"`
	}
	if err := runJSON(ctx, &payload, "ec2", "describe-instances",
		"--profile", profile,
		"--region", region,
		"--filters",
		"Name=tag:EgressMode,Values=*",
		"Name=instance-state-name,Values=pending,running,stopping,stopped",
	); err != nil {
		return nil, err
	}
	var instances []Instance
	for _, reservation := range payload.Reservations {
		instances = append(instances, reservation.Instances...)
	}
	return instances, nil
}

func findManagedAddresses(ctx context.Context, profile, region string) ([]Address, error) {
	var payload struct {
		Addresses []Address `json:"Addresses"`
	}
	if err := runJSON(ctx, &payload, "ec2", "describe-addresses", "--profile", profile, "--region", region); err != nil {
		return nil, err
	}
	filtered := make([]Address, 0, len(payload.Addresses))
	for _, address := range payload.Addresses {
		if hasEgressTag(address.Tags) {
			filtered = append(filtered, address)
		}
	}
	return filtered, nil
}

func findManagedSecurityGroups(ctx context.Context, profile, region string) ([]string, error) {
	var payload struct {
		SecurityGroups []struct {
			GroupID   string `json:"GroupId"`
			GroupName string `json:"GroupName"`
		} `json:"SecurityGroups"`
	}
	if err := runJSON(ctx, &payload, "ec2", "describe-security-groups", "--profile", profile, "--region", region); err != nil {
		return nil, err
	}
	groupIDs := make([]string, 0, len(payload.SecurityGroups))
	for _, group := range payload.SecurityGroups {
		if strings.HasPrefix(group.GroupName, "egress-gateway-") {
			groupIDs = append(groupIDs, group.GroupID)
		}
	}
	return groupIDs, nil
}

func hasEgressTag(tags []Tag) bool {
	for _, tag := range tags {
		if tag.Key == "Name" && strings.HasPrefix(tag.Value, "egress-") {
			return true
		}
	}
	return false
}

func ignoreMissing(err error) error {
	if err == nil {
		return nil
	}
	text := err.Error()
	if strings.Contains(text, "InvalidInstanceID.NotFound") ||
		strings.Contains(text, "InvalidAllocationID.NotFound") ||
		strings.Contains(text, "InvalidAssociationID.NotFound") ||
		strings.Contains(text, "InvalidGroup.NotFound") ||
		strings.Contains(text, "AuthFailure") && strings.Contains(text, "not found") {
		return nil
	}
	return err
}

func gatewayName(account model.CloudAccount, region, accessMode string) string {
	suffix := account.AWSAccountID
	if len(suffix) > 6 {
		suffix = suffix[len(suffix)-6:]
	}
	return safeNameSuffix(fmt.Sprintf("egress-gateway-%s-%s-%s", suffix, region, accessMode))
}

func safeNameSuffix(value string) string {
	value = strings.ToLower(value)
	replacer := strings.NewReplacer("/", "-", "_", "-", " ", "-")
	value = replacer.Replace(value)
	if len(value) > 64 {
		value = value[len(value)-64:]
	}
	return value
}
