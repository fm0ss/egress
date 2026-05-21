# Contributing

## Scope

This project is a Go-based control plane and local operator for region-aware egress.

Contributions are welcome in these areas:

- AWS provisioning and teardown
- local VPN lifecycle and safety
- CLI and HTTP API ergonomics
- frontend usability
- test coverage
- documentation

## Development

Run tests before opening a change:

```bash
GOCACHE=/tmp/go-build go test ./...
```

If you are working on the installed runtime path, remember that the service-user copy runs from `/opt/egress`. After local repo changes, refresh that copy with:

```bash
sudo bash ./scripts/install-egressd
```

Then restart the server.

## Safety expectations

This project can create and destroy real AWS resources.

When changing provisioning, cleanup, or local VPN behavior:

- prefer explicit, reversible behavior
- make destructive operations visible in logs/UI
- avoid broad privilege expansion
- keep the `egressd` sudo surface narrow

## Pull requests

Please include:

- a short problem statement
- the proposed behavior change
- any AWS or local-machine side effects
- test coverage or a note explaining why tests were not added

## Style

- keep changes focused
- prefer small composable functions
- preserve the current CLI and API behavior unless the change is intentional and documented
