from __future__ import annotations

RUNS = ()
PROFILES = ("baseline", "optimized-ema", "optimized-sma")
PLATFORMS = ("tinyfaas", "faasd")
WORKFLOWS = (
    "iot",
    "tree",
    "webshop-browse-addcart-checkout",
    "webshop-addcart-checkout",
)

TRIM_HEAD = 10
TRIM_TAIL = 10
EXPECTED_ITERATIONS = 70

PROFILE_LABELS = {
    "baseline": "Baseline",
    "optimized-ema": "Optimized EMA",
    "optimized-sma": "Optimized SMA",
}

PROFILE_COLORS = {
    "baseline": "#4c566a",
    "optimized-ema": "#2a9d8f",
    "optimized-sma": "#d08770",
}

WORKFLOW_METRICS = {
    "iot": ("workflow_latency_ms",),
    "tree": ("workflow_latency_ms",),
    "webshop-browse-addcart-checkout": (
        "browse_latency_ms",
        "addcart_latency_ms",
        "checkout_latency_ms",
    ),
    "webshop-addcart-checkout": ("addcart_latency_ms", "checkout_latency_ms"),
}

ENTRY_FUNCTIONS = {
    "iot": "iot-i",
    "tree": "tree-a",
    "webshop-browse-addcart-checkout": "webshop-frontend",
    "webshop-addcart-checkout": "webshop-frontend",
}

SUMMARY_METRICS = ("mean", "median", "p90", "p95")
