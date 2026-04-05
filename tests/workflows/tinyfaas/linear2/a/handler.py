#!/usr/bin/env python3

import os
import time
import typing
import requests

GATEWAY_BASE = os.environ.get("TINYFAAS_GATEWAY_URL", "http://tinyfaas.com")


def _build_forward_headers(
    headers: typing.Optional[typing.Dict[str, str]],
) -> typing.Dict[str, str]:
    forwarded = dict(headers or {})
    forwarded.pop("host", None)
    forwarded.pop("Host", None)
    forwarded.pop("content-length", None)
    forwarded.pop("Content-Length", None)
    return forwarded


def handle(
    input: typing.Optional[str], headers: typing.Optional[typing.Dict[str, str]]
) -> typing.Optional[str]:
    time.sleep(0.5)
    res = requests.post(
        f"{GATEWAY_BASE}/fn/linear2-b",
        headers=_build_forward_headers(headers),
        json={},
        timeout=30,
    )
    return {"msg": "Function linear2-a is finished", "data": res.json()}
