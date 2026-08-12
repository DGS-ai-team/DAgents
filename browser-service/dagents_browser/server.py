from __future__ import annotations

from contextlib import asynccontextmanager
from typing import Any

from fastapi import FastAPI, Request

from dagents_browser.config import BrowserServiceSettings
from dagents_browser.driver import BrowserUseDriver


def create_app(settings: BrowserServiceSettings) -> FastAPI:
    driver = BrowserUseDriver(settings)

    @asynccontextmanager
    async def lifespan(_app: FastAPI):
        yield
        await driver.close()

    app = FastAPI(title="dagents-browser", lifespan=lifespan)

    @app.get("/health")
    async def health() -> dict[str, bool]:
        return {"ok": True}

    @app.get("/v1/browser/ping")
    async def ping() -> dict[str, Any]:
        return await driver.call({"op": "ping"})

    @app.post("/v1/browser/call")
    async def call(req: Request) -> dict[str, Any]:
        payload = await req.json()
        if not isinstance(payload, dict):
            return {"ok": False, "error": "invalid JSON object"}
        return await driver.call(payload)

    return app
