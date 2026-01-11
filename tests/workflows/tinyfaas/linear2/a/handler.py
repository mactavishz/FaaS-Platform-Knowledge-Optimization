#!/usr/bin/env python3

import time
import typing
import requests

def handle(input: typing.Optional[str], headers: typing.Optional[typing.Dict[str, str]]) -> typing.Optional[str]:
    time.sleep(0.5)
    res = requests.get("http://tinyfaas.com/fn/linear2-b", headers=headers)
    return {
        "msg": "Function linear2-a is finished",
        "data": res.json()
    }