import os
from time import sleep
import logging

logging.basicConfig(
    level=logging.INFO,
    format="{asctime} {levelname} {message}",
    style="{",
    datefmt="%Y-%m-%d %H:%M:%S",
)
logging.getLogger().setLevel(logging.INFO)


def handle(event, context):
    logging.info("Function linear3-c is called")
    sleep(0.1)
    return {
        "statusCode": 200,
        "body": {
            "msg": "Function linear3-c is finished",
            "request_headers": dict(event.headers),
            "env": dict(os.environ),
        },
    }
