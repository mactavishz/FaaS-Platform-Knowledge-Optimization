"""Process ``resources/vm-usage.csv`` from the on-VM sampler output.

New benchmark runs collect a high-resolution (default 5s) whole-VM CPU/memory
series with ``benchmark/scripts/vm-sampler.sh``; ``run.sh`` downloads it to
``<experiment>/resources/vm-samples.csv``. The raw file deliberately covers a
wider window than the measurement, so this module trims it to the ``[k6_run_started_at,
k6_run_finished_at]`` interval from ``metadata.json`` and writes ``vm-usage.csv``
for downstream analysis.
"""

import csv
import json
from datetime import datetime, timezone
from pathlib import Path

from .constants import PLATFORMS, PROFILES, WORKFLOWS

RESOURCE_CSV_COLUMNS = (
    "timestamp",
    "seconds_since_start",
    "cpu_pct",
    "mem_used_pct",
    "mem_used_bytes",
)


def _parse_timestamp(value: str) -> datetime:
    return datetime.fromisoformat(value.replace("Z", "+00:00")).astimezone(timezone.utc)


def process_resource_usage(
    results: Path,
    runs: tuple[str, ...],
) -> None:
    """Process ``vm-usage.csv`` for every experiment.

    Existing derived ``vm-samples.csv`` files are overwritten on every evaluation. Experiments
    without ``vm-samples.csv`` are left untouched, including any existing local
    ``vm-usage.csv``.
    """
    for run in runs:
        for profile in PROFILES:
            for platform in PLATFORMS:
                for workflow in WORKFLOWS:
                    experiment_dir = results / run / profile / platform / workflow
                    _process_experiment(experiment_dir)


def _process_experiment(experiment_dir: Path) -> None:
    samples_path = experiment_dir / "resources" / "vm-samples.csv"
    if not samples_path.exists():
        return

    output_path = experiment_dir / "resources" / "vm-usage.csv"
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

    start_dt = _parse_timestamp(started_at)
    end_dt = _parse_timestamp(finished_at)

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
                point_dt = _parse_timestamp(record["timestamp"])
                cpu_pct = float(record["cpu_pct"])
                mem_used_pct = float(record["mem_used_pct"])
                mem_used_bytes = float(record["mem_used_bytes"])
            except (KeyError, TypeError, ValueError):
                # handle sampler kill signal or other malformed lines 
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
    if len(rows) < 2:
        return None
    deltas = sorted(b[1] - a[1] for a, b in zip(rows, rows[1:]))
    return round(deltas[len(deltas) // 2], 3)
