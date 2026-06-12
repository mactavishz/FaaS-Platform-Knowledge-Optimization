# Benchmark Evaluation

This directory contains the Python evaluation tool for the thesis benchmark results in `benchmark/results`.

It uses `uv` for project management, Polars for data processing, and Matplotlib for PNG figures.

## Approach

Each benchmark configuration has 70 k6 iterations. The first 10 iterations are treated as warmup and the last 10 as ramp-down, leaving retained iteration indices 10 through 59. This yields 50 retained samples per run and 150 retained samples across the three repeated runs.

The primary latency source is each experiment's `k6/metrics.csv`:

- `iot` and `tree`: `workflow_latency_ms`
- `webshop-browse-addcart-checkout`: step metrics `browse_latency_ms`,
  `addcart_latency_ms`, `checkout_latency_ms`, plus a computed journey total
- `webshop-addcart-checkout`: step metrics `addcart_latency_ms`,
  `checkout_latency_ms`, plus a computed journey total

The tool computes per-run statistics first, then aggregates those run-level statistics. Pooled retained samples are used for distribution plots only, not as independent experiment repeats.

Function-level plots use `stats/functions/*.json`. Function invocation finish times are aligned relative to the entry function start for each retained iteration or operation. Samples are retained only when the function is reachable in the explicit workflow graph and starts before the next entry invocation. Webshop function plots are operation-windowed: `webshop-addcart-checkout` renders `addcart` and `checkout` subplots, and `webshop-browse-addcart-checkout` renders `browse`, `addcart`, and `checkout` subplots.

## Usage

Run commands from this directory:

```bash
cd benchmark/eval
uv run bench-eval --results ../results --out out
```

Equivalent commands from the repository root:

```bash
uv --project benchmark/eval run bench-eval --results benchmark/results --out benchmark/eval/out
```

The run list must be explicitly passed to the tool. By default it is empty.

To pass it explicitly:

```bash
uv run bench-eval \
  --results ../results \
  --runs bench-run-5 bench-run-6 bench-run-7 \
  --out out
```

## Outputs

Tables are written to `out/tables`:

- `latency_run_summary.csv`: per-run statistics
- `latency_summary.csv`: aggregate of run-level statistics
- `latency_improvements.csv`: percentage improvement relative to baseline
- `validation.csv`: validation status per experiment

Figures are written to `out/figures` as PNG only:

- `distribution_<platform>_<workflow>.png`
- `summary_<platform>_<workflow>.png`
- `iterations_<platform>_<workflow>.png`
- `functions_<platform>_<workflow>.png`

## Validation

By default the tool fails if a experiment does not contain the expected number of samples or if k6 reports failures.

Pass `--allow-incomplete` to write validation warnings and continue generating outputs:

```bash
uv run bench-eval plot --results ../results --out out --allow-incomplete
```
