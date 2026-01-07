#!/usr/bin/env python3

import typing
import requests

def handle(input: typing.Optional[str], headers: typing.Optional[typing.Dict[str, str]]) -> typing.Optional[str]:
    res = requests.get("http://tinyfaas.com/fn/linear-c", headers=headers)
    return {
        "msg": "Function linear-b is finished",
        "data": res.json()
    }
