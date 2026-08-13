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

When VM metrics are available, a second terminal table reports average resource
overhead. `CPU mean %` and `memory mean %` are means of the independent run-level
time averages, with their between-run sample SD in the adjacent columns.
`CPU change ±SD (pp)` and `memory change ±SD (pp)` pair each optimized profile
with the baseline from the same run, subtract the baseline utilization, and then
report the mean signed change and its between-run sample SD. The change is
expressed in utilization percentage points (`pp`), not as a percentage relative
to the baseline. For example, a change from 7.9% to 8.2% is approximately
`+0.30 pp`. The baseline row displays an em dash because it is not a comparison.
These values support claims about **average** resource overhead; they are not
peak or tail-utilization statistics. With only one selected run, sample SD is
undefined and is displayed as unavailable rather than as zero.

### Resource-change variation (per-run pairing)

The resource changes are computed **per run**, pairing each profile against the
baseline measured *in the same run*:

`change = optimized utilization - baseline utilization`

The table reports the mean and standard deviation of those signed per-run
changes (`<mean> ± <sd>`). Positive values mean that the optimized profile used
more of the resource; negative values mean that it used less. This change SD is
distinct from the adjacent `CPU +/-sd` and `memory +/-sd` columns, which describe
the spread of the absolute run-level utilization means.

Within-run pairing is appropriate because every run is an independent
re-deployment: a run reprovisions the VM and redeploys all profiles together,
so the profiles in one run share that run's infrastructure conditions.
Differencing within the run cancels run-to-run environmental variation. With a
small number of runs the SD is a coarse estimate (sample SD, `ddof=1`), so it
should be read as an indicative spread rather than a formal confidence interval;
it is reported precisely so the point estimates are not mistaken for exact
values.

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
- `functions_<platform>_overview.svg`: all function-completion panels for one
  platform in a compact overview with a shared profile legend
- `resources_<platform>_<workflow>.svg`: VM CPU utilization and memory usage
  over the measured k6 window (per-profile one-minute mean with between-run SD
  ribbon), showing the prewarm optimization's resource overhead relative to
  baseline

## VM host metrics (CPU/memory)

All resource figures read from one contract per experiment:
`resources/vm-usage.csv`, trimmed to the measured window
(`[k6_run_started_at, k6_run_finished_at]` from `metadata.json`). k6 runs on the
orchestrator rather than the VM, so VM-level CPU/memory reflect only the FaaS
platform plus deployed functions. A `resources/vm-usage.meta.json` sidecar
records which instrumentation produced the file and its resolution. Two sources
feed it:

For figures and terminal statistics, raw samples are preserved and resampled in
memory. Samples are assigned to floor-aligned elapsed-time windows (`[0, 60)`,
`[60, 120)`, and so on). The tool first averages within every run-minute and
then gives those run means equal weight when calculating the plotted mean and
between-run SD. Only complete one-minute windows shared by every selected run
are retained; a window counts as complete only when the recorded series reaches
its next minute boundary, so an incomplete trailing window is dropped. The
terminal resource summary averages the retained minute values once more within
each run before aggregating the independent runs.

Resource aggregation fails when a figure would mix instrumentation sources or
sampling intervals, when a selected run/profile is absent, or when an interior
run-minute is missing. This keeps the number and provenance of experimental
repeats constant along every curve.

**On-VM sampler (primary, new runs).** `benchmark/run.sh` pushes
`benchmark/scripts/vm-sampler.sh` to the VM and samples whole-VM CPU
(`/proc/stat` deltas; idle + iowait count as idle) and memory
(`MemTotal - MemAvailable` from `/proc/meminfo`) every
`BENCH_VM_SAMPLE_INTERVAL_S` seconds (default 5) from workflow deploy until
after the stats scrape, then downloads the raw series to
`resources/vm-samples.csv`. The tool derives `vm-usage.csv` from it
automatically -- no flag, no credentials. Local samples always win: the cloud
backfill below skips any experiment that has a `vm-samples.csv`.

**Cloud Monitoring backfill (fallback, historical runs).** Every benchmark VM
runs the Google Cloud Ops Agent (installed during provisioning), which ships
host metrics to Cloud Monitoring. With `--fetch-resources` the tool backfills
runs that lack local samples: CPU from the GCE built-in
`compute.googleapis.com/instance/cpu/utilization` metric; memory from the Ops
Agent `agent.googleapis.com/memory/{percent_used, bytes_used}`
(`state="used"`). The Ops Agent samples at 60s, so a ~37 minute run yields ~37
points -- coarse, which is why new runs use the 5s sampler instead.

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
`--refresh-resources` to rebuild it (from local samples when available,
otherwise by re-querying Cloud Monitoring). Once the CSVs are cached, normal
runs (without `--fetch-resources`) read them offline and regenerate the figures
with no GCP access required. Missing cached data for a selected run/profile
fails resource validation; use `--fetch-resources` to backfill historical runs
before comparing them.

## Validation

By default the tool fails if an experiment does not contain the expected number
of samples or if k6 reports failures. The validation status is printed before
the latency table.

Pass `--allow-incomplete` to downgrade validation errors to warnings and
continue generating outputs:

```bash
uv run bench-eval --results ../results --out out --allow-incomplete
```
