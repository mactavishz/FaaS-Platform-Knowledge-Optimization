import asyncio

from fastapi import Request
from fastapi.responses import JSONResponse


async def handle(request: Request) -> JSONResponse:
    _ = request
    await asyncio.sleep(0.3)
    return JSONResponse(
        content={
            "msg": "Function linear2-b is finished",
            "request_headers": dict(request.headers),
        }
    )
