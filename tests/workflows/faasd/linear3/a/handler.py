import os
from time import sleep
import logging
import requests


GATEWAY_BASE = os.environ.get("FAASD_GATEWAY_URL", "http://faasd.com").rstrip("/")
FUNCTION_DELAY_SEC = float(os.environ.get("FUNCTION_DELAY_SEC", "0"))
logging.basicConfig(
    level=logging.INFO,
    format="{asctime} {levelname} {message}",
    style="{",
    datefmt="%Y-%m-%d %H:%M:%S",
)
logging.getLogger().setLevel(logging.INFO)


def _build_forward_headers(headers: dict) -> dict[str, str]:
    forwarded = dict(headers)
    forwarded.pop("Host", None)
    forwarded.pop("Content-Length", None)
    return forwarded


def handle(event, context):
    logging.info("Function linear3-a is called")
    if FUNCTION_DELAY_SEC > 0:
        sleep(FUNCTION_DELAY_SEC)
    logging.info("Function linear3-a is calling linear3-b")
    res = requests.post(
        f"{GATEWAY_BASE}/function/linear3-b",
        headers=_build_forward_headers(event.headers),
        json={},
        timeout=30,
    )
    return {
        "statusCode": 200,
        "body": {
            "msg": "Function linear3-a is finished",
            "linear3-b-body": res.json(),
            "request_headers": dict(event.headers),
            "env": dict(os.environ),
        },
    }
