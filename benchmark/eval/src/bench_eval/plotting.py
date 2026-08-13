import os
from pathlib import Path

os.environ.setdefault("MPLCONFIGDIR", str(Path.cwd() / ".matplotlib-cache"))
os.environ.setdefault("XDG_CACHE_HOME", str(Path.cwd() / ".cache"))

import matplotlib.pyplot as plt
import numpy as np
import polars as pl

from .aggregate import retained_latency, run_improvements
from .constants import (
    PROFILE_COLORS,
    PROFILE_LABELS,
    PROFILES,
    SUMMARY_METRICS,
    WEBSHOP_OPERATIONS,
)

# Vector output so figures stay crisp at any scale when embedded in the thesis.
# Defaults to "svg"; pass fig_format="pdf" to write_all_figures for PDF output.
FIGURE_FORMAT = "svg"

SUMMARY_METRIC_LABELS = {"mean": "Mean", "median": "Median", "p90": "p90", "p95": "p95"}


def _apply_style() -> None:
    # Shared style so every figure is legible after being scaled to text width
    # in the thesis and uses one consistent look.
    plt.rcParams.update(
        {
            "font.size": 12,
            "axes.titlesize": 13,
            "axes.labelsize": 12,
            "legend.fontsize": 11,
            "xtick.labelsize": 11,
            "ytick.labelsize": 11,
            "savefig.bbox": "tight",
        }
    )


def write_all_figures(
    samples: pl.DataFrame,
    run_stats: pl.DataFrame,
    summary: pl.DataFrame,
    function_samples: pl.DataFrame,
    resource_minutes: pl.DataFrame,
    out_dir: Path,
    fig_format: str = FIGURE_FORMAT,
) -> None:
    _apply_style()
    figures = out_dir / "figures"
    figures.mkdir(parents=True, exist_ok=True)

    # Generate the same figure set for every platform/workflow combination
    # present in the latency samples. Function and resource plots are optional
    # because older result directories may not contain stats/functions output or
    # backfilled host metrics.
    for platform, workflow in _benchmark_config_pairs(samples):
        _plot_distribution(samples, run_stats, platform, workflow, figures, fig_format)
        _plot_summary(run_stats, platform, workflow, figures, fig_format)
        _plot_iterations(samples, platform, workflow, figures, fig_format)
        if not function_samples.is_empty():
            _plot_functions(function_samples, platform, workflow, figures, fig_format)
        if not resource_minutes.is_empty():
            _plot_resources(resource_minutes, platform, workflow, figures, fig_format)

    if not function_samples.is_empty():
        platforms = function_samples["platform"].unique().sort().to_list()
        for platform in platforms:
            _plot_function_platform_overview(
                function_samples, platform, figures, fig_format
            )


def _plot_distribution(
    samples: pl.DataFrame,
    run_stats: pl.DataFrame,
    platform: str,
    workflow: str,
    out_dir: Path,
    fig_format: str = FIGURE_FORMAT,
) -> None:
    # Distribution plots use pooled retained samples for shape, while the black
    # diamonds show independent run means so repeated runs remain visible.
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
    # Matplotlib expects one value array per violin. Keep profile order stable
    # even if a profile has no samples, so colors and labels remain consistent.
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

    # Proxy handle so the reader knows the black diamonds are per-run means.
    ax.scatter([], [], marker="D", s=36, color="black", label="Per-run mean")
    ax.set_title(f"{platform} / {workflow}: latency distribution")
    ax.set_ylabel("Latency (ms)")
    ax.set_xticks(positions, [PROFILE_LABELS[p] for p in PROFILES])
    ax.grid(axis="y", alpha=0.25)
    ax.legend(frameon=False)
    _save_figure(fig, out_dir / f"distribution_{platform}_{workflow}.{fig_format}")


def _plot_summary(
    run_stats: pl.DataFrame,
    platform: str,
    workflow: str,
    out_dir: Path,
    fig_format: str = FIGURE_FORMAT,
) -> None:
    # Summary bars compare profile-level run summaries for the primary metric.
    # Webshop uses computed journey latency; IoT/tree use workflow latency.
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
        # Average the per-run statistics for each profile. This mirrors
        # aggregate_summary and avoids weighting runs by raw sample count.
        heights = [float(profile_df[f"{metric}_ms"].mean()) for metric in SUMMARY_METRICS]
        # Error bars are the between-run standard deviation of each statistic, so
        # the figure makes run-to-run variability visible rather than hiding it.
        errors = [_between_run_sd(profile_df, f"{metric}_ms") for metric in SUMMARY_METRICS]
        bars = ax.bar(
            x + offset,
            heights,
            width=width,
            yerr=errors,
            capsize=4,
            label=PROFILE_LABELS[profile],
            color=colors[profile],
            alpha=0.82,
        )
        if profile != "baseline":
            _annotate_improvements(ax, bars, _improvement_stats(df, profile))

    ax.set_title(f"{platform} / {workflow}: latency summary")
    ax.set_ylabel("Latency (ms)")
    ax.set_xticks(x, [SUMMARY_METRIC_LABELS[m] for m in SUMMARY_METRICS])
    ax.grid(axis="y", alpha=0.25)
    ax.legend(frameon=False)
    ax.margins(y=0.16)
    _save_figure(fig, out_dir / f"summary_{platform}_{workflow}.{fig_format}")


def _plot_iterations(
    samples: pl.DataFrame,
    platform: str,
    workflow: str,
    out_dir: Path,
    fig_format: str = FIGURE_FORMAT,
) -> None:
    # Thin lines show each retained run trajectory; the bold line is the mean
    # latency at each retained iteration index across runs for the profile.
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

    ax.set_title(f"{platform} / {workflow}: latencies across iterations")
    ax.set_xlabel("iteration index")
    ax.set_ylabel("Latency (ms)")
    ax.grid(axis="y", alpha=0.25)
    ax.legend(frameon=False)
    _save_figure(fig, out_dir / f"iterations_{platform}_{workflow}.{fig_format}")


def _plot_resources(
    resources: pl.DataFrame,
    platform: str,
    workflow: str,
    out_dir: Path,
    fig_format: str = FIGURE_FORMAT,
) -> None:
    # Each point is the equal-weight mean of the repeated runs' one-minute
    # averages. The ribbon is the between-run sample SD, so temporal detail stays
    # readable without hiding experimental variability.
    df = resources.filter((pl.col("platform") == platform) & (pl.col("workflow") == workflow))
    if df.is_empty():
        return

    colors = _profile_colors()
    panels = (
        ("cpu_mean_pct", "cpu_sd_pct", "CPU utilization (%)"),
        ("mem_mean_pct", "mem_sd_pct", "Memory used (%)"),
    )
    fig, axes = plt.subplots(2, 1, figsize=(9, 7.2), sharex=True, constrained_layout=True)

    for ax, (mean_column, sd_column, ylabel) in zip(axes, panels, strict=True):
        panel = df.filter(pl.col(mean_column).is_not_null())
        for profile in PROFILES:
            profile_df = panel.filter(pl.col("profile") == profile).sort("minute")
            if profile_df.is_empty():
                continue
            x = profile_df["minute_midpoint"].to_numpy()
            mean = profile_df[mean_column].to_numpy()
            sd = profile_df[sd_column].to_numpy()
            ax.plot(
                x,
                mean,
                linewidth=2.2,
                color=colors[profile],
                label=PROFILE_LABELS[profile],
            )
            ax.fill_between(
                x,
                np.maximum(mean - sd, 0),
                mean + sd,
                color=colors[profile],
                alpha=0.16,
                linewidth=0,
            )
        ax.set_ylabel(ylabel)
        ax.set_ylim(bottom=0)
        ax.grid(axis="y", alpha=0.25)

    axes[0].set_title(f"{platform} / {workflow}: aggregated mean VM resource usage (±SD)")
    axes[-1].set_xlabel("Elapsed time since k6 start (min)")
    axes[0].legend(frameon=False, ncol=len(PROFILES))
    _save_figure(fig, out_dir / f"resources_{platform}_{workflow}.{fig_format}")


def _plot_functions(
    function_samples: pl.DataFrame,
    platform: str,
    workflow: str,
    out_dir: Path,
    fig_format: str = FIGURE_FORMAT,
) -> None:
    # Function samples are already filtered during ingestion by graph
    # reachability and entry-window timing. Plotting only decides whether the
    # workflow needs one axis or webshop operation subplots.
    df = function_samples.filter((pl.col("platform") == platform) & (pl.col("workflow") == workflow))
    if df.is_empty():
        return

    if workflow in WEBSHOP_OPERATIONS:
        _plot_webshop_functions(df, platform, workflow, out_dir, fig_format)
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
    _save_figure(fig, out_dir / f"functions_{platform}_{workflow}.{fig_format}")


def _plot_webshop_functions(
    df: pl.DataFrame,
    platform: str,
    workflow: str,
    out_dir: Path,
    fig_format: str = FIGURE_FORMAT,
) -> None:
    operations = WEBSHOP_OPERATIONS[workflow]
    # Each operation can call a different subset of webshop functions, so order
    # x-axis labels independently per subplot by observed median finish time.
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

    _save_figure(fig, out_dir / f"functions_{platform}_{workflow}.{fig_format}")


def _plot_function_platform_overview(
    function_samples: pl.DataFrame,
    platform: str,
    out_dir: Path,
    fig_format: str = FIGURE_FORMAT,
) -> None:
    """Plot every function-completion panel for one platform in one figure."""
    df = function_samples.filter(pl.col("platform") == platform)
    if df.is_empty():
        return

    # The final checkout panel spans both columns because it has the widest set
    # of function labels. The other six panels form three comparison rows.
    panel_specs = (
        ("iot", None, "IoT", (0, 0)),
        ("tree", None, "Tree", (0, 1)),
        (
            "webshop-addcart-checkout",
            "addcart",
            "Addcart-checkout: addcart",
            (1, 0),
        ),
        (
            "webshop-addcart-checkout",
            "checkout",
            "Addcart-checkout: checkout",
            (1, 1),
        ),
        (
            "webshop-browse-addcart-checkout",
            "browse",
            "Browse-addcart-checkout: browse",
            (2, 0),
        ),
        (
            "webshop-browse-addcart-checkout",
            "addcart",
            "Browse-addcart-checkout: addcart",
            (2, 1),
        ),
        (
            "webshop-browse-addcart-checkout",
            "checkout",
            "Browse-addcart-checkout: checkout",
            (3, -1),
        ),
    )

    fig = plt.figure(figsize=(10, 12.8), constrained_layout=True)
    grid = fig.add_gridspec(4, 2)
    visible_axes: list[plt.Axes] = []

    for workflow, operation, title, (row, column) in panel_specs:
        slot = grid[row, :] if column == -1 else grid[row, column]
        ax = fig.add_subplot(slot)
        panel = df.filter(pl.col("workflow") == workflow)
        if operation is None:
            panel = panel.filter(pl.col("operation").is_null())
        else:
            panel = panel.filter(pl.col("operation") == operation)

        order = _function_order(panel)
        if not order:
            ax.set_visible(False)
            continue

        _plot_function_axis(
            ax,
            panel,
            order,
            title,
            show_ylabel=False,
            show_legend=False,
        )
        ax.title.set_fontsize(11)
        ax.tick_params(axis="both", labelsize=9)
        visible_axes.append(ax)

    if not visible_axes:
        plt.close(fig)
        return

    handles, labels = visible_axes[0].get_legend_handles_labels()
    fig.legend(handles, labels, loc="outside upper center", ncol=len(PROFILES), frameon=False)
    fig.supylabel("Relative finish time (ms)")
    _save_figure(fig, out_dir / f"functions_{platform}_overview.{fig_format}")


def _function_order(df: pl.DataFrame) -> list[str]:
    if df.is_empty():
        return []
    # Median finish time gives a stable left-to-right execution order that is
    # less sensitive to outliers than ordering by mean.
    order = (
        df.group_by("function")
        .agg(pl.col("relative_finished_ms").median().alias("median_ms"))
        .sort("median_ms")["function"]
        .to_list()
    )
    return order


def _function_label(name: str) -> str:
    return name.split("-", maxsplit=1)[-1]


def _plot_function_axis(
    ax: plt.Axes,
    df: pl.DataFrame,
    order: list[str],
    title: str,
    *,
    show_ylabel: bool = True,
    show_legend: bool = True,
) -> None:
    # Map function names to categorical x positions once; individual samples
    # receive small deterministic jitter so overlapping invocations are visible.
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
        # Overlay profile/function means as diamonds to make the central trend
        # readable on top of the raw invocation scatter.
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
    if show_ylabel:
        ax.set_ylabel("Relative finish time (ms)")
    ax.set_xticks(
        range(len(order)),
        [_function_label(name) for name in order],
        rotation=45,
        ha="right",
    )
    ax.grid(axis="y", alpha=0.25)
    if show_legend:
        ax.legend(frameon=False)


def _benchmark_config_pairs(samples: pl.DataFrame) -> list[tuple[str, str]]:
    return (
        samples.select("platform", "workflow")
        .unique()
        .sort(["platform", "workflow"])
        .iter_rows()
    )


def _between_run_sd(profile_df: pl.DataFrame, column: str) -> float:
    # Sample SD (ddof=1) across the per-run values. Returns 0 for a single run,
    # where the SD is undefined, so the error bar simply vanishes.
    if profile_df.height < 2:
        return 0.0
    return float(profile_df[column].std())


def _improvement_stats(df: pl.DataFrame, profile: str) -> list[tuple[float, float] | None]:
    # Per-run paired improvement (see aggregate.run_improvements) summarized as
    # (mean, SD) for each reported metric. Computed from the same function the
    # printed table uses, so the figure annotations and the table agree exactly.
    # An entry is None when no paired baseline run exists (e.g. a pilot that only
    # ran an optimized profile), in which case the bar gets no annotation.
    impr = run_improvements(df).filter(pl.col("profile") == profile)
    stats: list[tuple[float, float] | None] = []
    for metric in SUMMARY_METRICS:
        column = f"{metric}_impr_pct"
        mean = impr[column].mean()
        if mean is None:
            stats.append(None)
            continue
        sd = float(impr[column].std()) if impr.height > 1 else 0.0
        stats.append((float(mean), sd))
    return stats


def _annotate_improvements(
    ax: plt.Axes,
    bars,
    stats: list[tuple[float, float] | None],
) -> None:
    # Label each optimized bar with its improvement over baseline as
    # "<mean>%" over "(+/-<SD>)", where the SD is the between-run spread of the
    # per-run improvements (the uncertainty on the improvement itself).
    for bar, stat in zip(bars, stats, strict=True):
        if stat is None:
            continue
        mean, sd = stat
        label = f"{mean:.1f}%\n(±{sd:.1f})"
        ax.annotate(
            label,
            xy=(bar.get_x() + bar.get_width() / 2, bar.get_height()),
            xytext=(0, 4),
            textcoords="offset points",
            ha="center",
            va="bottom",
            fontsize=8,
        )


def _profile_colors() -> dict[str, str]:
    return dict(PROFILE_COLORS)


def _primary_metric(workflow: str) -> str:
    if workflow.startswith("webshop-"):
        return "journey_latency_ms"
    return "workflow_latency_ms"


def _save_figure(fig: plt.Figure, path: Path) -> None:
    fig.savefig(path)
    plt.close(fig)
