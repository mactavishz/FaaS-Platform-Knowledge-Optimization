import os
from time import sleep

import requests

GATEWAY_BASE = os.environ.get("TINYFAAS_GATEWAY_URL", "http://tinyfaas.com")


def _build_forward_headers(request) -> dict[str, str]:
    forwarded = dict(request.headers)
    forwarded.pop("Host", None)
    forwarded.pop("Content-Length", None)
    return forwarded


def handle(request):
    sleep(0.5)
    res = requests.post(
        f"{GATEWAY_BASE}/fn/linear2-b",
        headers=_build_forward_headers(request),
        json={},
        timeout=30,
    )
    return {
        "body": {
            "msg": "Function linear2-a is finished",
            "linear2-b-body": res.json(),
            "request_headers": dict(request.headers),
        }
    }
