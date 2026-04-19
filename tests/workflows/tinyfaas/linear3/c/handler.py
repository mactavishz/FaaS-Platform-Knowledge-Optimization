import os
import logging
import time

FUNCTION_DELAY_SEC = float(os.environ.get("FUNCTION_DELAY_SEC", "0"))

logging.basicConfig(
    level=logging.INFO,
    format="{asctime} {levelname} {message}",
    style="{",
    datefmt="%Y-%m-%d %H:%M:%S",
)
logging.getLogger().setLevel(logging.INFO)


def handle(request):
    logging.info("Function linear3-c is called")
    if FUNCTION_DELAY_SEC > 0:
        time.sleep(FUNCTION_DELAY_SEC)
    return {
        "body": {
            "msg": "Function linear3-c is finished",
            "data": "Hello World! End of the story.",
            "request_headers": dict(request.headers),
            "env": dict(os.environ),
        }
    }
