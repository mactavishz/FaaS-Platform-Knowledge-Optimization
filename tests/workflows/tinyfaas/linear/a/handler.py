#!/usr/bin/env python3

import typing
import requests

def handle(input: typing.Optional[str], headers: typing.Optional[typing.Dict[str, str]]) -> typing.Optional[str]:
    res = requests.get("http://tinyfaas.com/fn/linear-b")
    return {
        "msg": "Function linear-a is finished",
        "data": res.json()
    }