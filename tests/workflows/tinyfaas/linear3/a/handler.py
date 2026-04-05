import os

import requests
from fastapi import Request
from fastapi.responses import JSONResponse

GATEWAY_BASE = os.environ.get("TINYFAAS_GATEWAY_URL", "http://tinyfaas.com")


def _build_forward_headers(request: Request) -> dict[str, str]:
    forwarded = dict(request.headers)
    forwarded.pop("host", None)
    forwarded.pop("content-length", None)
    return forwarded


async def handle(request: Request) -> JSONResponse:
    res = requests.post(
        f"{GATEWAY_BASE}/fn/linear3-b",
        headers=_build_forward_headers(request),
        json={},
        timeout=30,
    )
    return JSONResponse(
        content={"msg": "Function linear3-a is finished", "data": res.json()}
    )
