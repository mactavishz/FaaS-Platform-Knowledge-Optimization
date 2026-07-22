from dataclasses import dataclass

import polars as pl


@dataclass(frozen=True)
class ResourceAnalysis:
    minutes: pl.DataFrame
    summary: pl.DataFrame
    validation: pl.DataFrame


def analyze_resources(
    samples: pl.DataFrame,
    *,
    expected_runs: tuple[str, ...] | None = None,
    expected_profiles: tuple[str, ...] | None = None,
) -> ResourceAnalysis:
    """Aggregate VM samples without treating time points as experiment repeats."""
    if samples.is_empty():
        if expected_runs or expected_profiles:
            validation = pl.DataFrame(
                [
                    _resource_error(
                        platform="",
                        workflow="",
                        message="No VM resource data found for the selected runs and profiles",
                    )
                ]
            )
        else:
            validation = _empty_resource_validation()
        return ResourceAnalysis(pl.DataFrame(), pl.DataFrame(), validation)

    scope = ["platform", "workflow"]
    expected_runs = expected_runs or tuple(samples["run"].unique().sort().to_list())
    expected_profiles = expected_profiles or tuple(
        samples["profile"].unique().sort().to_list()
    )
    validations = [
        _validate_resource_coverage(samples, expected_runs, expected_profiles),
        _validate_resource_metadata(samples, scope),
    ]
    validations = [frame for frame in validations if not frame.is_empty()]
    validation = (
        pl.concat(validations, how="vertical_relaxed")
        if validations
        else _empty_resource_validation()
    )
    if not validation.is_empty():
        return ResourceAnalysis(pl.DataFrame(), pl.DataFrame(), validation)

    experiment = [*scope, "profile", "run"]
    with_minutes = samples.with_columns(
        (pl.col("seconds_since_start") / 60).floor().cast(pl.Int64).alias("minute")
    )
    complete_counts = (
        with_minutes.group_by(experiment)
        .agg(
            (pl.col("seconds_since_start").max() / 60)
            .floor()
            .cast(pl.Int64)
            .alias("complete_minutes")
        )
    )
    shared_counts = complete_counts.group_by(scope).agg(
        pl.col("complete_minutes").min().alias("complete_minutes")
    )
    complete_samples = (
        with_minutes.join(shared_counts, on=scope, how="left")
        .filter(pl.col("minute") < pl.col("complete_minutes"))
    )
    run_minutes = (
        complete_samples.group_by([*experiment, "minute"])
        .agg(
            pl.col("cpu_pct").mean().alias("cpu_pct"),
            pl.col("mem_used_pct").mean().alias("mem_used_pct"),
            pl.col("cpu_pct").count().alias("cpu_samples"),
            pl.col("mem_used_pct").count().alias("mem_samples"),
        )
    )
    validation = _validate_resource_minutes(run_minutes, complete_counts, shared_counts)
    if not validation.is_empty():
        return ResourceAnalysis(pl.DataFrame(), pl.DataFrame(), validation)

    minutes = (
        run_minutes.group_by([*scope, "profile", "minute"])
        .agg(
            pl.len().alias("runs"),
            pl.col("cpu_pct").mean().alias("cpu_mean_pct"),
            pl.col("cpu_pct").std().alias("cpu_sd_pct"),
            pl.col("mem_used_pct").mean().alias("mem_mean_pct"),
            pl.col("mem_used_pct").std().alias("mem_sd_pct"),
        )
        .with_columns((pl.col("minute") + 0.5).alias("minute_midpoint"))
        .sort([*scope, "profile", "minute"])
    )

    run_summary = run_minutes.group_by(experiment).agg(
        pl.col("cpu_pct").mean().alias("cpu_mean_pct"),
        pl.col("mem_used_pct").mean().alias("mem_mean_pct"),
    )
    baseline = run_summary.filter(pl.col("profile") == "baseline").select(
        *scope,
        "run",
        pl.col("cpu_mean_pct").alias("baseline_cpu_mean_pct"),
        pl.col("mem_mean_pct").alias("baseline_mem_mean_pct"),
    )
    paired_summary = run_summary.join(
        baseline, on=[*scope, "run"], how="left"
    ).with_columns(
        (
            (pl.col("cpu_mean_pct") - pl.col("baseline_cpu_mean_pct"))
            / pl.col("baseline_cpu_mean_pct")
            * 100
        ).alias("cpu_increase_pct"),
        (
            (pl.col("mem_mean_pct") - pl.col("baseline_mem_mean_pct"))
            / pl.col("baseline_mem_mean_pct")
            * 100
        ).alias("mem_increase_pct"),
    )
    summary = (
        paired_summary.group_by([*scope, "profile"])
        .agg(
            pl.len().alias("runs"),
            pl.col("cpu_mean_pct").mean().alias("cpu_mean_pct"),
            pl.col("cpu_mean_pct").std().alias("cpu_sd_pct"),
            pl.col("cpu_increase_pct").mean().alias("cpu_increase_pct"),
            pl.col("cpu_increase_pct").std().alias("cpu_increase_sd_pct"),
            pl.col("mem_mean_pct").mean().alias("mem_mean_pct"),
            pl.col("mem_mean_pct").std().alias("mem_sd_pct"),
            pl.col("mem_increase_pct").mean().alias("mem_increase_pct"),
            pl.col("mem_increase_pct").std().alias("mem_increase_sd_pct"),
        )
        .sort([*scope, "profile"])
    )
    return ResourceAnalysis(minutes, summary, validation)


def _empty_resource_validation() -> pl.DataFrame:
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


def _validate_resource_metadata(samples: pl.DataFrame, scope: list[str]) -> pl.DataFrame:
    errors: list[dict[str, str]] = []
    sources = samples.group_by(scope).agg(pl.col("resource_source").unique().sort())
    for row in sources.iter_rows(named=True):
        values = row["resource_source"]
        if len(values) > 1:
            errors.append(
                _resource_error(
                    platform=row["platform"],
                    workflow=row["workflow"],
                    message="Resource samples mix instrumentation sources: "
                    + ", ".join(values),
                )
            )
    intervals = samples.group_by(scope).agg(pl.col("sample_interval_s").unique().sort())
    for row in intervals.iter_rows(named=True):
        values = row["sample_interval_s"]
        if len(values) > 1:
            errors.append(
                _resource_error(
                    platform=row["platform"],
                    workflow=row["workflow"],
                    message="Resource samples mix sampling intervals: "
                    + ", ".join(f"{value:g}" for value in values)
                    + " seconds",
                )
            )
    return pl.DataFrame(errors) if errors else _empty_resource_validation()


def _validate_resource_coverage(
    samples: pl.DataFrame,
    expected_runs: tuple[str, ...],
    expected_profiles: tuple[str, ...],
) -> pl.DataFrame:
    scopes = samples.select("platform", "workflow").unique().iter_rows(named=True)
    observed = {
        (row["platform"], row["workflow"], row["profile"], row["run"])
        for row in samples.select("platform", "workflow", "profile", "run")
        .unique()
        .iter_rows(named=True)
    }
    errors: list[dict[str, str]] = []
    for scope in scopes:
        for profile in expected_profiles:
            for run in expected_runs:
                if (scope["platform"], scope["workflow"], profile, run) not in observed:
                    errors.append(
                        _resource_error(
                            run=run,
                            profile=profile,
                            platform=scope["platform"],
                            workflow=scope["workflow"],
                            message=f"Missing resource data for {run}/{profile}",
                        )
                    )
    return pl.DataFrame(errors) if errors else _empty_resource_validation()


def _validate_resource_minutes(
    run_minutes: pl.DataFrame,
    complete_counts: pl.DataFrame,
    shared_counts: pl.DataFrame,
) -> pl.DataFrame:
    shared_by_scope = {
        (row["platform"], row["workflow"]): row["complete_minutes"]
        for row in shared_counts.iter_rows(named=True)
    }
    observed = {
        (row["platform"], row["workflow"], row["profile"], row["run"], row["minute"]): row
        for row in run_minutes.iter_rows(named=True)
    }
    errors: list[dict[str, str]] = []
    for row in complete_counts.iter_rows(named=True):
        limit = shared_by_scope[(row["platform"], row["workflow"])]
        for minute in range(limit):
            key = (
                row["platform"],
                row["workflow"],
                row["profile"],
                row["run"],
                minute,
            )
            if key not in observed:
                errors.append(
                    _resource_error(
                        run=row["run"],
                        profile=row["profile"],
                        platform=row["platform"],
                        workflow=row["workflow"],
                        message=f"Missing resource samples for {row['run']}/{row['profile']} "
                        f"at elapsed minute {minute}",
                    )
                )
                continue
            minute_row = observed[key]
            for count_column, label in (
                ("cpu_samples", "CPU"),
                ("mem_samples", "memory"),
            ):
                if minute_row[count_column] == 0:
                    errors.append(
                        _resource_error(
                            run=row["run"],
                            profile=row["profile"],
                            platform=row["platform"],
                            workflow=row["workflow"],
                            message=f"Missing {label} samples for "
                            f"{row['run']}/{row['profile']} at elapsed minute {minute}",
                        )
                    )
    return pl.DataFrame(errors) if errors else _empty_resource_validation()


def _resource_error(
    *,
    platform: str,
    workflow: str,
    message: str,
    run: str = "",
    profile: str = "",
) -> dict[str, str]:
    return {
        "run": run,
        "profile": profile,
        "platform": platform,
        "workflow": workflow,
        "severity": "error",
        "message": message,
    }


def retained_latency(samples: pl.DataFrame) -> pl.DataFrame:
    # Downstream tables and figures should only use the steady-state slice.
    # Keep the identifying dimensions with the latency value so later group-bys
    # cannot accidentally mix runs, platforms, workflows, or metrics.
    return samples.filter(pl.col("retained")).select(
        "run",
        "profile",
        "platform",
        "workflow",
        "metric",
        "iteration_index",
        "latency_ms",
        "source",
    )


def run_summary(samples: pl.DataFrame) -> pl.DataFrame:
    retained = retained_latency(samples)
    # First summarize each repeated run independently. The analysis
    # treats runs as the experimental repeats, so this layer must not pool
    # samples across run boundaries.
    return (
        retained.group_by(["run", "profile", "platform", "workflow", "metric"])
        .agg(
            pl.len().alias("n"),
            pl.col("latency_ms").mean().alias("mean_ms"),
            pl.col("latency_ms").quantile(0.90, interpolation="linear").alias("p90_ms"),
        )
        .sort(["platform", "workflow", "metric", "profile", "run"])
    )


def run_improvements(run_stats: pl.DataFrame) -> pl.DataFrame:
    # Compute the latency improvement of every profile *per run*, pairing each
    # profile against the baseline measured in the SAME run.
    #
    # Pairing within a run (rather than dividing the two aggregate means once)
    # matters because every run is a fresh, independent re-deployment of all
    # profiles: a run reprovisions the VM and redeploys baseline, EMA, and SMA
    # together, so the three profiles in one run share that run's infrastructure
    # conditions. Differencing within the run therefore cancels run-to-run
    # environmental variation, and the spread of the resulting per-run
    # improvements is a meaningful uncertainty estimate for the improvement
    # itself (not just for each profile's absolute latency).
    #
    # The returned frame carries one improvement value per run, which the
    # aggregation below turns into a mean +/- SD. Baseline rows resolve to 0%.
    baseline = (
        run_stats.filter(pl.col("profile") == "baseline")
        .select(
            "run",
            "platform",
            "workflow",
            "metric",
            pl.col("mean_ms").alias("baseline_mean_ms"),
            pl.col("p90_ms").alias("baseline_p90_ms"),
        )
    )
    joined = run_stats.join(baseline, on=["run", "platform", "workflow", "metric"], how="left")
    return joined.with_columns(
        ((pl.col("baseline_mean_ms") - pl.col("mean_ms")) / pl.col("baseline_mean_ms") * 100).alias(
            "mean_impr_pct"
        ),
        ((pl.col("baseline_p90_ms") - pl.col("p90_ms")) / pl.col("baseline_p90_ms") * 100).alias(
            "p90_impr_pct"
        ),
    )


def aggregate_summary(run_stats: pl.DataFrame) -> pl.DataFrame:
    # Aggregate the per-run summaries, not the raw latency samples. This keeps
    # each repeated benchmark run weighted equally when reporting profile-level
    # statistics. Only the statistics reported in the thesis are kept:
    #
    #   * mean_ms / p90_ms: mean of the per-run mean / p90 latencies
    #   * mean_sd_ms: between-run SD of the mean latency (absolute uncertainty)
    #   * {mean,p90}_impr_pct: mean of the per-run improvements vs baseline
    #   * {mean,p90}_impr_sd_pct: between-run SD of those per-run improvements,
    #     i.e. the uncertainty on the improvement percentage itself
    #
    # All SDs use the sample estimator (ddof=1) and are null for a single run;
    # fill_null(0) renders that as a vanishing error bar / "+/-0.0".
    run_impr = run_improvements(run_stats)
    return (
        run_impr.group_by(["profile", "platform", "workflow", "metric"])
        .agg(
            pl.len().alias("runs"),
            pl.col("n").sum().alias("retained_samples"),
            pl.col("mean_ms").mean().alias("mean_ms"),
            pl.col("mean_ms").std().alias("mean_sd_ms"),
            pl.col("p90_ms").mean().alias("p90_ms"),
            pl.col("mean_impr_pct").mean().alias("mean_impr_pct"),
            pl.col("mean_impr_pct").std().alias("mean_impr_sd_pct"),
            pl.col("p90_impr_pct").mean().alias("p90_impr_pct"),
            pl.col("p90_impr_pct").std().alias("p90_impr_sd_pct"),
        )
        .with_columns(
            pl.col("mean_sd_ms").fill_null(0.0),
            pl.col("mean_impr_sd_pct").fill_null(0.0),
            pl.col("p90_impr_sd_pct").fill_null(0.0),
        )
        .sort(["platform", "workflow", "metric", "profile"])
    )


def results_table(summary: pl.DataFrame) -> pl.DataFrame:
    # Project the enriched summary into the column order used by the printed
    # table. Improvement statistics are already computed per-run-paired in
    # aggregate_summary, so this is a pure selection/sort.
    return summary.select(
        "platform",
        "workflow",
        "metric",
        "profile",
        "runs",
        "retained_samples",
        "mean_ms",
        "mean_sd_ms",
        "p90_ms",
        "mean_impr_pct",
        "mean_impr_sd_pct",
        "p90_impr_pct",
        "p90_impr_sd_pct",
    ).sort(["platform", "workflow", "metric", "profile"])
