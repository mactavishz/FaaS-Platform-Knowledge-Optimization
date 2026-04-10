from time import sleep


def handle(event, context):
    sleep(0.1)
    return {
        "statusCode": 200,
        "body": {
            "msg": "Function linear3-c is finished",
            "request_headers": dict(event.headers),
        },
    }