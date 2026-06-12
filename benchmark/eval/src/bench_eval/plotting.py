from __future__ import annotations

import os
from pathlib import Path

os.environ.setdefault("MPLCONFIGDIR", str(Path.cwd() / ".matplotlib-cache"))
os.environ.setdefault("XDG_CACHE_HOME", str(Path.cwd() / ".cache"))

import matplotlib.pyplot as plt
import numpy as np
import polars as pl

from .aggregate import retained_latency
from .constants import PROFILE_LABELS, PROFILES, SUMMARY_METRICS, WEBSHOP_OPERATIONS


def write_all_figures(
    samples: pl.DataFrame,
    run_stats: pl.DataFrame,
    summary: pl.DataFrame,
    function_samples: pl.DataFrame,
    out_dir: Path,
) -> None:
    figures = out_dir / "figures"
    figures.mkdir(parents=True, exist_ok=True)

    for platform, workflow in _benchmark_config_pairs(samples):
        _plot_distribution(samples, run_stats, platform, workflow, figures)
        _plot_summary(run_stats, platform, workflow, figures)
        _plot_iterations(samples, platform, workflow, figures)
        if not function_samples.is_empty():
            _plot_functions(function_samples, platform, workflow, figures)


def _plot_distribution(
    samples: pl.DataFrame,
    run_stats: pl.DataFrame,
    platform: str,
    workflow: str,
    out_dir: Path,
) -> None:
    df = retained_latency(samples).filter(
        (pl.col("platform") == platform)
        & (pl.col("workflow") == workflow)
        & (pl.col("metric") == _primary_metric(workflow))
    )
    if df.is_empty():
        return

    colors = _profile_colors()
    fig, ax = plt.subplots(figsize=(8, 4.8), constrained_layout=True)
    positions = np.arange(1, len(PROFILES) + 1)
    values = [
        df.filter(pl.col("profile") == profile)["latency_ms"].to_numpy()
        for profile in PROFILES
    ]
    violin = ax.violinplot(values, positions=positions, widths=0.7, showmeans=False, showextrema=False)
    for body, profile in zip(violin["bodies"], PROFILES, strict=True):
        body.set_facecolor(colors[profile])
        body.set_edgecolor("black")
        body.set_alpha(0.28)

    rng = np.random.default_rng(42)
    for idx, profile in enumerate(PROFILES, start=1):
        profile_df = df.filter(pl.col("profile") == profile)
        y = profile_df["latency_ms"].to_numpy()
        x = rng.normal(idx, 0.045, size=len(y))
        ax.scatter(x, y, s=10, alpha=0.32, color=colors[profile], linewidths=0)

        means = run_stats.filter(
            (pl.col("platform") == platform)
            & (pl.col("workflow") == workflow)
            & (pl.col("metric") == _primary_metric(workflow))
            & (pl.col("profile") == profile)
        )["mean_ms"].to_numpy()
        ax.scatter(np.full(len(means), idx), means, marker="D", s=36, color="black", zorder=4)

    ax.set_title(f"{platform} / {workflow}: retained latency distribution")
    ax.set_ylabel("Latency (ms)")
    ax.set_xticks(positions, [PROFILE_LABELS[p] for p in PROFILES])
    ax.grid(axis="y", alpha=0.25)
    _save_png(fig, out_dir / f"distribution_{platform}_{workflow}.png")


def _plot_summary(run_stats: pl.DataFrame, platform: str, workflow: str, out_dir: Path) -> None:
    df = run_stats.filter(
        (pl.col("platform") == platform)
        & (pl.col("workflow") == workflow)
        & (pl.col("metric") == _primary_metric(workflow))
    )
    if df.is_empty():
        return

    colors = _profile_colors()
    fig, ax = plt.subplots(figsize=(8, 4.8), constrained_layout=True)
    width = 0.24
    x = np.arange(len(SUMMARY_METRICS))
    for offset, profile in zip((-width, 0, width), PROFILES, strict=True):
        profile_df = df.filter(pl.col("profile") == profile)
        if profile_df.is_empty():
            continue
        heights = [float(profile_df[f"{metric}_ms"].mean()) for metric in SUMMARY_METRICS]
        bars = ax.bar(
            x + offset,
            heights,
            width=width,
            capsize=4,
            label=PROFILE_LABELS[profile],
            color=colors[profile],
            alpha=0.82,
        )
        if profile != "baseline":
            _annotate_improvements(ax, bars, heights, _baseline_heights(df))

    ax.set_title(f"{platform} / {workflow}: run-level summary")
    ax.set_ylabel("Latency (ms)")
    ax.set_xticks(x, ["Mean", "Median", "p90", "p95"])
    ax.grid(axis="y", alpha=0.25)
    ax.legend(frameon=False)
    ax.margins(y=0.16)
    _save_png(fig, out_dir / f"summary_{platform}_{workflow}.png")


def _plot_iterations(samples: pl.DataFrame, platform: str, workflow: str, out_dir: Path) -> None:
    df = retained_latency(samples).filter(
        (pl.col("platform") == platform)
        & (pl.col("workflow") == workflow)
        & (pl.col("metric") == _primary_metric(workflow))
    )
    if df.is_empty():
        return

    colors = _profile_colors()
    fig, ax = plt.subplots(figsize=(9, 4.8), constrained_layout=True)
    for profile in PROFILES:
        profile_df = df.filter(pl.col("profile") == profile)
        for run in sorted(profile_df["run"].unique().to_list()):
            run_df = profile_df.filter(pl.col("run") == run).sort("iteration_index")
            ax.plot(
                run_df["iteration_index"].to_numpy(),
                run_df["latency_ms"].to_numpy(),
                alpha=0.5,
                linewidth=1.2,
                color=colors[profile],
            )
        mean_df = (
            profile_df.group_by("iteration_index")
            .agg(pl.col("latency_ms").mean().alias("latency_ms"))
            .sort("iteration_index")
        )
        ax.plot(
            mean_df["iteration_index"].to_numpy(),
            mean_df["latency_ms"].to_numpy(),
            linewidth=2.2,
            color=colors[profile],
            label=PROFILE_LABELS[profile],
        )

    ax.set_title(f"{platform} / {workflow}: retained iteration trend")
    ax.set_xlabel("Retained iteration index")
    ax.set_ylabel("Latency (ms)")
    ax.grid(axis="y", alpha=0.25)
    ax.legend(frameon=False)
    _save_png(fig, out_dir / f"iterations_{platform}_{workflow}.png")


def _plot_functions(function_samples: pl.DataFrame, platform: str, workflow: str, out_dir: Path) -> None:
    df = function_samples.filter((pl.col("platform") == platform) & (pl.col("workflow") == workflow))
    if df.is_empty():
        return

    if workflow in WEBSHOP_OPERATIONS:
        _plot_webshop_functions(df, platform, workflow, out_dir)
        return

    order = _function_order(df)
    if not order:
        return

    fig_width = max(9, len(order) * 0.55)
    fig, ax = plt.subplots(figsize=(fig_width, 5.2), constrained_layout=True)
    _plot_function_axis(
        ax,
        df,
        order,
        f"{platform} / {workflow}: function completion relative to entry start",
    )
    _save_png(fig, out_dir / f"functions_{platform}_{workflow}.png")


def _plot_webshop_functions(df: pl.DataFrame, platform: str, workflow: str, out_dir: Path) -> None:
    operations = WEBSHOP_OPERATIONS[workflow]
    orders = {
        operation: _function_order(df.filter(pl.col("operation") == operation))
        for operation in operations
    }
    max_functions = max((len(order) for order in orders.values()), default=0)
    if max_functions == 0:
        return

    fig_width = max(9, max_functions * 0.55)
    fig_height = max(4.2, len(operations) * 3.2)
    fig, axes = plt.subplots(
        len(operations),
        1,
        figsize=(fig_width, fig_height),
        constrained_layout=True,
        squeeze=False,
    )
    for ax, operation in zip(axes[:, 0], operations, strict=True):
        operation_df = df.filter(pl.col("operation") == operation)
        order = orders[operation]
        if not order:
            ax.set_visible(False)
            continue
        _plot_function_axis(
            ax,
            operation_df,
            order,
            f"{platform} / {workflow}: {operation}",
        )

    _save_png(fig, out_dir / f"functions_{platform}_{workflow}.png")


def _function_order(df: pl.DataFrame) -> list[str]:
    if df.is_empty():
        return []
    order = (
        df.group_by("function")
        .agg(pl.col("relative_finished_ms").median().alias("median_ms"))
        .sort("median_ms")["function"]
        .to_list()
    )
    return order


def _plot_function_axis(ax: plt.Axes, df: pl.DataFrame, order: list[str], title: str) -> None:
    xpos = {name: idx for idx, name in enumerate(order)}

    colors = _profile_colors()
    rng = np.random.default_rng(7)

    for profile in PROFILES:
        profile_df = df.filter(pl.col("profile") == profile)
        if profile_df.is_empty():
            continue
        x = np.array([xpos[name] for name in profile_df["function"].to_list()], dtype=float)
        x += rng.normal(0, 0.055, size=len(x))
        y = profile_df["relative_finished_ms"].to_numpy()
        ax.scatter(x, y, s=9, alpha=0.25, color=colors[profile], linewidths=0)

        mean = (
            profile_df.group_by("function")
            .agg(pl.col("relative_finished_ms").mean().alias("mean_ms"))
        )
        ax.scatter(
            [xpos[name] for name in mean["function"].to_list()],
            mean["mean_ms"].to_numpy(),
            marker="D",
            s=28,
            color=colors[profile],
            edgecolor="black",
            linewidth=0.4,
            label=PROFILE_LABELS[profile],
            zorder=4,
        )

    ax.set_title(title)
    ax.set_ylabel("Relative finish time (ms)")
    ax.set_xticks(range(len(order)), order, rotation=45, ha="right")
    ax.grid(axis="y", alpha=0.25)
    ax.legend(frameon=False)


def _benchmark_config_pairs(samples: pl.DataFrame) -> list[tuple[str, str]]:
    return (
        samples.select("platform", "workflow")
        .unique()
        .sort(["platform", "workflow"])
        .iter_rows()
    )


def _baseline_heights(df: pl.DataFrame) -> list[float]:
    baseline_df = df.filter(pl.col("profile") == "baseline")
    if baseline_df.is_empty():
        return [0.0 for _ in SUMMARY_METRICS]
    return [float(baseline_df[f"{metric}_ms"].mean()) for metric in SUMMARY_METRICS]


def _annotate_improvements(
    ax: plt.Axes,
    bars,
    heights: list[float],
    baseline_heights: list[float],
) -> None:
    for bar, height, baseline in zip(bars, heights, baseline_heights, strict=True):
        if baseline <= 0:
            continue
        improvement = (baseline - height) / baseline * 100
        label = f"{improvement:.1f}%"
        ax.annotate(
            label,
            xy=(bar.get_x() + bar.get_width() / 2, bar.get_height()),
            xytext=(0, 4),
            textcoords="offset points",
            ha="center",
            va="bottom",
            fontsize=8,
        )


def _std_or_zero(values: np.ndarray) -> float:
    if len(values) < 2:
        return 0.0
    return float(np.std(values, ddof=1))


def _profile_colors() -> dict[str, str]:
    return {
        "baseline": "C0",
        "optimized-ema": "C2",
        "optimized-sma": "C1",
    }


def _primary_metric(workflow: str) -> str:
    if workflow.startswith("webshop-"):
        return "journey_latency_ms"
    return "workflow_latency_ms"


def _save_png(fig: plt.Figure, path: Path) -> None:
    fig.savefig(path, dpi=180)
    plt.close(fig)
