import polars as pl


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
    # First summarize each repeated run independently. The thesis analysis
    # treats runs as the experimental repeats, so this layer must not pool
    # samples across run boundaries.
    return (
        retained.group_by(["run", "profile", "platform", "workflow", "metric"])
        .agg(
            pl.len().alias("n"),
            pl.col("latency_ms").mean().alias("mean_ms"),
            pl.col("latency_ms").median().alias("median_ms"),
            pl.col("latency_ms").quantile(0.90, interpolation="linear").alias("p90_ms"),
            pl.col("latency_ms").quantile(0.95, interpolation="linear").alias("p95_ms"),
            pl.col("latency_ms").min().alias("min_ms"),
            pl.col("latency_ms").max().alias("max_ms"),
            pl.col("latency_ms").std().alias("std_ms"),
        )
        .sort(["platform", "workflow", "metric", "profile", "run"])
    )


def aggregate_summary(run_stats: pl.DataFrame) -> pl.DataFrame:
    # Aggregate the per-run summaries, not the raw latency samples. This keeps
    # each repeated benchmark run weighted equally when reporting profile-level
    # means and tail statistics.
    return (
        run_stats.group_by(["profile", "platform", "workflow", "metric"])
        .agg(
            pl.len().alias("runs"),
            pl.col("n").sum().alias("retained_samples"),
            pl.col("mean_ms").mean().alias("mean_ms"),
            pl.col("mean_ms").std().alias("mean_run_sd_ms"),
            pl.col("mean_ms").min().alias("mean_run_min_ms"),
            pl.col("mean_ms").max().alias("mean_run_max_ms"),
            pl.col("median_ms").mean().alias("median_ms"),
            pl.col("p90_ms").mean().alias("p90_ms"),
            pl.col("p95_ms").mean().alias("p95_ms"),
            pl.col("min_ms").min().alias("min_ms"),
            pl.col("max_ms").max().alias("max_ms"),
            pl.col("std_ms").mean().alias("within_run_std_ms"),
        )
        .sort(["platform", "workflow", "metric", "profile"])
    )


def improvements(summary: pl.DataFrame) -> pl.DataFrame:
    # Join each optimized profile against the matching baseline for the same
    # platform/workflow/metric, then express lower latency as positive
    # improvement percentages.
    baseline = (
        summary.filter(pl.col("profile") == "baseline")
        .select(
            "platform",
            "workflow",
            "metric",
            pl.col("mean_ms").alias("baseline_mean_ms"),
            pl.col("median_ms").alias("baseline_median_ms"),
            pl.col("p90_ms").alias("baseline_p90_ms"),
            pl.col("p95_ms").alias("baseline_p95_ms"),
        )
    )
    joined = summary.join(baseline, on=["platform", "workflow", "metric"], how="left")
    return (
        joined.with_columns(
            ((pl.col("baseline_mean_ms") - pl.col("mean_ms")) / pl.col("baseline_mean_ms") * 100).alias(
                "mean_improvement_pct"
            ),
            (
                (pl.col("baseline_median_ms") - pl.col("median_ms"))
                / pl.col("baseline_median_ms")
                * 100
            ).alias("median_improvement_pct"),
            ((pl.col("baseline_p90_ms") - pl.col("p90_ms")) / pl.col("baseline_p90_ms") * 100).alias(
                "p90_improvement_pct"
            ),
            ((pl.col("baseline_p95_ms") - pl.col("p95_ms")) / pl.col("baseline_p95_ms") * 100).alias(
                "p95_improvement_pct"
            ),
        )
        .select(
            "profile",
            "platform",
            "workflow",
            "metric",
            "mean_ms",
            "baseline_mean_ms",
            "mean_improvement_pct",
            "median_improvement_pct",
            "p90_improvement_pct",
            "p95_improvement_pct",
        )
        .sort(["platform", "workflow", "metric", "profile"])
    )
