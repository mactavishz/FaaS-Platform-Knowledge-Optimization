import json
import warnings
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path

import polars as pl

from .constants import (
    ENTRY_FUNCTIONS,
    EXPECTED_ITERATIONS,
    PLATFORMS,
    PROFILES,
    TRIM_HEAD,
    TRIM_TAIL,
    WEBSHOP_OPERATIONS,
    WEBSHOP_OPERATION_CALL_GRAPHS,
    WORKFLOW_CALL_GRAPHS,
    WORKFLOW_METRICS,
    WORKFLOWS,
)


@dataclass(frozen=True)
class ValidationError:
    run: str
    profile: str
    platform: str
    workflow: str
    severity: str
    message: str


def load_latency_samples(
    results: Path,
    runs: tuple[str, ...],
    *,
    allow_incomplete: bool = False,
) -> tuple[pl.DataFrame, pl.DataFrame]:
    frames: list[pl.DataFrame] = []
    errors: list[ValidationError] = []

    for run in runs:
        for profile in PROFILES:
            for platform in PLATFORMS:
                for workflow in WORKFLOWS:
                    experiment_dir = results / run / profile / platform / workflow
                    experiment_errors = validate_experiment(experiment_dir, run, profile, platform, workflow)
                    errors.extend(experiment_errors)
                    experiment_samples = _load_experiment_metrics(
                        experiment_dir, run, profile, platform, workflow
                    )
                    errors.extend(
                        _validate_metric_counts(experiment_samples, run, profile, platform, workflow)
                    )
                    frames.append(experiment_samples)

    samples = pl.concat(frames, how="vertical_relaxed") if frames else _empty_latency_frame()
    # Webshop scripts record per-step metrics. Add a computed journey metric so
    # the distribution/summary plots can compare the full user flow directly.
    samples = _add_journey_samples(samples)
    # Mark the retained steady-state range once during ingestion; all later
    # aggregation and plotting code reads this boolean instead of duplicating
    # trim logic.
    samples = _mark_retained(samples)

    # Validate after journey synthesis and trimming so the error messages match
    # the exact sample set used by the analysis.
    retained_counts = (
        samples.filter(pl.col("retained"))
        .group_by(["run", "profile", "platform", "workflow", "metric"])
        .len(name="retained_samples")
    )
    for row in retained_counts.iter_rows(named=True):
        if row["retained_samples"] != EXPECTED_ITERATIONS - TRIM_HEAD - TRIM_TAIL:
            errors.append(
                ValidationError(
                    row["run"],
                    row["profile"],
                    row["platform"],
                    row["workflow"],
                    "error",
                    f"{row['metric']} has {row['retained_samples']} retained samples",
                )
            )

    validation = _errors_to_frame(errors)
    errors = validation.filter(pl.col("severity") == "error").height if validation.height else 0
    if errors and not allow_incomplete:
        details = "\n".join(validation.filter(pl.col("severity") == "error")["message"].head(10).to_list())
        raise ValueError(f"Benchmark validation failed with {errors} error(s):\n{details}")

    return samples, validation


def load_function_samples(
    results: Path,
    runs: tuple[str, ...],
    *,
    allow_incomplete: bool = False,
) -> pl.DataFrame:
    frames: list[pl.DataFrame] = []

    for run in runs:
        for profile in PROFILES:
            for platform in PLATFORMS:
                for workflow in WORKFLOWS:
                    experiment_dir = results / run / profile / platform / workflow
                    frames.append(_load_experiment_functions(experiment_dir, run, profile, platform, workflow))

    samples = pl.concat(frames, how="vertical_relaxed") if frames else _empty_function_frame()
    if samples.is_empty():
        if allow_incomplete:
            return samples
        raise ValueError("No function invocation samples found")

    # Attach the workflow entry function name to every function invocation.
    # The alignment code uses this to build per-entry timing windows.
    entry_names = pl.DataFrame(
        {
            "workflow": list(ENTRY_FUNCTIONS.keys()),
            "entry_function": list(ENTRY_FUNCTIONS.values()),
        }
    )
    samples = samples.join(entry_names, on="workflow", how="left")

    # Webshop needs operation-aware alignment because one k6 iteration invokes
    # the frontend two or three times. IoT/tree have one entry invocation per
    # benchmark iteration and can use the generic workflow alignment.
    generic = samples.filter(~pl.col("workflow").is_in(list(WEBSHOP_OPERATIONS)))
    webshop = samples.filter(pl.col("workflow").is_in(list(WEBSHOP_OPERATIONS)))

    frames = [_align_generic_function_samples(generic), _align_webshop_function_samples(webshop)]
    frames = [frame for frame in frames if not frame.is_empty()]
    return pl.concat(frames, how="vertical_relaxed") if frames else _empty_aligned_function_frame()


def load_resource_samples(
    results: Path,
    runs: tuple[str, ...],
) -> pl.DataFrame:
    """Load cached VM host-metric CSVs written by ``monitoring.fetch_resource_samples``.

    Each experiment may contribute a ``resources/vm-usage.csv`` time series of VM
    CPU and memory usage over its measured k6 window. Missing files are skipped
    silently because not every result directory has been backfilled; the latency
    and function analyses do not depend on this data.
    """
    frames: list[pl.DataFrame] = []

    for run in runs:
        for profile in PROFILES:
            for platform in PLATFORMS:
                for workflow in WORKFLOWS:
                    csv_path = results / run / profile / platform / workflow / "resources" / "vm-usage.csv"
                    if not csv_path.exists():
                        continue
                    df = pl.read_csv(
                        csv_path,
                        schema_overrides={
                            "timestamp": pl.Utf8,
                            "seconds_since_start": pl.Float64,
                            "cpu_pct": pl.Float64,
                            "mem_used_pct": pl.Float64,
                            "mem_used_bytes": pl.Float64,
                        },
                    )
                    if df.is_empty():
                        continue
                    frames.append(
                        df.with_columns(
                            pl.lit(run).alias("run"),
                            pl.lit(profile).alias("profile"),
                            pl.lit(platform).alias("platform"),
                            pl.lit(workflow).alias("workflow"),
                        ).select(_empty_resource_frame().columns)
                    )

    return pl.concat(frames, how="vertical_relaxed") if frames else _empty_resource_frame()


def _align_generic_function_samples(samples: pl.DataFrame) -> pl.DataFrame:
    if samples.is_empty():
        return _empty_aligned_function_frame()

    # Entry invocations define iteration windows. The next entry start is the
    # upper bound, not the current entry finish, because async descendants can
    # legitimately continue after the entry function has returned.
    entry_samples = (
        samples.filter(pl.col("function") == pl.col("entry_function"))
        .select(
            "run",
            "profile",
            "platform",
            "workflow",
            pl.col("operation_index").alias("entry_operation_index"),
            pl.col("started_at_ns").alias("entry_started_at_ns"),
            pl.col("finished_at_ns").alias("entry_finished_at_ns"),
        )
        .sort(["run", "profile", "platform", "workflow", "entry_started_at_ns"])
        .with_columns(
            pl.col("entry_started_at_ns")
            .shift(-1)
            .over(["run", "profile", "platform", "workflow"])
            .alias("next_entry_started_at_ns")
        )
    )
    if entry_samples.is_empty():
        return _empty_aligned_function_frame()

    # Match each invocation to the latest entry that started at or before it.
    # This gives every function call an entry window, then the filter below
    # removes calls that fall outside the window or outside the explicit graph.
    with warnings.catch_warnings():
        warnings.filterwarnings("ignore", message="Sortedness of columns cannot be checked.*")
        aligned = samples.sort("started_at_ns").join_asof(
            entry_samples.sort("entry_started_at_ns"),
            left_on="started_at_ns",
            right_on="entry_started_at_ns",
            by=["run", "profile", "platform", "workflow"],
            strategy="backward",
        )
    # Normalize generic workflows into the same schema used by webshop. The
    # operation fields stay null for IoT/tree, but shared plotting code can
    # still consume the aligned frame uniformly.
    aligned = aligned.with_columns(
        ((pl.col("finished_at_ns") - pl.col("entry_started_at_ns")) / 1_000_000).alias(
            "relative_finished_ms"
        ),
        pl.col("entry_operation_index").alias("iteration_index"),
        pl.lit(None, dtype=pl.UInt32).alias("operation_step_index"),
        pl.lit(None, dtype=pl.Utf8).alias("operation"),
    )
    # The final retained set must satisfy both timing and topology:
    # invocation starts within the matched entry window, iteration is in the
    # steady-state trim range, and the function is reachable from the workflow
    # entry in the explicit call graph.
    aligned = aligned.filter(
        (
            pl.col("started_at_ns").is_between(
                pl.col("entry_started_at_ns"),
                pl.col("next_entry_started_at_ns"),
                closed="left",
            )
            | (
                (pl.col("started_at_ns") >= pl.col("entry_started_at_ns"))
                & pl.col("next_entry_started_at_ns").is_null()
            )
        )
        & pl.col("iteration_index").is_between(
            TRIM_HEAD, EXPECTED_ITERATIONS - TRIM_TAIL - 1, closed="both"
        )
        & _workflow_reachable_expr()
    )
    return _select_aligned_function_columns(aligned.filter(pl.col("relative_finished_ms").is_not_null()))


def _align_webshop_function_samples(samples: pl.DataFrame) -> pl.DataFrame:
    frames: list[pl.DataFrame] = []
    for workflow, operations in WEBSHOP_OPERATIONS.items():
        workflow_samples = samples.filter(pl.col("workflow") == workflow)
        if workflow_samples.is_empty():
            continue

        operation_count = len(operations)
        # The k6 webshop scripts call frontend operations in a fixed sequence.
        # Convert the ordered frontend invocation index into the journey number
        # and operation label used for subplotting and filtering.
        operation_names = pl.DataFrame(
            {
                "operation_step_index": list(range(operation_count)),
                "operation": list(operations),
            },
            schema={"operation_step_index": pl.UInt32, "operation": pl.Utf8},
        )
        entry_windows = (
            workflow_samples.filter(pl.col("function") == pl.col("entry_function"))
            .with_columns(
                (pl.col("operation_index") // operation_count).alias("journey_index"),
                (pl.col("operation_index") % operation_count).alias("operation_step_index"),
            )
            .join(operation_names, on="operation_step_index", how="left")
            .select(
                "run",
                "profile",
                "platform",
                "workflow",
                "operation",
                "operation_step_index",
                pl.col("journey_index").alias("iteration_index"),
                pl.col("operation_index").alias("entry_operation_index"),
                pl.col("started_at_ns").alias("entry_started_at_ns"),
                pl.col("finished_at_ns").alias("entry_finished_at_ns"),
            )
            .sort(["run", "profile", "platform", "workflow", "entry_started_at_ns"])
            .with_columns(
                pl.col("entry_started_at_ns")
                .shift(-1)
                .over(["run", "profile", "platform", "workflow"])
                .alias("next_entry_started_at_ns")
            )
        )
        if entry_windows.is_empty():
            continue

        # As with IoT/tree, assign each function invocation to the latest
        # frontend operation that started before it. The operation-specific
        # graph filter below prevents functions from leaking into unrelated
        # webshop operation panels.
        with warnings.catch_warnings():
            warnings.filterwarnings("ignore", message="Sortedness of columns cannot be checked.*")
            aligned = workflow_samples.sort("started_at_ns").join_asof(
                entry_windows.sort("entry_started_at_ns"),
                left_on="started_at_ns",
                right_on="entry_started_at_ns",
                by=["run", "profile", "platform", "workflow"],
                strategy="backward",
            )
        # Use the next frontend invocation as the operation boundary. This
        # keeps async checkout side effects if they outlive the checkout
        # frontend response, while still excluding the next operation/journey.
        aligned = aligned.filter(
            (
                pl.col("started_at_ns").is_between(
                    pl.col("entry_started_at_ns"),
                    pl.col("next_entry_started_at_ns"),
                    closed="left",
                )
                | (
                    (pl.col("started_at_ns") >= pl.col("entry_started_at_ns"))
                    & pl.col("next_entry_started_at_ns").is_null()
                )
            )
            & _webshop_reachable_expr()
        )
        # Plot function finish times relative to the operation start so all
        # downstream functions share the same x/y reference point per subplot.
        aligned = aligned.with_columns(
            ((pl.col("finished_at_ns") - pl.col("entry_started_at_ns")) / 1_000_000).alias(
                "relative_finished_ms"
            )
        )
        aligned = aligned.filter(
            pl.col("iteration_index").is_between(
                TRIM_HEAD, EXPECTED_ITERATIONS - TRIM_TAIL - 1, closed="both"
            )
        )
        frames.append(_select_aligned_function_columns(aligned.filter(pl.col("relative_finished_ms").is_not_null())))

    return pl.concat(frames, how="vertical_relaxed") if frames else _empty_aligned_function_frame()


def _select_aligned_function_columns(samples: pl.DataFrame) -> pl.DataFrame:
    return samples.select(_empty_aligned_function_frame().columns)


def _workflow_reachable_expr() -> pl.Expr:
    # Build a Polars predicate from the explicit workflow graphs. Each row is
    # kept only if its function is reachable from the workflow entry.
    expr = pl.lit(False)
    for workflow, graph in WORKFLOW_CALL_GRAPHS.items():
        reachable = _reachable_functions(ENTRY_FUNCTIONS[workflow], graph)
        expr = expr | ((pl.col("workflow") == workflow) & pl.col("function").is_in(sorted(reachable)))
    return expr


def _webshop_reachable_expr() -> pl.Expr:
    # Webshop reachability depends on the frontend operation. A function such
    # as cartstorage can appear in multiple operations, while functions outside
    # the current operation graph are filtered even if timings overlap.
    expr = pl.lit(False)
    for operation, graph in WEBSHOP_OPERATION_CALL_GRAPHS.items():
        reachable = _reachable_functions("webshop-frontend", graph)
        expr = expr | ((pl.col("operation") == operation) & pl.col("function").is_in(sorted(reachable)))
    return expr


def _reachable_functions(entry: str, graph: dict[str, tuple[str, ...]]) -> set[str]:
    # Traverse the direct edge map so constants can stay close to the README
    # topology while filters use a complete transitive reachable set.
    seen: set[str] = set()
    stack = [entry]
    while stack:
        function = stack.pop()
        if function in seen:
            continue
        seen.add(function)
        stack.extend(graph.get(function, ()))
    return seen


def validate_experiment(
    experiment_dir: Path,
    run: str,
    profile: str,
    platform: str,
    workflow: str,
) -> list[ValidationError]:
    errors: list[ValidationError] = []
    metrics_csv = experiment_dir / "k6" / "metrics.csv"
    summary_json = experiment_dir / "k6" / "summary.json"
    metadata_json = experiment_dir / "metadata.json"

    if not metrics_csv.exists():
        errors.append(_verror(run, profile, platform, workflow, "error", f"Missing {metrics_csv}"))
    if not summary_json.exists():
        errors.append(_verror(run, profile, platform, workflow, "error", f"Missing {summary_json}"))
    if not metadata_json.exists():
        errors.append(_verror(run, profile, platform, workflow, "error", f"Missing {metadata_json}"))

    if metadata_json.exists():
        metadata = json.loads(metadata_json.read_text())
        if metadata.get("status") != "success":
            errors.append(
                _verror(
                    run,
                    profile,
                    platform,
                    workflow,
                    "error",
                    f"{metadata_json} status is {metadata.get('status')!r}",
                )
            )

    if summary_json.exists():
        summary = json.loads(summary_json.read_text())
        metrics = summary.get("metrics", {})
        checks = metrics.get("checks", {})
        if int(checks.get("fails", 0) or 0) > 0:
            errors.append(_verror(run, profile, platform, workflow, "error", "k6 checks failed"))
        for name in ("invoke_failures", "list_failures", "state_validation_failures", "scale_down_timeouts"):
            metric = metrics.get(name)
            if metric and _metric_count(metric) > 0:
                errors.append(_verror(run, profile, platform, workflow, "error", f"{name} is non-zero"))

    return errors


def _load_experiment_metrics(
    experiment_dir: Path,
    run: str,
    profile: str,
    platform: str,
    workflow: str,
) -> pl.DataFrame:
    metrics_csv = experiment_dir / "k6" / "metrics.csv"
    wanted = WORKFLOW_METRICS[workflow]
    if not metrics_csv.exists():
        return _empty_latency_frame()

    df = pl.read_csv(
        metrics_csv,
        columns=["metric_name", "timestamp", "metric_value", "extra_tags"],
        schema_overrides={"metric_name": pl.Utf8, "timestamp": pl.Int64, "metric_value": pl.Float64},
    )
    df = (
        df.with_row_index("source_row")
        .filter(pl.col("metric_name").is_in(wanted))
        .sort("source_row")
        # k6 metric rows arrive interleaved. Count within each metric after
        # restoring source order to recover the benchmark iteration index for
        # every latency sample.
        # cum_count().over("metric_name") here is to count the nth occurrence of each metric
        .with_columns((pl.col("metric_name").cum_count().over("metric_name") - 1).alias("iteration_index"))
        .with_columns(
            pl.lit(run).alias("run"),
            pl.lit(profile).alias("profile"),
            pl.lit(platform).alias("platform"),
            pl.lit(workflow).alias("workflow"),
            pl.col("metric_name").alias("metric"),
            pl.col("metric_value").alias("latency_ms"),
            pl.lit("k6").alias("source"),
        )
        .select(
            "run",
            "profile",
            "platform",
            "workflow",
            "metric",
            "iteration_index",
            "timestamp",
            "latency_ms",
            "source",
            "extra_tags",
        )
    )
    return df


def _validate_metric_counts(
    samples: pl.DataFrame,
    run: str,
    profile: str,
    platform: str,
    workflow: str,
) -> list[ValidationError]:
    errors: list[ValidationError] = []
    counts = {
        row["metric"]: row["sample_count"]
        for row in samples.group_by("metric").len(name="sample_count").iter_rows(named=True)
    }
    for metric in WORKFLOW_METRICS[workflow]:
        count = counts.get(metric, 0)
        if count != EXPECTED_ITERATIONS:
            errors.append(
                _verror(
                    run,
                    profile,
                    platform,
                    workflow,
                    "error",
                    f"{metric} has {count} samples, expected {EXPECTED_ITERATIONS}",
                )
            )
    return errors


def _add_journey_samples(samples: pl.DataFrame) -> pl.DataFrame:
    webshop = samples.filter(
        pl.col("workflow").is_in(("webshop-browse-addcart-checkout", "webshop-addcart-checkout"))
    )
    if webshop.is_empty():
        return samples

    # Sum the step latencies for each webshop journey. The earliest timestamp
    # represents the journey start for sorting/retention, while the source marks
    # this as derived data instead of a raw k6 metric.
    journey = (
        webshop.group_by(["run", "profile", "platform", "workflow", "iteration_index"])
        .agg(
            pl.col("latency_ms").sum().alias("latency_ms"),
            pl.col("timestamp").min().alias("timestamp"),
        )
        .with_columns(
            pl.lit("journey_latency_ms").alias("metric"),
            pl.lit("computed").alias("source"),
            pl.lit("").alias("extra_tags"),
        )
        .select(samples.columns)
    )
    return pl.concat([samples, journey], how="vertical")


def _mark_retained(samples: pl.DataFrame) -> pl.DataFrame:
    # Only complete metrics should contribute to retained analysis. This avoids
    # treating partial failed runs as valid just because their iteration index
    # falls inside the trim window.
    counts = samples.group_by(["run", "profile", "platform", "workflow", "metric"]).len(name="sample_count")
    samples = samples.join(counts, on=["run", "profile", "platform", "workflow", "metric"], how="left")
    return samples.with_columns(
        (
            (pl.col("sample_count") == EXPECTED_ITERATIONS)
            & pl.col("iteration_index").is_between(
                TRIM_HEAD, EXPECTED_ITERATIONS - TRIM_TAIL - 1, closed="both"
            )
        ).alias("retained")
    )


def _load_experiment_functions(
    experiment_dir: Path,
    run: str,
    profile: str,
    platform: str,
    workflow: str,
) -> pl.DataFrame:
    functions_dir = experiment_dir / "stats" / "functions"
    if not functions_dir.exists():
        return _empty_function_frame()

    rows: list[dict[str, object]] = []
    for path in sorted(functions_dir.glob("*.json")):
        payload = json.loads(path.read_text())
        function = payload.get("function", {}).get("name", path.stem)
        for operation_index, invocation in enumerate(payload.get("invocations", [])):
            if not invocation.get("success", False):
                continue
            rows.append(
                {
                    "run": run,
                    "profile": profile,
                    "platform": platform,
                    "workflow": workflow,
                    "function": function,
                    "operation_index": operation_index,
                    "started_at_ns": _parse_rfc3339_ns(invocation["started_at"]),
                    "finished_at_ns": _parse_rfc3339_ns(invocation["finished_at"]),
                    "duration_ms": float(invocation["duration_ns"]) / 1_000_000,
                    "method": invocation.get("method", ""),
                    "path": invocation.get("path", ""),
                    "status_code": int(invocation.get("status_code", 0) or 0),
                }
            )

    return pl.DataFrame(rows) if rows else _empty_function_frame()


def _parse_rfc3339_ns(value: str) -> int:
    text = value.removesuffix("Z")
    if "." in text:
        prefix, fraction = text.split(".", 1)
        fraction = (fraction + "000000000")[:9]
    else:
        prefix, fraction = text, "000000000"
    seconds = int(datetime.fromisoformat(prefix).replace(tzinfo=timezone.utc).timestamp())
    return seconds * 1_000_000_000 + int(fraction)


def _metric_count(metric: dict[str, object]) -> int:
    for key in ("count", "passes", "value"):
        value = metric.get(key)
        if value is not None:
            return int(value)
    return 0


def _verror(
    run: str,
    profile: str,
    platform: str,
    workflow: str,
    severity: str,
    message: str,
) -> ValidationError:
    return ValidationError(run, profile, platform, workflow, severity, message)


def _errors_to_frame(errors: list[ValidationError]) -> pl.DataFrame:
    if not errors:
        return pl.DataFrame(
            schema={
                "run": pl.Utf8,
                "profile": pl.Utf8,
                "platform": pl.Utf8,
                "workflow": pl.Utf8,
                "severity": pl.Utf8,
                "message": pl.Utf8,
            }
        )
    return pl.DataFrame([err.__dict__ for err in errors])


def _empty_latency_frame() -> pl.DataFrame:
    return pl.DataFrame(
        schema={
            "run": pl.Utf8,
            "profile": pl.Utf8,
            "platform": pl.Utf8,
            "workflow": pl.Utf8,
            "metric": pl.Utf8,
            "iteration_index": pl.UInt32,
            "timestamp": pl.Int64,
            "latency_ms": pl.Float64,
            "source": pl.Utf8,
            "extra_tags": pl.Utf8,
        }
    )


def _empty_resource_frame() -> pl.DataFrame:
    return pl.DataFrame(
        schema={
            "run": pl.Utf8,
            "profile": pl.Utf8,
            "platform": pl.Utf8,
            "workflow": pl.Utf8,
            "timestamp": pl.Utf8,
            "seconds_since_start": pl.Float64,
            "cpu_pct": pl.Float64,
            "mem_used_pct": pl.Float64,
            "mem_used_bytes": pl.Float64,
        }
    )


def _empty_function_frame() -> pl.DataFrame:
    return pl.DataFrame(
        schema={
            "run": pl.Utf8,
            "profile": pl.Utf8,
            "platform": pl.Utf8,
            "workflow": pl.Utf8,
            "function": pl.Utf8,
            "operation_index": pl.UInt32,
            "started_at_ns": pl.Int64,
            "finished_at_ns": pl.Int64,
            "duration_ms": pl.Float64,
            "method": pl.Utf8,
            "path": pl.Utf8,
            "status_code": pl.Int64,
        }
    )


def _empty_aligned_function_frame() -> pl.DataFrame:
    return pl.DataFrame(
        schema={
            "run": pl.Utf8,
            "profile": pl.Utf8,
            "platform": pl.Utf8,
            "workflow": pl.Utf8,
            "function": pl.Utf8,
            "operation_index": pl.UInt32,
            "started_at_ns": pl.Int64,
            "finished_at_ns": pl.Int64,
            "duration_ms": pl.Float64,
            "method": pl.Utf8,
            "path": pl.Utf8,
            "status_code": pl.Int64,
            "entry_function": pl.Utf8,
            "entry_operation_index": pl.UInt32,
            "entry_started_at_ns": pl.Int64,
            "entry_finished_at_ns": pl.Int64,
            "next_entry_started_at_ns": pl.Int64,
            "iteration_index": pl.UInt32,
            "operation_step_index": pl.UInt32,
            "operation": pl.Utf8,
            "relative_finished_ms": pl.Float64,
        }
    )
