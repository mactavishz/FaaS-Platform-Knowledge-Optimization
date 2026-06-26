import argparse
import os
from pathlib import Path

import polars as pl

from .aggregate import aggregate_summary, results_table, run_summary
from .constants import RUNS
from .ingest import load_function_samples, load_latency_samples, load_resource_samples


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(prog="bench-eval")

    parser.add_argument("--results", type=Path, default=Path("../results"))
    parser.add_argument("--runs", nargs="+", default=list(RUNS))
    parser.add_argument("--out", type=Path, default=Path("out"))
    parser.add_argument("--allow-incomplete", action="store_true")
    parser.add_argument(
        "--fetch-resources",
        action="store_true",
        help="Backfill VM host metrics from Cloud Monitoring before plotting "
        "(requires gcloud auth and a project).",
    )
    parser.add_argument(
        "--refresh-resources",
        action="store_true",
        help="Re-fetch host metrics even if a cached resources/vm-usage.csv exists.",
    )
    parser.add_argument(
        "--project",
        default=os.environ.get("GOOGLE_PROJECT"),
        help="Google Cloud project holding the benchmark VM metrics "
        "(defaults to $GOOGLE_PROJECT).",
    )

    args = parser.parse_args(argv)
    results = resolve_input_path(args.results)
    out = args.out.resolve()
    runs = tuple(args.runs)

    if not runs:
        print("No runs specified. Exiting without processing.")
        return 0

    samples, validation = load_latency_samples(
        results,
        runs,
        allow_incomplete=args.allow_incomplete,
    )
    run_stats = run_summary(samples)
    summary = aggregate_summary(run_stats)
    table = results_table(summary)

    print_validation(validation)
    print_results_table(table)

    from .plotting import write_all_figures

    function_samples = load_function_samples(
        results,
        runs,
        allow_incomplete=args.allow_incomplete,
    )

    if args.fetch_resources or args.refresh_resources:
        from .monitoring import fetch_resource_samples

        print("\nFetching VM host metrics from Cloud Monitoring:")
        fetch_resource_samples(
            results, runs, project=args.project, refresh=args.refresh_resources
        )

    resource_samples = load_resource_samples(results, runs)
    write_all_figures(samples, run_stats, summary, function_samples, resource_samples, out)

    print(f"\nWrote figures to {out / 'figures'}")
    return 0


def print_validation(validation: pl.DataFrame) -> None:
    if validation.is_empty():
        print("Validation: passed (no issues)\n")
        return
    errors = validation.filter(pl.col("severity") == "error").height
    warnings = validation.height - errors
    print(f"Validation: {errors} error(s), {warnings} warning(s)")
    with _table_config():
        print(validation)
    print()


def print_results_table(table: pl.DataFrame) -> None:
    # Round to a single decimal and rename to compact headers for the terminal.
    # The aggregation already weights each run equally, so these are the numbers
    # to quote directly in the thesis: mean +/- between-run SD, p90, and the
    # percentage improvement of each profile relative to its baseline.
    #
    # Each improvement is shown as "<mean> +/-<sd>", where the SD is the spread
    # of the per-run (within-run paired) improvements -- i.e. the uncertainty on
    # the improvement percentage itself, not on the absolute latency.
    display = (
        table.with_columns(
            pl.col("mean_ms").round(1),
            pl.col("mean_sd_ms").round(1),
            pl.col("p90_ms").round(1),
            _impr_label("mean_impr_pct", "mean_impr_sd_pct").alias("mean_impr"),
            _impr_label("p90_impr_pct", "p90_impr_sd_pct").alias("p90_impr"),
        )
        .select(
            "platform",
            "workflow",
            "metric",
            "profile",
            "runs",
            "retained_samples",
            "mean_ms",
            "mean_sd_ms",
            "p90_ms",
            "mean_impr",
            "p90_impr",
        )
        .rename(
            {
                "retained_samples": "n",
                "mean_ms": "mean (ms)",
                "mean_sd_ms": "+/-sd (ms)",
                "p90_ms": "p90 (ms)",
                "mean_impr": "mean impr %",
                "p90_impr": "p90 impr %",
            }
        )
    )
    print("Latency summary (per-run statistics aggregated across runs):")
    with _table_config():
        print(display)


def _impr_label(mean_col: str, sd_col: str) -> pl.Expr:
    # Render an improvement as "44.4 +/-2.3" so the point estimate and its
    # between-run uncertainty sit in a single column.
    return (
        pl.col(mean_col).round(1).cast(pl.Utf8)
        + " +/-"
        + pl.col(sd_col).round(1).cast(pl.Utf8)
    )


def _table_config() -> pl.Config:
    return pl.Config(
        tbl_formatting="ASCII_FULL_CONDENSED",
        tbl_hide_dataframe_shape=True,
        tbl_hide_column_data_types=True,
        tbl_rows=-1,
        tbl_cols=-1,
        tbl_width_chars=200,
        fmt_str_lengths=50,
    )


def resolve_input_path(path: Path) -> Path:
    if path.exists():
        return path.resolve()
    if path.is_absolute():
        return path
    for parent in (Path.cwd(), *Path.cwd().parents):
        candidate = parent / path
        if candidate.exists():
            return candidate.resolve()
    return path.resolve()


if __name__ == "__main__":
    raise SystemExit(main())
