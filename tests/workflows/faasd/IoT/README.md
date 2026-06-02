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

- `faasd_sensor_data`
- `faasd_use_case`

### Reads

- `CSL` reads `faasd_use_case`:
  - select row for `sensor_id = sensorID + 1`
  - select row for `sensor_id = sensorID - 1`

### Writes

- `SE` upserts `faasd_sensor_data`:
  - key `sensor_id = event.sensorID`
  - payload `message = event`
- `CSA` upserts `faasd_use_case`:
  - key `sensor_id = 1001`
- `CSL` upserts `faasd_use_case` (alert):
  - key `sensor_id = 1500`
- `DJ` upserts `faasd_use_case`:
  - key `sensor_id = 998`
- `AS` upserts `faasd_use_case` across a range:
  - key `sensor_id` for each ID in the computed range

## Setup

Build local faasd workflow images as multi-arch OCI archives with `faas-cli build` or `faas-cli publish`. The stack `image` values point to archive paths under `./dist/`.

### Required runtime env vars

Set runtime configuration through shell environment variables before deploying the stack.

1. Export:

```bash
export SUPABASE_URL="https://<project-ref>.supabase.co"
export SUPABASE_KEY="<your-supabase-anon-or-service-role-key>"
```

Or use `.envrc` with [direnv](https://direnv.net/):

```bash
echo 'export SUPABASE_URL="https://<project-ref>.supabase.co"' >> .envrc
echo 'export SUPABASE_KEY="<your-supabase-anon-or-service-role-key>"' >> .envrc
direnv allow
```

The stack reads both variables from the current environment and defaults them to empty strings if unset.

- `SUPABASE_URL`
- `SUPABASE_KEY`

Table/schema names are hardcoded in the handlers:

- `public.faasd_sensor_data`
- `public.faasd_use_case`

Create the faasd IoT tables before deploying:

```bash
bash tests/supabase/scripts/db.sh create iot faasd
```

To reset or inspect only the faasd IoT tables:

```bash
bash tests/supabase/scripts/db.sh reset iot faasd
bash tests/supabase/scripts/db.sh get iot faasd
```

2. Deploy:

```bash
# From repo root:
faas-cli deploy --platform faasd -f ./tests/workflows/faasd/IoT/stack.yaml
```

## Notes / Caveats

- Several functions are workload simulators (CPU-bound work via sieve or worker threads) rather than implementing real detection logic.
- Some "decision" values are currently hardcoded:
  - `CW` always returns `valid: true`
  - `CSL` sets `isTooLoud = true`
