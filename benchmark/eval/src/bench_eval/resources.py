"""Materialize ``resources/vm-usage.csv`` from the on-VM sampler output.

New benchmark runs collect a high-resolution (default 5s) whole-VM CPU/memory
series with ``benchmark/scripts/vm-sampler.sh``; ``run.sh`` downloads it to
``<experiment>/resources/vm-samples.csv``. The raw file deliberately covers a
wider window than the measurement (workflow deploy through the post-run stats
scrape), so this module trims it to the ``[k6_run_started_at,
k6_run_finished_at]`` interval from ``metadata.json`` and writes ``vm-usage.csv``
in the exact schema the Cloud Monitoring backfill produces. Everything
downstream of ``vm-usage.csv`` is therefore source-agnostic.

Runs without a local sample file (all historical runs) keep using
``monitoring.fetch_resource_samples`` as the 60s-resolution fallback. When both
could apply, the local samples win: the backfill skips any experiment that has a
``vm-samples.csv``.

A ``vm-usage.meta.json`` sidecar records which instrumentation produced the
derived file and at what resolution, so figures can state their data source.
"""

import csv
import json
from datetime import datetime
from pathlib import Path

from .constants import PLATFORMS, PROFILES, WORKFLOWS
from .monitoring import RESOURCE_CSV_COLUMNS, _parse_rfc3339


def materialize_resource_usage(
    results: Path,
    runs: tuple[str, ...],
    *,
    refresh: bool = False,
) -> None:
    """Derive ``vm-usage.csv`` for every experiment that has local samples.

    Existing derived files are kept unless ``refresh`` is set. Experiments
    without ``vm-samples.csv`` are left untouched for the Cloud Monitoring
    fallback. Needs no credentials; it is safe to run unconditionally.
    """
    for run in runs:
        for profile in PROFILES:
            for platform in PLATFORMS:
                for workflow in WORKFLOWS:
                    experiment_dir = results / run / profile / platform / workflow
                    _materialize_one_experiment(experiment_dir, refresh=refresh)


def _materialize_one_experiment(experiment_dir: Path, *, refresh: bool) -> None:
    samples_path = experiment_dir / "resources" / "vm-samples.csv"
    if not samples_path.exists():
        return

    output_path = experiment_dir / "resources" / "vm-usage.csv"
    if output_path.exists() and not refresh:
        return

    metadata_path = experiment_dir / "metadata.json"
    label = experiment_dir.relative_to(experiment_dir.parents[3])
    if not metadata_path.exists():
        print(f"  skip {label}: vm-samples.csv present but metadata.json missing")
        return

    metadata = json.loads(metadata_path.read_text())
    started_at = metadata.get("k6_run_started_at")
    finished_at = metadata.get("k6_run_finished_at")
    if not (started_at and finished_at):
        print(f"  skip {label}: missing k6 window in metadata")
        return

    start_dt = _parse_rfc3339(started_at)
    end_dt = _parse_rfc3339(finished_at)

    rows = _trim_samples(samples_path, start_dt, end_dt)
    if not rows:
        print(f"  skip {label}: no samples inside the measured window")
        return

    output_path.parent.mkdir(parents=True, exist_ok=True)
    with output_path.open("w", newline="") as handle:
        writer = csv.writer(handle)
        writer.writerow(RESOURCE_CSV_COLUMNS)
        writer.writerows(rows)

    interval_s = _sampler_interval(metadata, rows)
    meta_path = experiment_dir / "resources" / "vm-usage.meta.json"
    meta_path.write_text(
        json.dumps({"source": "sampler", "interval_s": interval_s}, indent=2) + "\n"
    )
    print(f"  wrote {label}/resources/vm-usage.csv ({len(rows)} points, sampler)")


def _trim_samples(
    samples_path: Path,
    start_dt: datetime,
    end_dt: datetime,
) -> list[tuple[str, float, float, float, float]]:
    rows: list[tuple[str, float, float, float, float]] = []
    with samples_path.open(newline="") as handle:
        for record in csv.DictReader(handle):
            try:
                point_dt = _parse_rfc3339(record["timestamp"])
                cpu_pct = float(record["cpu_pct"])
                mem_used_pct = float(record["mem_used_pct"])
                mem_used_bytes = float(record["mem_used_bytes"])
            except (KeyError, TypeError, ValueError):
                # A row truncated by the sampler kill is expected; drop it.
                continue
            if not (start_dt <= point_dt <= end_dt):
                continue
            seconds = (point_dt - start_dt).total_seconds()
            rows.append(
                (
                    record["timestamp"],
                    round(seconds, 3),
                    cpu_pct,
                    mem_used_pct,
                    mem_used_bytes,
                )
            )
    return rows


def _sampler_interval(metadata: dict, rows: list[tuple]) -> float | None:
    interval = (metadata.get("vm_sampler") or {}).get("interval_s")
    if interval is not None:
        return interval
    # Older metadata has no sampler block; infer the grid from the data.
    if len(rows) < 2:
        return None
    deltas = sorted(b[1] - a[1] for a, b in zip(rows, rows[1:]))
    return round(deltas[len(deltas) // 2], 3)
