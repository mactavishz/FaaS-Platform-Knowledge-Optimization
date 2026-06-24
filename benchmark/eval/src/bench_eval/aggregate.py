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
