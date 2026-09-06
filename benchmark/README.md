# Benchmarking

This folder contains scripts for the benchmarking against tinyFaaS/Faasd. It creates infrastructure using terraform on Google Cloud Platform (GCP) and runs the benchmarks using k6. The results are stored in `benchmark/results` and can be evaluated using the evaluation tool in `benchmark/eval`.

## Prerequisites

- terraform installed locally (checkout `terraform` in the root of this repository)
- k6 installed locally (`k6 version` should work)

## Workflow cold-start latency benchmark

This benchmark sends one request to a workflow entry function, records the client-observed latency, then waits for all workflow functions to scale down before sending the next request. For tinyFaaS this uses `/system/list` with `running=false`; for faasd this uses `/system/functions` with `availableReplicas=0`.

### Environment variables

These are the important environment variables for the benchmark script:

- `PLATFORM` (default: `faasd tinyfaas`), you can also specify a single platform, e.g. `PLATFORM=faasd`
- `WORKFLOWS` (default: `iot tree webshop-browse-addcart-checkout webshop-addcart-checkout`), you can also specify a single workflow, e.g. `WORKFLOWS=iot`
- `PROFILES` (default: `baseline optimized-sma optimized-ema`), you can also specify a single profile, e.g. `PROFILES=optimized-sma`
- `BENCH_ITERATIONS` (default: `70`)
- `MACHINE_TYPE` (default: `n2-standard-4`)
- `RUN_NAME` (default: current timestamp)
- `DRY_RUN` (default: `false`)

All the multi-value environment variables are separated by spaces. The benchmark script will iterate over all combinations of the specified platforms, workflows, and profiles.

We recommend to set the `DRY_RUN=true` to get a quick overview of the benchmark configuration and the expected output directory structure before running the full benchmark.

All the available environment variables are listed in `benchmark/run.sh`.

### Run example

```bash
# start benchmark execution that runs for all workflows and profiles, with 70 iterations each, on a n2-standard-4 machine, and saves the results in a the `bench-run-1` directory in the results folder
RUN_NAME="bench-run-1"  benchmark/run.sh
```

### k6 Script

- `benchmark/scripts/workflow_cold_latency.js`
- `benchmark/scripts/webshop_browse_addcart_checkout.js`
- `benchmark/scripts/webshop_addcart_checkout.js`
