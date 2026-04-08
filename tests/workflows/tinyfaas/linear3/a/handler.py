import os
import requests

GATEWAY_BASE = os.environ.get("TINYFAAS_GATEWAY_URL", "http://tinyfaas.com")


def _build_forward_headers(request) -> dict[str, str]:
    forwarded = dict(request.headers)
    forwarded.pop("Host", None)
    forwarded.pop("Content-Length", None)
    return forwarded


def handle(request):
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
        }
    }
