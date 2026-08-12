"""Blob API routes for managing binary artifacts."""

from __future__ import annotations

import re

from fastapi import APIRouter, File, HTTPException, Request, Response, UploadFile

from manage.platform.auth import authenticate, require_admin
from manage.platform.blob import BlobStore

_BLOB_ID_RE = re.compile(r"^[0-9a-f]{64}$")


def _validate_blob_id(blob_id: str) -> None:
    """Raise 404 if blob_id is not a 64-char lowercase hex string."""
    if not _BLOB_ID_RE.match(blob_id):
        raise HTTPException(status_code=404, detail="not found")


def build_blob_router(blob: BlobStore) -> APIRouter:
    router = APIRouter(prefix="/v1/blobs", tags=["blob"])

    @router.post("")
    async def upload(request: Request, file: UploadFile = File(...)) -> dict:
        require_admin(authenticate(request))
        data = await file.read()
        meta = blob.put(data, content_type=file.content_type or "application/octet-stream")
        return {"blob_id": meta["blob_id"], "sha256": meta["sha256"], "size": meta["size"]}

    @router.get("/{blob_id}")
    def download(blob_id: str, request: Request) -> Response:
        _validate_blob_id(blob_id)
        authenticate(request)
        if not blob.enabled:
            raise HTTPException(status_code=503, detail="blob store disabled")
        got = blob.get(blob_id)
        if got is None:
            raise HTTPException(status_code=404, detail="not found")
        data, meta = got
        return Response(content=data, media_type=meta.get("content_type", "application/octet-stream"))

    @router.head("/{blob_id}")
    def head(blob_id: str, request: Request) -> Response:
        _validate_blob_id(blob_id)
        authenticate(request)
        if not blob.enabled:
            raise HTTPException(status_code=503, detail="blob store disabled")
        meta = blob.head(blob_id)
        if meta is None:
            raise HTTPException(status_code=404, detail="not found")
        return Response(
            status_code=200,
            headers={
                "x-blob-sha256": meta["sha256"],
                "x-blob-size": str(meta["size"]),
            },
        )

    @router.delete("/{blob_id}")
    def delete_blob(blob_id: str, request: Request) -> dict:
        _validate_blob_id(blob_id)
        require_admin(authenticate(request))
        if not blob.enabled:
            raise HTTPException(status_code=503, detail="blob store disabled")
        return {"deleted": blob.delete(blob_id)}

    return router
