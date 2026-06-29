"""案例库 API 路由。"""

from __future__ import annotations

import time

from fastapi import APIRouter, File, Form, HTTPException, Request, Response, UploadFile
from pydantic import ValidationError

from manage.cases.jsonl import export_jsonl_bytes, parse_jsonl_bytes
from manage.cases.models import (
    CaseCreate,
    CaseExample,
    CaseMessage,
    CaseMessageInsert,
    CaseMessagesReplace,
    CaseMetadataPatch,
    CaseResources,
)
from manage.cases.store import CaseExampleStore
from manage.platform.audit import AuditLog
from manage.platform.auth import authenticate, require_admin


def _parse_csv_ids(raw: str) -> list[str]:
    return [part.strip() for part in raw.split(",") if part.strip()]


def build_cases_router(store: CaseExampleStore, audit: AuditLog) -> APIRouter:
    router = APIRouter(prefix="/v1/cases", tags=["cases"])

    @router.get("", response_model=list[CaseExample])
    def list_cases(request: Request) -> list[CaseExample]:
        authenticate(request)
        return store.list()

    @router.post("/parse-jsonl", response_model=list[CaseMessage])
    async def parse_jsonl_preview(
        request: Request,
        file: UploadFile = File(...),
    ) -> list[CaseMessage]:
        auth = authenticate(request)
        require_admin(auth)
        data = await file.read()
        if not data.strip():
            raise HTTPException(status_code=422, detail="empty file")
        try:
            return parse_jsonl_bytes(data)
        except ValueError as exc:
            raise HTTPException(status_code=422, detail=str(exc)) from exc

    @router.post("", response_model=CaseExample)
    async def create_case(
        request: Request,
        case_id: str = Form(...),
        name: str = Form(...),
        description: str = Form(""),
        skill_ids: str = Form(""),
        plugin_ids: str = Form(""),
        file: UploadFile | None = File(None),
    ) -> CaseExample:
        auth = authenticate(request)
        require_admin(auth)
        try:
            payload = CaseCreate(
                case_id=case_id.strip(),
                name=name.strip(),
                description=description,
                resources=CaseResources(
                    skill_ids=_parse_csv_ids(skill_ids),
                    plugin_ids=_parse_csv_ids(plugin_ids),
                ),
            )
        except ValidationError as exc:
            raise HTTPException(status_code=422, detail="invalid case metadata") from exc
        messages: list[CaseMessage] = []
        if file is not None:
            data = await file.read()
            if data.strip():
                try:
                    messages = parse_jsonl_bytes(data)
                except ValueError as exc:
                    raise HTTPException(status_code=422, detail=str(exc)) from exc
        try:
            case = store.create(payload, messages=messages, now=int(time.time()))
        except KeyError as exc:
            raise HTTPException(status_code=409, detail="case_id already exists") from exc
        audit.record(actor=auth.token_id, action="case.create", target_agent_id=payload.case_id)
        return case

    @router.get("/{case_id}", response_model=CaseExample)
    def get_case(case_id: str, request: Request) -> CaseExample:
        authenticate(request)
        case = store.get(case_id)
        if not case:
            raise HTTPException(status_code=404, detail="not found")
        return case

    @router.patch("/{case_id}", response_model=CaseExample)
    def patch_case(case_id: str, body: CaseMetadataPatch, request: Request) -> CaseExample:
        auth = authenticate(request)
        require_admin(auth)
        case = store.patch_metadata(case_id, body, now=int(time.time()))
        if not case:
            raise HTTPException(status_code=404, detail="not found")
        audit.record(actor=auth.token_id, action="case.update", target_agent_id=case_id)
        return case

    @router.delete("/{case_id}")
    def delete_case(case_id: str, request: Request) -> dict[str, str]:
        auth = authenticate(request)
        require_admin(auth)
        case = store.delete(case_id)
        if not case:
            raise HTTPException(status_code=404, detail="not found")
        audit.record(actor=auth.token_id, action="case.delete", target_agent_id=case_id)
        return {"status": "deleted", "case_id": case_id}

    @router.post("/{case_id}/import-jsonl", response_model=CaseExample)
    async def import_jsonl(
        case_id: str,
        request: Request,
        file: UploadFile = File(...),
        replace: bool = Form(True),
    ) -> CaseExample:
        auth = authenticate(request)
        require_admin(auth)
        data = await file.read()
        try:
            case = store.import_jsonl(case_id, data, replace=replace, now=int(time.time()))
        except ValueError as exc:
            raise HTTPException(status_code=422, detail=str(exc)) from exc
        if not case:
            raise HTTPException(status_code=404, detail="not found")
        audit.record(actor=auth.token_id, action="case.import_jsonl", target_agent_id=case_id)
        return case

    @router.put("/{case_id}/messages", response_model=CaseExample)
    def replace_messages(case_id: str, body: CaseMessagesReplace, request: Request) -> CaseExample:
        auth = authenticate(request)
        require_admin(auth)
        case = store.replace_messages(case_id, body.messages, now=int(time.time()))
        if not case:
            raise HTTPException(status_code=404, detail="not found")
        audit.record(actor=auth.token_id, action="case.messages.replace", target_agent_id=case_id)
        return case

    @router.post("/{case_id}/messages", response_model=CaseExample)
    def insert_message(case_id: str, body: CaseMessageInsert, request: Request) -> CaseExample:
        auth = authenticate(request)
        require_admin(auth)
        case = store.insert_message(
            case_id,
            body.message,
            index=body.index,
            now=int(time.time()),
        )
        if not case:
            raise HTTPException(status_code=404, detail="not found")
        audit.record(actor=auth.token_id, action="case.message.insert", target_agent_id=case_id)
        return case

    @router.patch("/{case_id}/messages/{message_id}", response_model=CaseExample)
    def update_message(
        case_id: str,
        message_id: str,
        body: CaseMessage,
        request: Request,
    ) -> CaseExample:
        auth = authenticate(request)
        require_admin(auth)
        if body.id and body.id != message_id:
            raise HTTPException(status_code=422, detail="message id mismatch")
        patch = body.model_copy(update={"id": message_id})
        case = store.update_message(case_id, message_id, patch, now=int(time.time()))
        if not case:
            raise HTTPException(status_code=404, detail="not found")
        audit.record(
            actor=auth.token_id,
            action="case.message.update",
            target_agent_id=f"{case_id}/{message_id}",
        )
        return case

    @router.delete("/{case_id}/messages/{message_id}", response_model=CaseExample)
    def delete_message(case_id: str, message_id: str, request: Request) -> CaseExample:
        auth = authenticate(request)
        require_admin(auth)
        case = store.delete_message(case_id, message_id, now=int(time.time()))
        if not case:
            raise HTTPException(status_code=404, detail="not found")
        audit.record(
            actor=auth.token_id,
            action="case.message.delete",
            target_agent_id=f"{case_id}/{message_id}",
        )
        return case

    @router.get("/{case_id}/export/jsonl")
    def export_jsonl(case_id: str, request: Request) -> Response:
        authenticate(request)
        case = store.get(case_id)
        if not case:
            raise HTTPException(status_code=404, detail="not found")
        data = export_jsonl_bytes(case.messages)
        filename = f"{case_id}.jsonl"
        return Response(
            content=data,
            media_type="application/x-ndjson",
            headers={"Content-Disposition": f'attachment; filename="{filename}"'},
        )

    return router
