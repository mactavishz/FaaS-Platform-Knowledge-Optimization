# Benchmarks (k6)

This folder contains basic k6 scripts for benchmarking function workflows on tinyFaaS/Faasd.

## Prereqs

- k6 installed locally (`k6 version` should work)
- tinyFaaS/Faasd running and reachable
- Workflow functions deployed (example below uses the IoT workflow)

## Install

```bash
pnpm -C benchmark install
```

## Workflow cold-start latency benchmark

This benchmark sends one request to a workflow entry function, records the client-observed latency, then waits for all workflow functions to scale down before sending the next request. For tinyFaaS this uses `/system/list` with `running=false`; for faasd this uses `/system/functions` with `availableReplicas=0`.

This script uses a `shared-iterations` scenario with configurable `MAX_DURATION` and `GRACEFUL_STOP`, so longer cold-start runs do not get cut off by the default k6 time limit.

### Environment variables

- `PLATFORM` (default: `tinyfaas`; supported: `tinyfaas`, `faasd`)
- `GATEWAY_URL` (default: `http://127.0.0.1:8080`)
- `WORKFLOW` (required; supported: `iot`, `tree`, `webshop`)

- `INVOKE_PATH` (default: `/fn/{name}` for tinyFaaS, `/function/{name}` for faasd)
- `LIST_PATH` (default: `/system/list` for tinyFaaS, `/system/functions` for faasd)

For authenticated faasd gateways, set:

- `BASIC_AUTH_USER`
- `BASIC_AUTH_PASSWORD`

- `ITERATIONS` (default: `10`)
- `VUS` (default: `1`)
- `MAX_DURATION` (default: `60m`)
- `GRACEFUL_STOP` (default: `30s`)

- `METHOD` (default: `POST`)
- `BODY` (default: `{}`)
- `EXPECTED_STATUS` (default: `200`)

- `INVOKE_TIMEOUT_MS` (default: `60000`)
- `POLL_INTERVAL_MS` (default: `15000`)
- `SCALE_DOWN_TIMEOUT_MS` (default: `900000`)
- `STRICT_FUNCTIONS` (default: `true`)

### Example (IoT workflow)

```bash
PLATFORM=tinyfaas \
WORKFLOW=iot \
GATEWAY_URL=http://127.0.0.1:8080 \
ITERATIONS=10 \
MAX_DURATION=60m \
k6 run benchmark/scripts/workflow_cold_latency.js
```

For the faasd IoT workflow, use `PLATFORM=faasd` and include `BASIC_AUTH_USER` / `BASIC_AUTH_PASSWORD` when the gateway has basic auth enabled.

Note: the IoT workflow includes async fan-out. This benchmark measures the entry invocation latency, not the full end-to-end completion across async branches.

## Webshop cold-sensitive user journey benchmarks

These benchmarks drive the `webshop-frontend` function as shopping sessions:

- `webshop-browse-addcart-checkout`: browse products (`operation=get`), add a product to cart (`operation=addcart`), then checkout (`operation=checkout`)
- `webshop-addcart-checkout`: add a product to cart (`operation=addcart`), then checkout (`operation=checkout`)

Each k6 iteration waits for all webshop functions to scale down once before the first operation. The scripts do not wait for scale-down between operations in the same journey.

It is designed to compare `CALLGRAPH_ENABLED=false|true` while keeping autoscaler enabled.

In the current webshop workflow, frontend `addcart` and `checkout` are synchronous.
The checkout function asynchronously empties the cart, and the benchmark uses a new `userId`
for every VU iteration, so there is no separate cart cleanup step.

### Script

- `benchmark/scripts/webshop_browse_addcart_checkout.js`
- `benchmark/scripts/webshop_addcart_checkout.js`

In `benchmark/run.sh`, the two webshop journeys are separate workflow identifiers. Each gets a fresh provisioned VM and its own normal per-workflow output directory:

- `webshop-browse-addcart-checkout`
- `webshop-addcart-checkout`

### Environment variables

Required/important:

- `PLATFORM` (default: `tinyfaas`; supported: `tinyfaas`, `faasd`)
- `GATEWAY_URL` (default: `http://127.0.0.1:8080`)
- `WORKFLOW` (default: `webshop`; only `webshop` is supported by these scripts)
- `RUN_ID` (default: `webshop-bench`)

Traffic and iteration:

- `ITERATIONS` (default: `10`)
- `VUS` (default: `1`)
- `MAX_DURATION` (default: `60m`)
- `GRACEFUL_STOP` (default: `30s`)

Timeout and polling:

- `INVOKE_TIMEOUT_MS` (default: `60000`)
- `POLL_INTERVAL_MS` (default: `15000`)
- `SCALE_DOWN_TIMEOUT_MS` (default: `120000`)
- `STRICT_FUNCTIONS` (default: `true`)

Journey payload tuning:

- `CURRENCY` (default: `EUR`)
- `CHECKOUT_ADDRESS_JSON` (default: `{"street":"123 Main St"}`)
- `CHECKOUT_EMAIL` (default: `bench-user@example.com`)
- `CHECKOUT_CREDIT_CARD_JSON` (default: `{"creditCardNumber":"4111111111111111"}`)

For each iteration, the webshop journey adds one random cart item. Product IDs are selected
from the hardcoded webshop catalogue (`1..11`), and quantities are selected from `1..5`.

Compatibility/path options:

- `INVOKE_PATH` (default: `/fn/{name}` for tinyFaaS, `/function/{name}` for faasd)
- `LIST_PATH` (default: `/system/list` for tinyFaaS, `/system/functions` for faasd)
- `EXPECTED_STATUS` (default: `200`)

For authenticated faasd gateways, set:

- `BASIC_AUTH_USER`
- `BASIC_AUTH_PASSWORD`

### Metrics emitted

- `browse_latency_ms` (`webshop_browse_addcart_checkout.js` only)
- `addcart_latency_ms`
- `checkout_latency_ms`
- `invoke_failures`
- `list_failures`
- `state_validation_failures`
- `scale_down_timeouts`

### Recommended run protocol

1. Keep autoscaler enabled in both conditions: `AUTOSCALER_ENABLED=true`.
2. Set idle scale-down to `30s` for both conditions: `DEFAULT_SCALE_TO_ZERO_IDLE_DURATION=30s`.
3. Rebuild tinyFaaS after env changes.
4. Deploy `tests/workflows/tinyfaas/webshop/stack.yaml`.
5. Run one short training pass for each condition before measured samples.
6. Run measured samples with `VUS=1` for strict cold-sensitive comparison.

### Example: training pass (callgraph disabled)

```bash
PLATFORM=tinyfaas \
RUN_ID=webshop-off-train \
GATEWAY_URL=http://127.0.0.1:8080 \
ITERATIONS=3 \
VUS=1 \
MAX_DURATION=60m \
k6 run benchmark/scripts/webshop_browse_addcart_checkout.js
```

### Example: measured pass (callgraph disabled)

```bash
PLATFORM=tinyfaas \
RUN_ID=webshop-off-measure \
GATEWAY_URL=http://127.0.0.1:8080 \
ITERATIONS=10 \
VUS=1 \
MAX_DURATION=60m \
k6 run benchmark/scripts/webshop_browse_addcart_checkout.js
```

### Example: measured pass (callgraph enabled)

```bash
PLATFORM=tinyfaas \
RUN_ID=webshop-on-measure \
GATEWAY_URL=http://127.0.0.1:8080 \
ITERATIONS=10 \
VUS=1 \
MAX_DURATION=60m \
k6 run benchmark/scripts/webshop_addcart_checkout.js
```

Note: callgraph on/off is controlled by tinyFaaS runtime env (`CALLGRAPH_ENABLED`), not by the k6 script.
