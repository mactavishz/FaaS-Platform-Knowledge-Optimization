# Integration Tests

This directory contains integration tests in two categories:

- Platform behavior scenarios (autoscaler/callgraph)
- Workflow functionality scenarios (no autoscaler, no callgraph)

## Prerequisites

- Vagrant VM is running: `vagrant up tinyfaas` or `vagrant ssh faasd`
- Go toolchain installed
- Docker available in the tinyfaas VM (handled by provisioning)

## Platform Behavior Scenarios

The platform behavior suite rebuilds tinyFaaS with one of these profiles in `tests/integration/env/`:

- `no-autoscaler-no-callgraph.env`
- `autoscaler-only.env`
- `autoscaler-and-callgraph.env`

These tests currently deploy `tests/workflows/tinyfaas/linear3/stack.yaml`.

## Workflow Functionality Scenarios

Workflow functionality tests cover all workflow directories under `tests/workflows/tinyfaas/`:

- `linear3`
- `tree`
- `IoT`
- `webshop`

All workflow functionality tests rebuild tinyFaaS with:

- `no-autoscaler-no-callgraph.env`

### External prerequisites

`IoT` and `webshop` workflow tests require local env files and Supabase-backed state:

- `tests/workflows/tinyfaas/IoT/.env.yaml`
- `tests/workflows/tinyfaas/webshop/.env.yaml`

Missing/unconfigured env files are treated as test failures.

## Run Integration Tests for tinyFaaS

Run all integration tests for both faad and tinyFaaS:

```bash
make test-integration
```

### For tinyFaaS

Run the tinyFaaS workflow functionality suite:

```bash
go test ./tests/integration/tinyfaas -run TestWorkflowIntegrationSuite -v
```

Run the tinyFaaS platform behavior suite:

```bash
go test ./tests/integration/tinyfaas -run TestPlatformIntegrationSuite -v
```

## Notes

- Tests rebuild the platform per scenario, so they are intentionally slower.
- Callgraph assertions use API responses and avoid log-based checks.
