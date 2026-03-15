#!/usr/bin/env python3

import os
import time
import typing
import requests

GATEWAY_BASE = os.environ.get("TINYFAAS_GATEWAY_URL", "http://tinyfaas.com")


def handle(
    input: typing.Optional[str], headers: typing.Optional[typing.Dict[str, str]]
) -> typing.Optional[str]:
    time.sleep(0.5)
    res = requests.get(f"{GATEWAY_BASE}/fn/linear2-b", headers=headers)
    return {"msg": "Function linear2-a is finished", "data": res.json()}
