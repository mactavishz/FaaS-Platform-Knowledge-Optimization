import os
import logging

logging.basicConfig(
    level=logging.INFO,
    format="{asctime} {levelname} {message}",
    style="{",
    datefmt="%Y-%m-%d %H:%M:%S",
)
logging.getLogger().setLevel(logging.INFO)


def handle(request):
    logging.info("Function linear3-c is called")
    return {
        "body": {
            "msg": "Function linear3-c is finished",
            "data": "Hello World! End of the story.",
            "request_headers": dict(request.headers),
            "env": dict(os.environ),
        }
    }
