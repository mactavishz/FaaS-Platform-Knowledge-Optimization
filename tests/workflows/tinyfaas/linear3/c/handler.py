#!/usr/bin/env python3

import typing
import requests

def handle(input: typing.Optional[str], headers: typing.Optional[typing.Dict[str, str]]) -> typing.Optional[str]:
    return {
        "msg": "Function linear3-c is finished",
        "data": "Hello World! End of the story."
    }
