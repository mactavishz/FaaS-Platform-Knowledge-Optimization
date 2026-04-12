import os

def handle(request):
    return {
        "body": {
            "msg": "Function linear3-c is finished",
            "data": "Hello World! End of the story.",
            "request_headers": dict(request.headers),
            "env": dict(os.environ),
        }
    }
