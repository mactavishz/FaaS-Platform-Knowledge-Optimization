# Benchmarks (k6)

This folder contains basic k6 scripts for benchmarking function workflows on tinyFaaS/Faasd.

## Prereqs

- k6 installed locally (`k6 version` should work)
- tinyFaaS/Faasd running and reachable
- Workflow functions deployed (example below uses the IoT workflow)

## Install

```bash
pnpm -C tests/benchmark install
```

## Workflow cold-start latency benchmark

This benchmark sends one request to a workflow entry function, records the client-observed latency, then waits for all workflow functions to scale down (`/system/list` reports `running=false`) before sending the next request. This intentionally triggers cold starts.

### Environment variables

- `GATEWAY_URL` (default: `http://127.0.0.1:8888`)
- `ENTRY_FUNCTION` (required)
- `WORKFLOW_FUNCTIONS` (required, comma-separated list)

- `INVOKE_PATH` (default: `/fn/{name}`)
- `LIST_PATH` (default: `/system/list`)

- `ITERATIONS` (default: `10`)
- `VUS` (default: `1`)

- `METHOD` (default: `POST`)
- `BODY` (default: `{}`)
- `EXPECTED_STATUS` (default: `200`)

- `INVOKE_TIMEOUT_MS` (default: `60000`)
- `POLL_INTERVAL_MS` (default: `500`)
- `SCALE_DOWN_TIMEOUT_MS` (default: `900000`)
- `STRICT_FUNCTIONS` (default: `true`)

### Example (IoT workflow)

```bash
ENTRY_FUNCTION=iot-i \
WORKFLOW_FUNCTIONS=iot-i,iot-cw,iot-se,iot-ct,iot-cs,iot-ca,iot-csl,iot-csa,iot-dj,iot-as \
GATEWAY_URL=http://127.0.0.1:8888 \
ITERATIONS=10 \
k6 run tests/benchmark/scripts/workflow_cold_latency.js
```

Note: the IoT workflow includes async fan-out. This benchmark measures the entry invocation latency, not the full end-to-end completion across async branches.
