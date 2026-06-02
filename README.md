# FaaS-Platform-Knowledge-Optimization

Optimize FaaS platform cold starts using platform knowledge.

This repository contains the platform code, local development workflows, and evaluation tooling for `faasd`, `tinyFaaS`, `faas-cli`, autoscaling, and callgraph-based prewarming experiments.

## Quickstart

### Prerequisites

- [Go](https://go.dev/doc/install)
- [make](https://www.gnu.org/software/make/)
- [Vagrant](https://www.vagrantup.com/docs/installation)
- [VirtualBox](https://www.virtualbox.org/)
- [A Supabase Project](https://supabase.com/)

We suggest managing the Go toolchain with [mise](https://github.com/jdx/mise).

### Setup

```bash
git submodule update --init --recursive
cp .env.example .env
```

The local platform build scripts read `.env` and install that configuration into `faasd` or `tinyFaaS`.

Both Vagrant VMs forward guest port `8080` to host `127.0.0.1:8080`, so only one platform should be active locally at a time.

### Build And Start A Platform

```bash
# Build the local faas-cli binary
make build-faas-cli

# Build and start one platform
make build-faasd
# or
make build-tinyfaas
```

`make build-faasd` also performs the `faas-cli` login step. If needed, you can rerun:

```bash
make faasd-login
make faasd-passwd
```

Workflow stack files live under `tests/workflows/<platform>/`. Use the workflow READMEs there for deploy and invocation details.

## Common Commands

```bash
make build-faas-cli
make build-faasd
make build-tinyfaas
make test-faas-cli
make test-faasd
make test-tinyfaas
make integration-test
make unit-test
```

`make unit-test` covers the repo-root `autoscaler` and `callgraph` modules only. This repository is multi-module, so it is not a full-repo test sweep.

## Testing

Use these entry points from the repository root:

```bash
# Fast local checks
make unit-test
make test-faas-cli

# Platform-specific test runs
make test-faasd
make test-tinyfaas

# Repo-level integration suites
make integration-test
```

Testing notes:

- Integration tests require Vagrant and VirtualBox.
- Integration tests for `IoT` and `webshop` workflow require a running [Supabase](https://supabase.com/) project.
- `faas-cli` must be installed or built and available on `PATH` for integration flows.
- Some workflow tests, especially `IoT` and `webshop`, require setting `SUPABASE_URL` and `SUPABASE_KEY` in the environment variables.
- For live command output while running integration tests, set `INTEGRATION_DEBUG=1`.

See `tests/integration/README.md` for suite details and `tests/supabase/README.md` for database setup used by the workflow tests.

## Repository Map

- `faasd/`: faasd platform source
- `tinyFaaS/`: tinyFaaS platform source
- `faas-cli/`: CLI used to deploy and invoke functions
- `autoscaler/`: autoscaling logic
- `callgraph/`: callgraph tracking and prewarming logic
- `tests/integration/`: repo-level integration suites
- `tests/workflows/`: example workflows and stack files for each platform
- `benchmark/`: k6-based benchmarking tools
- `terraform/`: remote benchmarking infrastructure
- `scripts/`: local build and remote deployment helpers

## Further Reading

- `tests/integration/README.md`
- `tests/supabase/README.md`
- `benchmark/README.md`
- `terraform/README.md`
- `tests/workflows/faasd/*/README.md`
- `tests/workflows/tinyfaas/*/README.md`
