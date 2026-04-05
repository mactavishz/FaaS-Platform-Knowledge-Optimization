# Root Integration Tests

This directory contains root-level tinyFaaS integration tests that validate autoscaler and callgraph behavior using workflow-based scenarios.

## Prerequisites

- Vagrant VM is running: `vagrant up tinyfaas`
- Go toolchain installed
- Docker available in the tinyfaas VM (handled by provisioning)

## Scenarios

The test suite rebuilds tinyFaaS with one of these profiles in `tests/integration/env/`:

- `no-autoscaler-no-callgraph.env`
- `autoscaler-only.env`
- `autoscaler-and-callgraph.env`

All scenarios use the workflow stack at `tests/workflows/tinyfaas/linear2/stack.yml`.

## Run

Run all root integration tests:

```bash
make test-integration
```

Run one scenario:

```bash
go test ./tests/integration -run TestIntegration_AutoscalerOnly -v
```

Rebuild tinyFaaS with a specific profile manually:

```bash
make build-tinyfaas-profile PROFILE=autoscaler-only.env
```

## Notes

- Tests rebuild tinyFaaS per scenario, so they are intentionally slower.
- Tests call `/system/wipe` before and after each scenario to isolate state.
- Callgraph assertions use API responses and avoid log-based checks.
