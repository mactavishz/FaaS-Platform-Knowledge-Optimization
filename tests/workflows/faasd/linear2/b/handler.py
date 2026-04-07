from time import sleep

def handle(event, context):
    sleep(0.3)
    return {
        "statusCode": 200,
        "body": {
            "msg": "Function linear2-b is finished",
            "request_headers": dict(event.headers),
        }
    }