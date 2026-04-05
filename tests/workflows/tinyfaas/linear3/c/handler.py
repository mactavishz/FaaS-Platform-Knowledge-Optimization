from fastapi import Request
from fastapi.responses import JSONResponse


async def handle(request: Request) -> JSONResponse:
    _ = request
    return JSONResponse(
        content={
            "msg": "Function linear3-c is finished",
            "data": "Hello World! End of the story.",
        }
    )
