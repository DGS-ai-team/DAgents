"""Blob API routes for managing binary artifacts."""

from __future__ import annotations

from fastapi import APIRouter, File, HTTPException, Request, Response, UploadFile

from manage.platform.auth import authenticate, require_admin
from manage.platform.blob import BlobStore


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
        authenticate(request)
        got = blob.get(blob_id)
        if got is None:
            raise HTTPException(status_code=404, detail="not found")
        data, meta = got
        return Response(content=data, media_type=meta.get("content_type", "application/octet-stream"))

    @router.head("/{blob_id}")
    def head(blob_id: str, request: Request) -> Response:
        authenticate(request)
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
        require_admin(authenticate(request))
        return {"deleted": blob.delete(blob_id)}

    return router
