import os
from time import sleep
import logging

FUNCTION_DELAY_SEC = float(os.environ.get("FUNCTION_DELAY_SEC", "0"))

logging.basicConfig(
    level=logging.INFO,
    format="{asctime} {levelname} {message}",
    style="{",
    datefmt="%Y-%m-%d %H:%M:%S",
)
logging.getLogger().setLevel(logging.INFO)


def handle(event, context):
    logging.info("Function linear3-c is called")
    if FUNCTION_DELAY_SEC > 0:
        sleep(FUNCTION_DELAY_SEC)
    return {
        "statusCode": 200,
        "body": {
            "msg": "Function linear3-c is finished",
            "request_headers": dict(event.headers),
            "env": dict(os.environ),
        },
    }
