import os
import logging
import requests

GATEWAY_BASE = os.environ.get("TINYFAAS_GATEWAY_URL", "http://tinyfaas.com")
logging.basicConfig(
    level=logging.INFO,
    format="{asctime} {levelname} {message}",
    style="{",
    datefmt="%Y-%m-%d %H:%M:%S",
)
logging.getLogger().setLevel(logging.INFO)


def _build_forward_headers(request) -> dict[str, str]:
    forwarded = dict(request.headers)
    forwarded.pop("Host", None)
    forwarded.pop("Content-Length", None)
    return forwarded


def handle(request):
    logging.info("Function linear3-a is called")
    logging.info("Function linear3-a is calling linear3-b")
    res = requests.post(
        f"{GATEWAY_BASE}/fn/linear3-b",
        headers=_build_forward_headers(request),
        json={},
        timeout=30,
    )
    return {
        "body": {
            "msg": "Function linear3-a is finished",
            "data": res.json(),
            "request_headers": dict(request.headers),
            "env": dict(os.environ),
        }
    }
