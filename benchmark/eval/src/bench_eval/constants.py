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

# Applied consistently across every figure so the thesis uses one profile color scheme throughout.
PROFILE_COLORS = {
    "baseline": "#0072B2",
    "optimized-ema": "#009E73",
    "optimized-sma": "#D55E00",
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

# Reported statistics: the mean (central tendency) and p90 (tail). p95/median
# are intentionally excluded because p95 from 50 retained samples per run is too
# noisy to report reliably and median tracks the mean closely here.
SUMMARY_METRICS = ("mean", "p90")
