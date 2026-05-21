# Egress

`Egress` is a local control plane for region-aware, on-demand outbound networking.

This repository now contains the first working slice:

- map-first frontend for picking an exit city
- one-click provisioning of either a proxy endpoint or a VPN config
- declarative policy config in JSON
- local state reconciliation with `plan` and `apply`
- AWS account onboarding via manual records or local AWS CLI profile import
- workload-to-policy attachments
- temporary egress lease issuance from region-scoped warm pools
- real AWS-backed regional gateways for the browser and product-level CLI flows

## What it does today

The current product flow is:

1. Pick a city on the map.
2. Choose `proxy` or `vpn`.
3. Provision the egress endpoint in a connected AWS account.
4. Use the returned proxy URL or VPN config.

Under the hood, the control plane auto-creates the regional policy, attaches the workload, and issues the lease. The lower-level CLI and state model still exist, but the intended user experience is now location-first.

## Runtime model

There are two distinct execution paths in this repo:

- product path: real AWS-backed provisioning through `egress provision` and the web console
- legacy low-level path: local policy/lease simulation through `plan`, `apply`, `attach`, and `lease`

The real product path provisions AWS resources. The legacy low-level lease path still exists for control-plane modeling and testing.

AWS resources created by the real product path:

- `1 EC2 instance` per `account + region + mode`
- `1 security group` per gateway
- the instance root `EBS` volume
- the instance public IPv4

Mode-specific software:

- `vpn`: WireGuard
- `proxy`: Tinyproxy

Reuse behavior:

- repeated provisions in the same `account + region + mode` reuse the same gateway

Cleanup behavior:

- `Disconnect and clean up` tears down local `wg0`, destroys the matching AWS gateway, and removes matching leases from state
- `Cleanup all AWS egress` attempts to destroy all app-managed AWS gateways for connected AWS CLI accounts and removes AWS-backed leases from state

## Prerequisites

Required to build and run:

- Go
- Linux
- `sudo`
- `bash`

Required on the machine where local VPN auto-connect will run:

- `wireguard-tools`
- `iproute2`

Required for AWS-backed provisioning:

- AWS CLI installed on the machine and available as `aws` on `PATH`
- outbound network access from the host running `egress serve` to AWS APIs
- an authenticated AWS CLI profile if using the fast-path import flow

Recommended:

- run the app through the `egressd` service-user installer path documented below
- keep a test AWS account/profile separate from personal or production infrastructure while evaluating the project

## Quick start

Install AWS CLI first if you want real AWS-backed provisioning:

```bash
aws --version
```

If that command fails, install AWS CLI v2 using the official AWS installer or your operating system package manager, then verify `aws --version` works before running `egress serve`.

## CLI binary distribution

This project now includes a GitHub release workflow for prebuilt CLI binaries.

On release tags such as `v0.1.0`, GitHub Actions publishes archives for:

- `linux-amd64`
- `linux-arm64`
- `darwin-amd64`
- `darwin-arm64`
- `windows-amd64`

The release workflow is:

- `.github/workflows/release-cli.yml`

Expected asset names:

- `egress_v0.1.0_linux_amd64.tar.gz`
- `egress_v0.1.0_linux_arm64.tar.gz`
- `egress_v0.1.0_darwin_amd64.tar.gz`
- `egress_v0.1.0_darwin_arm64.tar.gz`
- `egress_v0.1.0_windows_amd64.zip`

Each release also publishes `checksums.txt`.

Once a release exists, users can install the CLI without Go by downloading the matching archive from GitHub Releases and placing `egress` on `PATH`.

Build the CLI:

```bash
GOCACHE=/tmp/go-build go build ./cmd/egress
```

Inspect the sample config in `sample.egress.json`, then run:

```bash
GOCACHE=/tmp/go-build go run ./cmd/egress plan -f sample.egress.json
GOCACHE=/tmp/go-build go run ./cmd/egress apply -f sample.egress.json
GOCACHE=/tmp/go-build go run ./cmd/egress locations
GOCACHE=/tmp/go-build go run ./cmd/egress accounts
GOCACHE=/tmp/go-build go run ./cmd/egress import-aws-cli -profile terraform-playground
GOCACHE=/tmp/go-build go run ./cmd/egress provision -location us-east-1 -access-mode vpn
GOCACHE=/tmp/go-build go run ./cmd/egress local-status
GOCACHE=/tmp/go-build go run ./cmd/egress connect-local -latest
GOCACHE=/tmp/go-build go run ./cmd/egress disconnect-local -latest
GOCACHE=/tmp/go-build go run ./cmd/egress cleanup-all
GOCACHE=/tmp/go-build go run ./cmd/egress attach -workload buildkite/job/1234 -policy github-actions-us
GOCACHE=/tmp/go-build go run ./cmd/egress lease -workload buildkite/job/1234
GOCACHE=/tmp/go-build go run ./cmd/egress state
GOCACHE=/tmp/go-build go run ./cmd/egress serve -addr 127.0.0.1:8080
```

State is written to `.egress/state.json`.

Open `http://127.0.0.1:8080` for the web console. The UI can:

- let a user click a city on the map
- choose between proxy and VPN delivery
- provision a real EC2-backed egress endpoint when the selected AWS CLI profile has the required permissions
- return ready-to-use connection details
- import an AWS account from a local AWS CLI profile when the `aws` binary is available on the server
- save AWS account metadata manually as a fallback

## Install

If you want browser-triggered local VPN connect/disconnect without an interactive sudo password prompt, install the dedicated service-user path.

Files included in this repo:

- `scripts/egress-apply-wg0`
- `scripts/egress-down-wg0`
- `scripts/egressd.sudoers`
- `scripts/install-egressd`

Install with:

```bash
chmod +x ./scripts/install-egressd ./scripts/egress-apply-wg0 ./scripts/egress-down-wg0
sudo bash ./scripts/install-egressd
```

What the installer does:

- creates user `egressd` if missing
- creates `/var/lib/egressd`
- creates `/var/lib/egressd/go-build`
- copies the repo into `/opt/egress`
- installs:
  - `/usr/local/libexec/egress-apply-wg0`
  - `/usr/local/libexec/egress-down-wg0`
  - `/etc/sudoers.d/egressd`
- validates the sudoers file with `visudo -cf`
- copies the invoking user's `~/.aws/config` and `~/.aws/credentials` into `/var/lib/egressd/.aws` when available

Start the server as `egressd`:

```bash
sudo -u egressd bash -lc 'cd /opt/egress && env HOME=/var/lib/egressd AWS_CONFIG_FILE=/var/lib/egressd/.aws/config AWS_SHARED_CREDENTIALS_FILE=/var/lib/egressd/.aws/credentials GOCACHE=/var/lib/egressd/go-build EGRESS_WG_STAGE_DIR=/var/lib/egressd go run ./cmd/egress serve -addr 127.0.0.1:8080'
```

Important installed-copy rule:

- if you change the repo in your working tree and you are running from `/opt/egress`, rerun:

```bash
sudo bash ./scripts/install-egressd
```

- then restart the server

## Connection modes

`proxy` returns:

- a proxy URL with credentials
- environment variables such as `HTTPS_PROXY`
- a shell setup command

`vpn` returns:

- a WireGuard-style config block
- a download URL placeholder
- a quick setup command

The browser `Provision` flow now attempts real AWS provisioning. Older leases already in `.egress/state.json`, or leases produced by the older low-level simulator paths, may still be synthetic.

## Config model

Policies are declared as JSON:

```json
{
  "policies": [
    {
      "name": "github-actions-us",
      "region": "us-east-1",
      "mode": "on_demand",
      "ip_class": "shared",
      "ttl_minutes": 60,
      "destinations": ["api.openai.com", "api.stripe.com"]
    }
  ]
}
```

Supported fields:

- `name`
- `region`
- `fallback_regions`
- `residency`
- `ip_class`
- `mode`
- `destinations`
- `ttl_minutes`

Supported regions in the product path are defined in `internal/control/control.go` and currently cover a broad AWS region set across:

- US
- Canada
- Mexico
- South America
- Europe
- Africa
- Middle East
- APAC

Use:

```bash
GOCACHE=/tmp/go-build go run ./cmd/egress locations
```

to print the exact currently supported location catalog.

## AWS account connections

The console supports two AWS onboarding paths:

- import from a local AWS CLI profile
- manual metadata entry

The AWS CLI import flow runs on the server and uses:

- `aws configure list-profiles`
- `aws sts get-caller-identity --profile <name>`
- `aws configure export-credentials --profile <name> --format env`

That import persists account metadata in state and returns a copyable env export block in the UI. The exported credentials are not stored in `.egress/state.json`.

Real provisioning uses the installed `aws` CLI and launches regional EC2 instances with:

- a security group exposing `3128/tcp` for proxy or `51820/udp` for VPN
- the instance's public IPv4
- Ubuntu user-data that installs and configures `tinyproxy` or `wireguard`

Stored account fields now include:

- `name`
- `aws_account_id`
- `aws_profile`
- `role_arn`
- `principal_arn`
- `credential_source`
- `external_id`
- `default_regions`

The AWS CLI path requires the `aws` binary and a valid local login on the machine running `egress serve`.

The AWS profile needs EC2 permissions including:

- `ec2:DescribeVpcs`
- `ec2:DescribeSubnets`
- `ec2:DescribeImages`
- `ec2:DescribeInstances`
- `ec2:DescribeSecurityGroups`
- `ec2:DescribeAddresses`
- `ec2:DescribeRegions`
- `ec2:CreateSecurityGroup`
- `ec2:AuthorizeSecurityGroupIngress`
- `ec2:RunInstances`
- `ec2:TerminateInstances`
- `ec2:DeleteSecurityGroup`

Recommended additional cleanup permissions:

- `ec2:DisassociateAddress`
- `ec2:ReleaseAddress`

If you are evaluating quickly, broad `AdministratorAccess` on a sandbox account will work, but this project is cleaner with a dedicated provisioner profile or role.

## Local VPN Auto-Connect

What happens when you press `Connect this machine`:

- the app writes the provisioned VPN config to `/var/lib/egressd/wg0.conf`
- `sudo -n /usr/local/libexec/egress-apply-wg0 /var/lib/egressd/wg0.conf` runs
- the wrapper installs `/etc/wireguard/wg0.conf` and brings up `wg0`

When you press `Disconnect and clean up`:

- the app brings down local `wg0`
- destroys the matching AWS gateway for that VPN lease
- removes all state leases that pointed at the same shared gateway

There is also a bulk cleanup path:

- UI button: `Cleanup all AWS egress`
- CLI: `egress cleanup-all`

That path terminates all app-managed AWS gateways it can find for connected AWS CLI accounts and removes AWS leases from local state.

## Safety

Before using this on a non-throwaway AWS account, understand:

- `provision` creates billable AWS resources
- `disconnect-local -latest` can destroy the corresponding AWS gateway when used with lease cleanup
- `cleanup-all` is intentionally destructive for app-managed AWS gateways
- the project identifies managed resources by tags and naming conventions used by this repo

Recommended evaluation workflow:

1. use a sandbox AWS account or isolated IAM profile
2. provision in one region first
3. verify created resources in AWS
4. test `disconnect-local -latest`
5. test `cleanup-all`

If you are unsure what exists, inspect:

```bash
GOCACHE=/tmp/go-build go run ./cmd/egress state
AWS_PAGER="" aws --no-cli-pager ec2 describe-instances --profile <profile> --region us-east-1 --output json
```

## CLI

```txt
egress locations
egress accounts [-state .egress/state.json]
egress import-aws-cli -profile <name> [-name saved-name] [-state .egress/state.json]
egress plan -f config.json [-state .egress/state.json]
egress apply -f config.json [-state .egress/state.json]
egress attach -workload <id> -policy <name|id> [-state .egress/state.json]
egress lease -workload <id> [-access-mode proxy|vpn] [-state .egress/state.json]
egress provision -location <region|location-id> [-account ref] [-access-mode proxy|vpn] [-workload id] [-state .egress/state.json]
egress connect-local (-lease <id> | -latest) [-state .egress/state.json]
egress disconnect-local [-lease <id> | -latest] [-state .egress/state.json]
egress destroy-lease -lease <id> [-state .egress/state.json]
egress cleanup-all [-state .egress/state.json]
egress local-status
egress state [-state .egress/state.json]
egress serve [-addr 127.0.0.1:8080] [-state .egress/state.json]
```

End-to-end product flow from the CLI:

```bash
GOCACHE=/tmp/go-build go run ./cmd/egress import-aws-cli -profile terraform-playground
GOCACHE=/tmp/go-build go run ./cmd/egress provision -location us-east-1 -access-mode vpn
GOCACHE=/tmp/go-build go run ./cmd/egress connect-local -latest
GOCACHE=/tmp/go-build go run ./cmd/egress local-status
GOCACHE=/tmp/go-build go run ./cmd/egress disconnect-local -latest
GOCACHE=/tmp/go-build go run ./cmd/egress cleanup-all
```

## GitHub Actions

This repo now includes reusable composite actions for CI/CD:

- `.github/actions/egress-provision`
- `.github/actions/egress-cleanup`

For external consumers, pin to a release tag instead of `main`:

```yaml
uses: fm0ss/egress/.github/actions/egress-provision@v0.1.0
```

and:

```yaml
uses: fm0ss/egress/.github/actions/egress-cleanup@v0.1.0
```

The intended pipeline model is:

1. authenticate to AWS in GitHub Actions
2. provision a location-specific egress lease
3. run tests through that exit location
4. clean up the lease in an `always()` step

Included example:

- `examples/github-actions/egress-matrix.yml`
- `examples/github-actions/external-usage.yml`

That example shows a matrix job using multiple locations for the same test workload.

Typical usage:

```yaml
- uses: aws-actions/configure-aws-credentials@v4
  with:
    aws-region: us-east-1
    role-to-assume: arn:aws:iam::123456789012:role/EgressControlPlane

- id: egress
  uses: ./.github/actions/egress-provision
  with:
    account_name: ci-aws
    location: eu-west-1
    access_mode: proxy

- name: Run tests through regional egress
  run: |
    curl -fsSL https://ifconfig.me
    ./scripts/run-tests.sh

- if: always()
  uses: ./.github/actions/egress-cleanup
  with:
    lease_id: ${{ steps.egress.outputs.id }}
```

Notes:

- proxy mode is the easiest fit for CI pipelines
- the provision action exports any returned proxy environment variables into `GITHUB_ENV`
- if `aws_profile` is not provided, the action creates a temporary `egress-ci` AWS CLI profile from the ambient `AWS_*` credentials
- the helper scripts now also support `EGRESS_BIN=/path/to/egress` if you want to use a released binary instead of `go run`
- for third-party usage, prefer `aws-actions/configure-aws-credentials` so the workflow receives short-lived AWS credentials

## Terraform Module

This repo now includes a CLI-backed Terraform module:

- `terraform/modules/egress-lease`

External Terraform consumers should pin a git ref:

```hcl
module "regional_egress" {
  source = "git::https://github.com/fm0ss/egress.git//terraform/modules/egress-lease?ref=v0.1.0"

  name             = "regional-test"
  root_dir         = path.root
  aws_profile_name = "terraform-playground"
  account_name     = "ci-aws"
  location         = "eu-west-1"
  access_mode      = "proxy"
}
```

It provisions a lease during `terraform apply` and destroys that lease during `terraform destroy`.

This is useful when teams already standardize pipeline setup with Terraform but still want to consume egress locations through the current CLI implementation.

Example:

```hcl
module "eu_test_egress" {
  source           = "../../terraform/modules/egress-lease"
  name             = "eu-test"
  root_dir         = "${path.root}/../.."
  aws_profile_name = "terraform-playground"
  account_name     = "ci-aws"
  location         = "eu-west-1"
  access_mode      = "proxy"
}
```

Module outputs include:

- `lease`
- `lease_id`
- `public_ip`
- `endpoint`

Important limitations:

- this is not a native Terraform provider
- it depends on local execution of the `egress` CLI
- the machine running Terraform still needs:
  - Go, or a released `egress` binary exposed through `EGRESS_BIN`
  - AWS CLI
  - this repository checkout or a vendored checkout path available to `root_dir`

## Release usage

For external users, the expected consumption model is:

- GitHub Actions:
  - `fm0ss/egress/.github/actions/egress-provision@v0.1.0`
  - `fm0ss/egress/.github/actions/egress-cleanup@v0.1.0`
- Terraform:
  - `git::https://github.com/fm0ss/egress.git//terraform/modules/egress-lease?ref=v0.1.0`

Do not recommend `@main` for automation consumers. Tag a release first, then document and use that tag.

## HTTP API

The web console is backed by a small HTTP API:

- `GET /api/dashboard`
- `POST /api/accounts`
- `POST /api/accounts/import-aws-cli`
- `POST /api/policies`
- `POST /api/attachments`
- `POST /api/leases`
- `POST /api/provision`
- `POST /api/connect-local`
- `POST /api/disconnect-local`
- `POST /api/cleanup-all`

## Repository layout

- `cmd/egress/main.go` is the CLI entrypoint.
- `internal/cli/cli.go` parses commands and renders output.
- `internal/control/control.go` owns validation, planning, apply, attachments, provisioning orchestration, and cleanup orchestration.
- `internal/model/model.go` defines config, state, lease, and cleanup resources.
- `internal/awsprovision/provision.go` owns AWS gateway provisioning and teardown.
- `internal/localvpn/localvpn.go` owns local machine connect/disconnect behavior.
- `internal/server/server.go` exposes the HTTP API and serves the frontend.
- `internal/server/web/index.html` and `internal/server/web/app.js` implement the browser console.

## Product direction

The architecture target remains the same:

- control plane: policy definitions, scheduling, lease issuance, auditability
- data plane: regional gateways that terminate WireGuard or proxy ingress and perform SNAT through stable public IPs

The current simulator is shaped to become that real control plane:

- policies are declarative
- regions are first-class
- leases are explicit resources with TTLs
- warm pool behavior is modeled through region-scoped IP pools

## Current limitations

- proxy disconnect does not currently have a local-machine equivalent to `wg0`; cleanup is AWS-side
- region support is a baked-in catalog, not yet live-discovered per account
- the legacy low-level simulator path still exists alongside the real AWS product path
- there is no persistent database yet; state is a local JSON file
- there is no background reconciler yet for automatic expiry-based teardown

## Next build steps

- replace synthetic gateways with a real regional agent protocol
- add a real backend store instead of a local JSON state file
- turn AWS account records into a real cloud onboarding flow with IAM verification and provisioning
- replace simulated proxy credentials and VPN configs with live gateway provisioning
- support HCL and a Terraform provider after the resource model settles
- enforce destination rules in the eventual data plane
