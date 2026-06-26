# Benchmark Evaluation

This directory contains the Python evaluation tool for the thesis benchmark results in `benchmark/results`.

It uses `uv` for project management, Polars for data processing, and Matplotlib for vector (SVG) figures.

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
  --runs bench-run-8 bench-run-9 bench-run-10 \
  --out out
```

## Outputs

The latency summary is printed to the terminal as an ASCII tables. One row per platform/workflow/metric/profile, with the statistics reported in the thesis:

- `mean (ms)` and `+/-sd (ms)`: mean latency and the between-run standard
  deviation (uncertainty across the repeated runs)
- `p90 (ms)`: 90th-percentile tail latency
- `mean impr %` and `p90 impr %`: improvement of each profile relative to its
  matching baseline (positive means lower latency), shown as `<mean> +/-<sd>`

All values are aggregates of the per-run statistics, so each repeated run is
weighted equally. `mean (ms)`, `p90 (ms)` are means of the per-run mean/p90.

### Improvement uncertainty (per-run pairing)

The improvement percentages are not computed by dividing the two aggregate
means once. Instead the improvement is computed **per run**, pairing each
profile against the baseline measured *in the same run*, and the table reports
the mean and standard deviation of those per-run improvements
(`<mean> +/-<sd>`). The `+/-<sd>` is therefore an uncertainty estimate on the
improvement itself, distinct from `+/-sd (ms)`, which is the spread of the
absolute latency.

Within-run pairing is appropriate because every run is an independent
re-deployment: a run reprovisions the VM and redeploys all profiles together,
so the profiles in one run share that run's infrastructure conditions.
Differencing within the run cancels run-to-run environmental variation. With
only three runs the SD is a coarse estimate (sample SD, `ddof=1`), so it should
be read as an indicative spread rather than a formal confidence interval; it is
reported precisely so the point estimates are not mistaken for exact values.

Figures are written to `out/figures` as vector SVG (set `FIGURE_FORMAT` in
`plotting.py` to `"png"` for a raster preview):

- `distribution_<platform>_<workflow>.svg`: pooled retained sample distribution
  (violin + strip), with black diamonds marking per-run means
- `summary_<platform>_<workflow>.svg`: mean and p90 bars with between-run SD
  error bars; each optimized bar is annotated with its improvement over baseline
  as `<mean>%` over `(+/-<sd>)`, using the same per-run pairing as the table
- `iterations_<platform>_<workflow>.svg`: per-run and mean latency trend across
  retained iterations (steady-state diagnostic)
- `functions_<platform>_<workflow>.svg`: per-function completion time relative
  to entry start, ordered by median finish
- `resources_<platform>_<workflow>.svg`: VM CPU utilization and memory usage
  over the measured k6 window (per-run lines plus per-profile mean), showing the
  prewarm optimization's resource overhead relative to baseline

## VM host metrics (CPU/memory)

Every benchmark VM runs the Google Cloud Ops Agent (installed during
provisioning), which ships host metrics to Cloud Monitoring. The tool can
backfill those metrics for each experiment's measured window
(`[k6_run_started_at, k6_run_finished_at]` from `metadata.json`) into a cached
`resources/vm-usage.csv` per experiment, then plot them. k6 runs on the
orchestrator rather than the VM, so VM-level CPU/memory reflect only the FaaS
platform plus deployed functions.

CPU comes from the GCE built-in `compute.googleapis.com/instance/cpu/utilization`
metric; memory from the Ops Agent `agent.googleapis.com/memory/{percent_used,
bytes_used}` (`state="used"`). The Ops Agent samples at 60s, so a ~37 minute run
yields ~37 points -- coarse, but adequate for a steady-state overhead
comparison, with identical instrumentation across every profile.

Fetching needs an identity with `roles/monitoring.viewer` on the benchmark
project and the project id via `--project` or `$GOOGLE_PROJECT`. Credentials are
resolved in this order:

1. `GOOGLE_CREDENTIALS` -- service-account JSON or a path to a key file. This is
   the variable the benchmark `.envrc` and Terraform already set, so in the same
   shell that runs the benchmark no extra auth step is needed.
2. `GOOGLE_APPLICATION_CREDENTIALS` -- a key file path.
3. The active `gcloud` identity (`gcloud auth print-access-token`).

So with the benchmark environment loaded, this just works:

```bash
GOOGLE_PROJECT=<benchmark-project> uv run bench-eval \
  --results ../results \
  --runs bench-run-8 bench-run-9 bench-run-10 \
  --out out \
  --fetch-resources
```

`--fetch-resources` reuses any existing `resources/vm-usage.csv`; pass
`--refresh-resources` to re-query. Once the CSVs are cached, normal runs (without
`--fetch-resources`) read them offline and regenerate the figures with no GCP
access required. Experiments without a cached CSV are simply omitted from the
resource figures.

## Validation

By default the tool fails if an experiment does not contain the expected number
of samples or if k6 reports failures. The validation status is printed before
the latency table.

Pass `--allow-incomplete` to downgrade validation errors to warnings and
continue generating outputs:

```bash
uv run bench-eval --results ../results --out out --allow-incomplete
```
