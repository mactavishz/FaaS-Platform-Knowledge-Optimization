"""Backfill VM host metrics (CPU, memory) from Google Cloud Monitoring.

Every benchmark VM runs the Google Cloud Ops Agent (installed during
provisioning), which ships host metrics to Cloud Monitoring. This module fetches
those metrics for each experiment's measured window -- the same
``[k6_run_started_at, k6_run_finished_at]`` interval recorded in ``metadata.json``
-- and caches them to ``<experiment>/resources/vm-usage.csv``.

Because k6 runs on the orchestrator host rather than on the VM, the VM-level
metrics reflect only the FaaS platform plus the deployed functions, so they are a
clean measure of the prewarm optimization's resource overhead.

Two metric sources are combined:

- CPU: ``compute.googleapis.com/instance/cpu/utilization`` (GCE built-in,
  filtered by ``metric.label.instance_name``). This also exposes the numeric
  ``instance_id`` needed to query the agent metrics.
- Memory: ``agent.googleapis.com/memory/{percent_used,bytes_used}`` with
  ``state="used"`` (Ops Agent, filtered by ``resource.label.instance_id``).

Further information on the metrics is available in the Cloud Monitoring documentation:
- https://docs.cloud.google.com/monitoring/api/metrics_gcp

The Ops Agent samples at 60s by default, so a ~37 minute run yields ~37 points.
That coarse series is the *fallback* for historical runs only: newer runs carry
a 5s on-VM sample file (``resources/vm-samples.csv``, see ``resources.py``),
which takes precedence -- this backfill skips any experiment that has one.

Authentication prefers a service account taken from the environment --
``GOOGLE_CREDENTIALS`` (the variable the benchmark ``.envrc`` and Terraform
already set) or ``GOOGLE_APPLICATION_CREDENTIALS`` -- each of which may hold raw
JSON or a path to a key file. This lets the backfill run in the same shell that
runs the benchmark with no extra auth step. When neither is set it falls back to
the active ``gcloud`` identity. Either way the identity needs
``roles/monitoring.viewer`` on the benchmark project, which is taken from
``--project`` or ``GOOGLE_PROJECT``.
"""

import csv
import json
import os
import subprocess
import urllib.parse
import urllib.request
from datetime import datetime, timezone
from pathlib import Path

from .constants import PLATFORMS, PROFILES, WORKFLOWS

_MONITORING_URL = "https://monitoring.googleapis.com/v3/projects/{project}/timeSeries"
_MONITORING_SCOPE = "https://www.googleapis.com/auth/monitoring.read"
_ALIGNMENT_PERIOD_S = 60

RESOURCE_CSV_COLUMNS = (
    "timestamp",
    "seconds_since_start",
    "cpu_pct",
    "mem_used_pct",
    "mem_used_bytes",
)


def fetch_resource_samples(
    results: Path,
    runs: tuple[str, ...],
    *,
    project: str,
    refresh: bool = False,
) -> None:
    """Fetch and cache VM host metrics for every experiment in ``runs``.

    Each experiment writes ``resources/vm-usage.csv``. Existing files are reused
    unless ``refresh`` is set. Missing instances or windows are warned about and
    skipped so a single gap never aborts the whole backfill.
    """
    if not project:
        raise ValueError(
            "A Google Cloud project is required to fetch host metrics. "
            "Pass --project or set GOOGLE_PROJECT."
        )

    token = _access_token()
    for run in runs:
        for profile in PROFILES:
            for platform in PLATFORMS:
                for workflow in WORKFLOWS:
                    experiment_dir = results / run / profile / platform / workflow
                    _fetch_one_experiment(
                        experiment_dir, project, token, refresh=refresh
                    )


def _fetch_one_experiment(
    experiment_dir: Path,
    project: str,
    token: str,
    *,
    refresh: bool,
) -> None:
    metadata_path = experiment_dir / "metadata.json"
    if not metadata_path.exists():
        return

    # Local sampler data wins; resources.materialize_resource_usage derives
    # vm-usage.csv from it, so never overwrite that with the coarse cloud series.
    if (experiment_dir / "resources" / "vm-samples.csv").exists():
        return

    output_path = experiment_dir / "resources" / "vm-usage.csv"
    if output_path.exists() and not refresh:
        return

    metadata = json.loads(metadata_path.read_text())
    instance_name = metadata.get("instance_name")
    started_at = metadata.get("k6_run_started_at")
    finished_at = metadata.get("k6_run_finished_at")
    label = experiment_dir.relative_to(experiment_dir.parents[3])

    if not (instance_name and started_at and finished_at):
        print(f"  skip {label}: missing instance_name or k6 window in metadata")
        return

    start_dt = _parse_rfc3339(started_at)
    end_dt = _parse_rfc3339(finished_at)

    cpu_series = _query_timeseries(
        project,
        token,
        filter_str=(
            'metric.type="compute.googleapis.com/instance/cpu/utilization" '
            f'AND metric.label.instance_name="{instance_name}"'
        ),
        start=started_at,
        end=finished_at,
    )
    if not cpu_series:
        print(f"  skip {label}: no CPU data for instance {instance_name}")
        return

    # The built-in CPU metric carries the numeric instance_id on its monitored
    # resource; the agent memory metrics are keyed by that id, not the name.
    instance_id = cpu_series[0]["resource"]["labels"].get("instance_id")
    cpu_by_ts = _points_by_timestamp(cpu_series, scale=100.0)

    mem_pct_by_ts = _points_by_timestamp(
        _query_timeseries(
            project,
            token,
            filter_str=(
                'metric.type="agent.googleapis.com/memory/percent_used" '
                f'AND resource.label.instance_id="{instance_id}" '
                'AND metric.label.state="used"'
            ),
            start=started_at,
            end=finished_at,
        )
    )
    mem_bytes_by_ts = _points_by_timestamp(
        _query_timeseries(
            project,
            token,
            filter_str=(
                'metric.type="agent.googleapis.com/memory/bytes_used" '
                f'AND resource.label.instance_id="{instance_id}" '
                'AND metric.label.state="used"'
            ),
            start=started_at,
            end=finished_at,
        )
    )

    rows = _build_rows(start_dt, end_dt, cpu_by_ts, mem_pct_by_ts, mem_bytes_by_ts)
    if not rows:
        print(f"  skip {label}: no points inside the measured window")
        return

    output_path.parent.mkdir(parents=True, exist_ok=True)
    with output_path.open("w", newline="") as handle:
        writer = csv.writer(handle)
        writer.writerow(RESOURCE_CSV_COLUMNS)
        writer.writerows(rows)
    meta_path = experiment_dir / "resources" / "vm-usage.meta.json"
    meta_path.write_text(
        json.dumps(
            {"source": "cloud-monitoring", "interval_s": _ALIGNMENT_PERIOD_S}, indent=2
        )
        + "\n"
    )
    print(f"  wrote {label}/resources/vm-usage.csv ({len(rows)} points, cloud-monitoring)")


def _build_rows(
    start_dt: datetime,
    end_dt: datetime,
    cpu_by_ts: dict[str, float],
    mem_pct_by_ts: dict[str, float],
    mem_bytes_by_ts: dict[str, float],
) -> list[tuple[str, float, float | str, float | str, float | str]]:
    # Union the timestamps so a missing memory (or CPU) sample at one minute does
    # not drop the whole row; absent values are written as empty cells.
    timestamps = sorted(set(cpu_by_ts) | set(mem_pct_by_ts) | set(mem_bytes_by_ts))
    rows: list[tuple[str, float, float | str, float | str, float | str]] = []
    for timestamp in timestamps:
        point_dt = _parse_rfc3339(timestamp)
        if not (start_dt <= point_dt <= end_dt):
            continue
        seconds = (point_dt - start_dt).total_seconds()
        rows.append(
            (
                timestamp,
                round(seconds, 3),
                _cell(cpu_by_ts.get(timestamp)),
                _cell(mem_pct_by_ts.get(timestamp)),
                _cell(mem_bytes_by_ts.get(timestamp)),
            )
        )
    return rows


def _cell(value: float | None) -> float | str:
    return "" if value is None else round(value, 4)


def _query_timeseries(
    project: str,
    token: str,
    *,
    filter_str: str,
    start: str,
    end: str,
) -> list[dict]:
    base = _MONITORING_URL.format(project=urllib.parse.quote(project))
    params = {
        "filter": filter_str,
        "interval.startTime": start,
        "interval.endTime": end,
        "aggregation.alignmentPeriod": f"{_ALIGNMENT_PERIOD_S}s",
        "aggregation.perSeriesAligner": "ALIGN_MEAN",
    }
    series: list[dict] = []
    page_token = ""
    while True:
        if page_token:
            params["pageToken"] = page_token
        url = f"{base}?{urllib.parse.urlencode(params)}"
        request = urllib.request.Request(url, headers={"Authorization": f"Bearer {token}"})
        with urllib.request.urlopen(request, timeout=60) as response:
            payload = json.loads(response.read().decode())
        series.extend(payload.get("timeSeries", []))
        page_token = payload.get("nextPageToken", "")
        if not page_token:
            return series


def _points_by_timestamp(series: list[dict], *, scale: float = 1.0) -> dict[str, float]:
    # Aligned series share a 60s grid keyed by point endTime, so endTime is a safe
    # join key across the CPU and memory metrics.
    points: dict[str, float] = {}
    for entry in series:
        for point in entry.get("points", []):
            timestamp = point["interval"]["endTime"]
            value = point["value"].get("doubleValue")
            if value is None:
                value = point["value"].get("int64Value")
            if value is None:
                continue
            points[timestamp] = float(value) * scale
    return points


def _access_token() -> str:
    """Obtain a Cloud Monitoring read token.

    Prefers a service account from the environment so the fetch is frictionless
    in the benchmark shell; falls back to the active gcloud identity otherwise.
    """
    token = _service_account_token()
    if token:
        return token
    return _gcloud_token()


def _service_account_token() -> str | None:
    info = _service_account_info()
    if info is None:
        return None
    try:
        import google.auth.transport.requests
        from google.oauth2 import service_account
    except ImportError as error:  # pragma: no cover - dependency declared in pyproject
        raise RuntimeError(
            "google-auth and requests are required to mint a token from "
            "GOOGLE_CREDENTIALS. Install this tool's dependencies, or unset "
            "GOOGLE_CREDENTIALS to fall back to gcloud."
        ) from error

    credentials = service_account.Credentials.from_service_account_info(
        info, scopes=[_MONITORING_SCOPE]
    )
    credentials.refresh(google.auth.transport.requests.Request())
    return credentials.token


def _service_account_info() -> dict | None:
    # GOOGLE_CREDENTIALS (Terraform's convention) and GOOGLE_APPLICATION_CREDENTIALS
    # may each hold either raw service-account JSON or a path to a key file.
    for variable in ("GOOGLE_CREDENTIALS", "GOOGLE_APPLICATION_CREDENTIALS"):
        raw = os.environ.get(variable)
        if not raw:
            continue
        stripped = raw.strip()
        if stripped.startswith("{"):
            return json.loads(stripped)
        path = Path(stripped).expanduser()
        if path.exists():
            return json.loads(path.read_text())
    return None


def _gcloud_token() -> str:
    try:
        result = subprocess.run(
            ["gcloud", "auth", "print-access-token"],
            capture_output=True,
            text=True,
            check=True,
        )
    except (OSError, subprocess.CalledProcessError) as error:
        raise RuntimeError(
            "Could not obtain credentials. Set GOOGLE_CREDENTIALS (service-account "
            "JSON or key path) as the benchmark .envrc does, or authenticate gcloud "
            "against an identity with roles/monitoring.viewer on the project."
        ) from error
    return result.stdout.strip()


def _parse_rfc3339(value: str) -> datetime:
    return datetime.fromisoformat(value.replace("Z", "+00:00")).astimezone(timezone.utc)


# Exposed for callers that want to iterate the same experiment grid as ingestion.
_EXPERIMENT_DIMENSIONS = (PROFILES, PLATFORMS, WORKFLOWS)
