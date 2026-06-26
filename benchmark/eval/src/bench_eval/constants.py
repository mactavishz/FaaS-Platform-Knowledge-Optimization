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

# Exact number of times each function is invoked per benchmark iteration. Used to
# validate that no invocation record is missing from the retained window.
#
# iot/tree run their entry function once per benchmark iteration, so counts are
# keyed by workflow. Each webshop benchmark iteration is one journey that runs
# every frontend operation once, so webshop counts are keyed by operation; the
# per-operation counts are identical across the two webshop workflows that share
# that operation.
WORKFLOW_FUNCTION_CALLS = {
    "iot": {
        "iot-i": 1,
        "iot-cw": 1,
        "iot-se": 1,
        "iot-ct": 1,
        "iot-cs": 1,
        "iot-ca": 1,
        "iot-as": 2,
        "iot-csl": 1,
        "iot-csa": 1,
        "iot-dj": 1,
    },
    "tree": {
        "tree-a": 1,
        "tree-b": 1,
        "tree-c": 1,
        "tree-d": 1,
        "tree-e": 1,
        "tree-f": 1,
        "tree-g": 1,
    },
}

WEBSHOP_OPERATION_FUNCTION_CALLS = {
    "browse": {
        "webshop-frontend": 1,
        "webshop-supportedcurrencies": 1,
        "webshop-currency": 11,
        "webshop-listproducts": 2,
        "webshop-listrecommendations": 1,
        "webshop-getads": 1,
        "webshop-getcart": 1,
        "webshop-cartstorage": 1,
    },
    "addcart": {
        "webshop-frontend": 1,
        "webshop-addcartitem": 1,
        "webshop-getcart": 1,
        "webshop-cartstorage": 2,
    },
    "checkout": {
        "webshop-frontend": 1,
        "webshop-checkout": 1,
        "webshop-getcart": 1,
        "webshop-listproducts": 1,
        "webshop-currency": 2,
        "webshop-shipmentquote": 1,
        "webshop-shiporder": 1,
        "webshop-email": 1,
        "webshop-emptycart": 1,
        "webshop-cartstorage": 2,
    },
}

# Reported statistics: the mean (central tendency) and p90 (tail). p95/median
# are intentionally excluded because p95 from 50 retained samples per run is too
# noisy to report reliably and median tracks the mean closely here.
SUMMARY_METRICS = ("mean", "p90")
