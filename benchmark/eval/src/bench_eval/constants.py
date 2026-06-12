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

WEBSHOP_OPERATIONS = {
    "webshop-browse-addcart-checkout": ("browse", "addcart", "checkout"),
    "webshop-addcart-checkout": ("addcart", "checkout"),
}

WEBSHOP_FRONTEND_OPERATIONS = {
    "browse": "get",
    "addcart": "addcart",
    "checkout": "checkout",
}

WORKFLOW_CALL_GRAPHS = {
    "iot": {
        "iot-i": ("iot-cw", "iot-se", "iot-ct", "iot-cs", "iot-ca"),
        "iot-ct": ("iot-as",),
        "iot-cs": ("iot-csl", "iot-csa"),
        "iot-ca": ("iot-dj", "iot-as"),
    },
    "tree": {
        "tree-a": ("tree-b", "tree-c"),
        "tree-b": ("tree-d", "tree-e"),
        "tree-c": ("tree-f", "tree-g"),
    },
}

WEBSHOP_OPERATION_CALL_GRAPHS = {
    "browse": {
        "webshop-frontend": (
            "webshop-supportedcurrencies",
            "webshop-listproducts",
            "webshop-currency",
            "webshop-getads",
            "webshop-getcart",
            "webshop-listrecommendations",
        ),
        "webshop-getcart": ("webshop-cartstorage",),
        "webshop-listrecommendations": ("webshop-listproducts",),
    },
    "addcart": {
        "webshop-frontend": ("webshop-addcartitem", "webshop-getcart"),
        "webshop-addcartitem": ("webshop-cartstorage",),
        "webshop-getcart": ("webshop-cartstorage",),
    },
    "checkout": {
        "webshop-frontend": ("webshop-checkout",),
        "webshop-checkout": (
            "webshop-getcart",
            "webshop-listproducts",
            "webshop-currency",
            "webshop-shipmentquote",
            "webshop-shiporder",
            "webshop-email",
            "webshop-emptycart",
        ),
        "webshop-getcart": ("webshop-cartstorage",),
        "webshop-emptycart": ("webshop-cartstorage",),
    },
}

SUMMARY_METRICS = ("mean", "median", "p90", "p95")
