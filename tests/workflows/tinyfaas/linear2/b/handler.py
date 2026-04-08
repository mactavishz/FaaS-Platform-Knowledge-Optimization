from time import sleep

def handle(request):
    sleep(0.3)
    return {
        "body": {
            "msg": "Function linear2-b is finished",
            "request_headers": dict(request.headers),
        }
    }
