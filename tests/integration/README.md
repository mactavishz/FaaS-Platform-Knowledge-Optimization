# Integration Tests

This directory contains integration tests in two categories:

- Platform behavior scenarios (autoscaler/callgraph)
- Workflow functionality scenarios (no autoscaler, no callgraph)

## Prerequisites

- Vagrant VM is running: `vagrant up tinyfaas` and/or `vagrant up faasd`
- Go toolchain installed
- Docker available in the tinyfaas VM (handled by provisioning)

## Platform Behavior Scenarios

The platform behavior suite rebuilds tinyFaaS with one of these profiles in `tests/integration/env/`:

- `no-autoscaler-no-callgraph.env`
- `autoscaler-only.env`
- `autoscaler-and-callgraph-no-prewarm.env`
- `autoscaler-and-callgraph-and-prewarm.env`

These tests deploy `tests/workflows/tinyfaas/linear3/stack.yaml`.
For prewarm-specific scenarios, they deploy with `-e FUNCTION_DELAY_SEC=5` to create deterministic downstream delay windows.

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

Run all integration tests for both faasd and tinyFaaS:

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

### For faasd

Run the faasd workflow functionality suite:

```bash
go test ./tests/integration/faasd -run TestWorkflowIntegrationSuite -v
```

The faasd workflow suite uses remote images for the deployment of the workflow functions, so it does not require a local build step.

Additional prerequisites for faasd workflow tests:

- Configured env files when required:
  - `tests/workflows/faasd/IoT/.env.yaml`
  - `tests/workflows/faasd/webshop/.env.yaml`
