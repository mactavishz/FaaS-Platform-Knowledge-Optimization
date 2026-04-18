# IoT Function Workflow Summary (Entry = `I`)

This document summarizes the workflow implemented under `IoT/` when starting from function **`I` (AnalyzeSensor)**, focusing on the *logical function workflow graph*.

## Functions

- **Workflow steps:** `IoT/*/handler.js`
  - `I` orchestrates the overall workflow
  - `CW` validates (CPU load)
  - `SE` persists sensor data
  - `CT` / `CS` / `CA` are parallel analysis branches
  - `AS` produces actuation side effects (signage updates)
  - `CSA`, `CSL`, `DJ` write alerts/markers to the database

## High-Level Call Graph

Legend:

- `sync` means the caller awaits a result.
- `async` means the caller intends fire-and-forget / non-blocking invocation.

```text
I (AnalyzeSensor)
  |-sync-> CW (CheckSensor)
  |-sync-> SE (StoreEvent)
  |-async fan-out (parallel)
       |-async-> CT (CheckTemperature)
       |          |-async-> AS (ActionSignage)
       |-async-> CS (CheckSound)
       |          |-sync-> CSL (CheckSoundLoud)
       |          |-sync-> CSA (CheckSoundAccident)
       |-async-> CA (CheckAir)
                  |-sync-> DJ (DetectJam)
                  |-async-> AS (ActionSignage)
```

## Supabase I/O Summary

### Tables

- `sensor_data`
- `use_case`

### Reads

- `CSL` reads `use_case`:
  - select row for `sensor_id = sensorID + 1`
  - select row for `sensor_id = sensorID - 1`

### Writes

- `SE` upserts `sensor_data`:
  - key `sensor_id = event.sensorID`
  - payload `message = event`
- `CSA` upserts `use_case`:
  - key `sensor_id = 1001`
- `CSL` upserts `use_case` (alert):
  - key `sensor_id = 1500`
- `DJ` upserts `use_case`:
  - key `sensor_id = 998`
- `AS` upserts `use_case` across a range:
  - key `sensor_id` for each ID in the computed range

## Setup

Before `faas-cli build` / `faas-cli push` with local faasd workflows:

- Keep the default image prefix `registry.local:5050/faasd/...`.
- Ensure `registry.local` resolves on the host running Docker push.
- Ensure Docker proxy bypass is configured for local registries.
- Ensure `NO_PROXY`/`no_proxy` includes at least: `registry.local,localhost,127.0.0.1,host.docker.internal`.
- Docker push probes registries with HTTPS `HEAD` by default. If your local registry serves plain HTTP, configure Docker insecure registries for `registry.local:5050`.

### Required runtime env vars

Set runtime configuration through an OpenFaaS YAML `environment_file`.

1) Create `tests/workflows/faasd/IoT/.env.yaml` from the example:

```bash
cd tests/workflows/faasd/IoT
cp .env.yaml.example .env.yaml
```

`.env.yaml` is git-ignored.

2) Fill in:

- `SUPABASE_URL`
- `SUPABASE_KEY`

Table/schema names are hardcoded in the handlers:

- `public.sensor_data`
- `public.use_case`

3) Deploy:

```bash
# From repo root:
faas-cli deploy --platform faasd -f ./tests/workflows/faasd/IoT/stack.yaml
```

## Notes / Caveats

- Several functions are workload simulators (CPU-bound work via sieve or worker threads) rather than implementing real detection logic.
- Some "decision" values are currently hardcoded:
  - `CW` always returns `valid: true`
  - `CSL` sets `isTooLoud = true`
