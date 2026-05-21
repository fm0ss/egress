# Security

## Reporting

If you find a security issue, do not open a public issue with exploit details.

For now, report it privately to the maintainer before public disclosure.

## Security model

This project has two sensitive trust boundaries:

- AWS credentials and resource lifecycle
- local privileged VPN connect/disconnect operations

## Local privilege boundary

The recommended runtime uses:

- service user: `egressd`
- root-owned wrappers:
  - `/usr/local/libexec/egress-apply-wg0`
  - `/usr/local/libexec/egress-down-wg0`
- narrow sudoers entry in `/etc/sudoers.d/egressd`

Do not widen that sudoers scope casually.

## AWS safety

This project can create and destroy EC2 instances, security groups, and related networking resources.

Use:

- a sandbox AWS account for testing, or
- a dedicated least-privilege IAM profile/role

Review cleanup behavior before running:

- `Disconnect and clean up`
- `Cleanup all AWS egress`
- `egress destroy-lease`
- `egress cleanup-all`

## Current limitations

- the project currently relies on a local JSON state file
- the installed runtime copies the repo into `/opt/egress`
- the browser stores the selected active account in local storage

Review those behaviors before running this in a shared environment.
