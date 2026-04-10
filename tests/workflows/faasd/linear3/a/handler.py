import os
from time import sleep

import requests


GATEWAY_BASE = os.environ.get("FAASD_GATEWAY_URL", "http://faasd.com").rstrip("/")


def _build_forward_headers(headers: dict) -> dict[str, str]:
    forwarded = dict(headers)
    forwarded.pop("Host", None)
    forwarded.pop("Content-Length", None)
    return forwarded


def handle(event, context):
    sleep(0.5)
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
        },
    }