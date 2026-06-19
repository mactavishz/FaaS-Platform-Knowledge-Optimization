# Integration Tests

This directory contains integration tests in two categories:

- Platform behavior scenarios (autoscaler/callgraph)
- Workflow functionality scenarios (no autoscaler, no callgraph)

## Logging

- Integration command logs are captured and only printed on test failure to keep test output clean.
- To stream verbose command output live, set `INTEGRATION_DEBUG=1`.

Example:

```bash
INTEGRATION_DEBUG=1 go test ./tests/integration/tinyfaas -run TestPlatformIntegrationSuite -v
```

## VM Exclusivity

- The integration helpers enforce a single active VM when switching platforms.
- tinyFaaS test setup suspends `faasd` if running, then ensures `tinyfaas` is running.
- faasd test setup suspends `tinyfaas` if running, then ensures `faasd` is running.

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

`IoT` and `webshop` workflow tests require Supabase-backed state and these variables in the current environment:

- `SUPABASE_URL`
- `SUPABASE_KEY`

The affected stack files read both values from the current shell and default them to empty strings when unset.
The integration helpers fail fast before deploy if either variable is missing.

Recommended local setup uses `.envrc` with [direnv](https://direnv.net/):

```bash
echo 'export SUPABASE_URL="https://<project-ref>.supabase.co"' >> .envrc
echo 'export SUPABASE_KEY="<your-supabase-key>"' >> .envrc
direnv allow
```

## Run Integration Tests for tinyFaaS

Run all integration tests for both faasd and tinyFaaS:

```bash
make integration-test
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

The faasd workflow suite deploys local OCI archive tars from each workflow's `dist/` directory.

The integration helper builds missing archives before deploy, but it does not detect whether existing archives are stale after function code changes.

Force a rebuild when changing faasd workflow function code:

```bash
ALWAYS_BUILD=true go test ./tests/integration/faasd -run TestWorkflowIntegrationSuite -v
# or run the entire faasd integration suite with forced workflow archive rebuilds:
ALWAYS_BUILD=true make integration-test
```

You can also delete all local faasd workflow archive tars and let the next test
run rebuild the missing archives:

```bash
make clean-faasd-workflow-tars
```

Additional prerequisites for faasd workflow tests:

- `SUPABASE_URL` and `SUPABASE_KEY` exported in the current environment for `IoT` and `webshop`
