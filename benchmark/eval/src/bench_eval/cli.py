import argparse
from pathlib import Path

from .aggregate import aggregate_summary, improvements, run_summary
from .constants import RUNS
from .ingest import load_function_samples, load_latency_samples


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(prog="bench-eval")

    parser.add_argument("--results", type=Path, default=Path("../results"))
    parser.add_argument("--runs", nargs="+", default=list(RUNS))
    parser.add_argument("--out", type=Path, default=Path("out"))
    parser.add_argument("--allow-incomplete", action="store_true")

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
    improvement = improvements(summary)
    write_tables(out, validation, run_stats, summary, improvement)

    from .plotting import write_all_figures

    function_samples = load_function_samples(
        results,
        runs,
        allow_incomplete=args.allow_incomplete,
    )
    write_all_figures(samples, run_stats, summary, function_samples, out)

    print(f"Wrote evaluation outputs to {out}")
    return 0


def write_tables(out: Path, validation, run_stats, summary, improvement) -> None:
    tables = out / "tables"
    tables.mkdir(parents=True, exist_ok=True)
    validation.write_csv(tables / "validation.csv")
    run_stats.write_csv(tables / "latency_run_summary.csv")
    summary.write_csv(tables / "latency_summary.csv")
    improvement.write_csv(tables / "latency_improvements.csv")


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
