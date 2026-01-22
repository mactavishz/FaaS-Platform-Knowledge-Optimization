# IoT Function Workflow Summary (Entry = `I`)

This document summarizes the workflow implemented under `IoT/` when starting from function **`I` (AnalyzeSensor)**, focusing on the *logical function workflow graph*.

## Components

- **Workflow entry handler:** `IoT/handler.js`
  - Parses input (API Gateway JSON body vs direct invoke)
  - Ensures a `traceId` and logs structured `FSMSG` timing records
  - Dispatches to the starting fusionable selected by env `FUNCTION_TO_HANDLE`
  - Provides fusionables with `callFunction(name, input, sync)` to invoke downstream steps

- **Workflow steps:** `IoT/*/handler.js`
  - `I` orchestrates the overall workflow
  - `CW` validates (CPU load)
  - `SE` persists sensor data
  - `CT` / `CS` / `CA` are parallel analysis branches
  - `AS` produces actuation side effects (signage updates)
  - `CSA`, `CSL`, `DJ` write alerts/markers to DynamoDB

## High-Level Call Graph

Legend:

- `sync` means the caller awaits a result.
- `async` means the caller intends fire-and-forget / non-blocking invocation.

```
I (AnalyzeSensor)
  ├─sync─> CW (CheckSensor)
  ├─sync─> SE (StoreEvent)
  └─async fan-out (parallel)
       ├─async─> CT (CheckTemperature)
       │          └─async─> AS (ActionSignage)
       ├─async─> CS (CheckSound)
       │          ├─sync─> CSL (CheckSoundLoud)
       │          └─sync─> CSA (CheckSoundAccident)
       └─async─> CA (CheckAir)
                  ├─sync─> DJ (DetectJam)
                  └─async─> AS (ActionSignage)
```

## Workflow Behavior (Step-by-Step)

### 1) `I`  AnalyzeSensor (`IoT/I/handler.js`)
- Generates a **synthetic sensor batch** for one random `sensorID`:
  - `Temperature`, `Sound`, `AirQuality`, `EmergencyVehicle`
  - each event has `{ sensorID, value }`
- Validation + persistence:
  - Calls `CW` (sync) with `{ originalEvent: TemperatureEvent }`
  - Calls `SE` (sync) with the raw temperature event
- Parallel fan-out:
  - Calls `CT` (async) with `{ originalEvent: TemperatureEvent }`
  - Calls `CS` (async) with `{ originalEvent: SoundEvent }`
  - Calls `CA` (async) with `{ originalEvent: AirQualityEvent }`
- Joins results via `Promise.all` and returns `{ results }`.

### 2) `CW`  CheckSensor (`IoT/CW/handler.js`)
- Does CPU-intensive Sieve of Eratosthenes (default size `500000`, configurable via `event.sieve`).
- Returns `{ valid: true, eratosthenes: <primes>, time: <ms> }`.

### 3) `SE`  StoreEvent (`IoT/SE/handler.js`)
- Persists the temperature event in DynamoDB table `SensorDataTable`.

### 4) `CT`  CheckTemperature (`IoT/CT/handler.js`)
- Runs artificial CPU load using `worker_threads`.
- Calls `AS` (async) to update signage near the temperature sensor.
- Note: uses `event.originalEvent.chain`, but `I` does not set `chain` on the generated Temperature event, so this may be `undefined` unless another entry mode provides it.

### 5) `CS`  CheckSound (`IoT/CS/handler.js`)
- Adds `location` and `sensorID` fields derived from `originalEvent.sensorID`.
- Runs two checks in parallel (both sync calls):
  - `CSL` for “too loud” context
  - `CSA` for accident-like detection

### 6) `CSL`  CheckSoundLoud (`IoT/CSL/handler.js`)
- Reads neighbor sensor IDs (±1) from `UseCaseTable`.
- Currently sets `isTooLoud = true` (hardcoded).
- If loud, writes an alert item with `SensorID = 1500`.

### 7) `CSA`  CheckSoundAccident (`IoT/CSA/handler.js`)
- CPU-intensive sieve computation (default `500000`).
- Writes an item to `UseCaseTable` with `SensorID = 1001`.

### 8) `CA`  CheckAir (`IoT/CA/handler.js`)
- Calls `DJ` (sync) to detect traffic jam markers.
- Calls `AS` (async) with `chain = 5` to update signage.

### 9) `DJ`  DetectJam (`IoT/DJ/handler.js`)
- Runs artificial CPU load using `worker_threads`.
- Writes an item to `UseCaseTable` with `SensorID = 998`.

### 10) `AS`  ActionSignage (`IoT/AS/handler.js`)
- Runs artificial CPU load using `worker_threads`.
- Writes **one item per sensor** across an inclusive range:
  - range: `min(location, location + chain)` to `max(...)`
  - all writes go to `UseCaseTable`

## DynamoDB I/O Summary

### Tables
- `SensorDataTable`
- `UseCaseTable`

### Reads
- `CSL` reads `UseCaseTable`:
  - `getItem` for `SensorID = sensorID + 1`
  - `getItem` for `SensorID = sensorID - 1`

### Writes
- `SE` writes `SensorDataTable`:
  - `putItem` with key `SensorID = event.sensorID`
  - attribute `Message = JSON.stringify(event)`
- `CSA` writes `UseCaseTable`:
  - `putItem` with key `SensorID = 1001`
- `CSL` writes `UseCaseTable` (alert):
  - `putItem` with key `SensorID = 1500`
- `DJ` writes `UseCaseTable`:
  - `putItem` with key `SensorID = 998`
- `AS` writes `UseCaseTable` across a range:
  - `putItem` for each `SensorID` in the computed range

## Notes / Caveats

- Several functions are workload simulators (CPU-bound work via sieve or worker threads) rather than implementing real detection logic.
- Some “decision” values are currently hardcoded:
  - `CW` always returns `valid: true`
  - `CSL` sets `isTooLoud = true`
- The orchestration API used by fusionables (`callFunction`) supports sync vs async intent, but actual delivery mechanics depend on the deployment/runtime configuration.
