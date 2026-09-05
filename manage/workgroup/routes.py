"""Workgroup HTTP API（Timeline、AgentRef turn、HITL 与 outbox）。"""

from __future__ import annotations

import asyncio
from queue import Empty as QueueEmpty
from typing import Any

from fastapi import APIRouter, HTTPException, Request
from fastapi.responses import Response, StreamingResponse
import json

from manage.llm.models import LLMConfigMasked
from manage.platform.audit import AuditLog
from manage.platform.auth import audit_actor, authenticate, ensure_node_identity, extract_agent_id
from manage.workgroup.d3_models import (
    HITLCreateRequest,
    HITLRequest,
    HITLResolveRequest,
    HumanPostRequest,
    OutboxFrame,
    QueuedHumanPatchRequest,
    SubscribeRequest,
    Subscription,
    TimelineEvent,
    TurnCancelRequest,
    TurnCancelResponse,
)
from manage.workgroup.errors import WorkgroupError
from manage.workgroup.models import (
    ActorRun,
    ActorRunCreateRequest,
    Assign,
    AssignCreateRequest,
    ACLPatchRequest,
    MemberCreateRequest,
    MemberPatchRequest,
    WorkGroup,
    WorkGroupACL,
    WorkGroupCreateRequest,
    WorkGroupMember,
    WorkGroupPatchRequest,
)
from manage.workgroup.llm_chat import describe_llm_resolution
from manage.workgroup.store import WorkGroupStore
from manage.workgroup.turn_kernel import TurnKernel
from manage.workgroup.vertical import VerticalLoop
from manage.workgroup.ws_hub import WorkgroupWSHub


def _http_error(exc: WorkgroupError) -> HTTPException:
    return HTTPException(status_code=exc.http_status, detail=exc.as_body())


def _sse_json(obj: Any) -> str:
    return json.dumps(obj, ensure_ascii=False, default=str)


def _sse_pack(event: str, data: Any, *, event_id: str | None = None) -> str:
    prefix = f"id: {event_id}\n" if event_id else ""
    return f"{prefix}event: {event}\ndata: {_sse_json(data)}\n\n"


def build_workgroup_router(
    store: WorkGroupStore,
    audit: AuditLog,
    *,
    hub: WorkgroupWSHub | None = None,
    llm_store: Any | None = None,
    registry_store: Any | None = None,
    mock_llm: bool = False,
    loop: VerticalLoop | None = None,
) -> APIRouter:
    router = APIRouter(prefix="/v1/workgroups", tags=["workgroups"])
    loop = loop or VerticalLoop(store, hub=hub)
    kernel = TurnKernel(
        store,
        llm_store=llm_store,
        registry_store=registry_store,
        mock_llm=mock_llm,
    )
    # ActorRun/Assign workers are process-local.  On a fresh Manage process,
    # fence stale active records before accepting new turns; pending HITL rows
    # remain durable and visible for the explicit recovery path.
    store.reconcile_inflight_runs()
    loop.set_turn_kernel(kernel)
    if hub is not None:
        store.set_timeline_listener(hub.publish_timeline_event)
        store.set_hitl_listener(hub.publish_hitl_change)
        kernel.set_realtime_event_listener(
            lambda workgroup_id, event_type, data, client_message_id=None: hub.publish_realtime_event(
                workgroup_id,
                event_type,
                data,
                client_message_id=client_message_id,
            )
        )
    kernel.set_assign_completer(loop.make_assign_completer(kernel))
    kernel.set_turn_cancel_hook(loop.cancel_pending_agent_turns)
    kernel.set_assign_cancel_hook(
        lambda workgroup_id, assign_id: loop.cancel_pending_agent_turns(
            workgroup_id, assign_id=assign_id
        )
    )
    kernel.resume_persisted_queues()
    kernel.resume_persisted_hitls()

    @router.post("", response_model=dict)
    def create_workgroup(req: WorkGroupCreateRequest, request: Request) -> dict:
        auth = authenticate(request)
        ensure_node_identity(request, req.created_by_node_id, auth)
        try:
            group, acl = store.create_workgroup(req)
        except WorkgroupError as exc:
            raise _http_error(exc) from exc
        audit.record(
            actor=audit_actor(request, auth, fallback_agent_id=req.created_by_node_id),
            action="workgroup.create",
            target_agent_id=group.workgroup_id,
        )
        return {"workgroup": group, "acl": acl}

    @router.get("", response_model=list[WorkGroup])
    def list_workgroups(
        request: Request,
        subscribed_by: str | None = None,
        acl_member: str | None = None,
        include_archived: bool = False,
    ) -> list[WorkGroup]:
        authenticate(request)
        return store.list_workgroups(
            subscribed_by=subscribed_by,
            acl_member=acl_member,
            include_archived=include_archived,
        )

    @router.get("/{workgroup_id}", response_model=WorkGroup)
    def get_workgroup(workgroup_id: str, request: Request) -> WorkGroup:
        authenticate(request)
        group = store.get_workgroup(workgroup_id)
        if group is None:
            raise HTTPException(status_code=404, detail={"code": "not_found", "message": "workgroup not found"})
        return group

    @router.patch("/{workgroup_id}", response_model=WorkGroup)
    def patch_workgroup(workgroup_id: str, req: WorkGroupPatchRequest, request: Request) -> WorkGroup:
        auth = authenticate(request)
        try:
            group = store.patch_workgroup(workgroup_id, req)
        except WorkgroupError as exc:
            raise _http_error(exc) from exc
        audit.record(actor=audit_actor(request, auth), action="workgroup.patch", target_agent_id=workgroup_id)
        return group

    @router.get("/{workgroup_id}/llm-configs", response_model=list[LLMConfigMasked])
    def list_workgroup_llm_configs(workgroup_id: str, request: Request) -> list[LLMConfigMasked]:
        auth = authenticate(request)
        if store.get_workgroup(workgroup_id) is None:
            raise HTTPException(status_code=404, detail={"code": "not_found", "message": "workgroup not found"})
        if llm_store is None:
            return []
        return [
            llm_store.mask(c)
            for c in llm_store.list()
            if auth.allows_resource_groups(c.allowed_groups)
        ]

    @router.post("/{workgroup_id}/archive", response_model=WorkGroup)
    def archive_workgroup(workgroup_id: str, request: Request) -> WorkGroup:
        auth = authenticate(request)
        try:
            group = store.begin_archive(workgroup_id)
        except WorkgroupError as exc:
            raise _http_error(exc) from exc
        audit.record(actor=audit_actor(request, auth), action="workgroup.archive", target_agent_id=workgroup_id)
        return group

    @router.post("/{workgroup_id}/publish", response_model=WorkGroup)
    def publish_workgroup(workgroup_id: str, request: Request) -> WorkGroup:
        auth = authenticate(request)
        try:
            group = store.publish_workgroup(workgroup_id)
        except WorkgroupError as exc:
            raise _http_error(exc) from exc
        audit.record(actor=audit_actor(request, auth), action="workgroup.publish", target_agent_id=workgroup_id)
        return group

    @router.get("/{workgroup_id}/acl", response_model=WorkGroupACL)
    def get_acl(workgroup_id: str, request: Request) -> WorkGroupACL:
        authenticate(request)
        acl = store.get_acl(workgroup_id)
        if acl is None:
            raise HTTPException(status_code=404, detail={"code": "not_found", "message": "acl not found"})
        return acl

    @router.patch("/{workgroup_id}/acl", response_model=WorkGroupACL)
    def patch_acl(workgroup_id: str, req: ACLPatchRequest, request: Request) -> WorkGroupACL:
        auth = authenticate(request)
        try:
            acl = store.patch_acl(workgroup_id, req)
        except WorkgroupError as exc:
            raise _http_error(exc) from exc
        audit.record(actor=audit_actor(request, auth), action="workgroup.acl.patch", target_agent_id=workgroup_id)
        return acl

    @router.post("/{workgroup_id}/members", response_model=dict)
    def create_member(workgroup_id: str, req: MemberCreateRequest, request: Request) -> dict:
        auth = authenticate(request)
        registry = getattr(request.app.state, "registry_store", None)
        # AgentRef members bind an existing Node Agent. Resolve the owning Node
        # from the Manage registry so the client does not need to invent a
        # placement/home_node value.
        if req.agent_id and registry is not None:
            record = registry.get(req.agent_id.strip())
            if record is None:
                raise HTTPException(
                    status_code=404,
                    detail={"code": "agent_not_found", "message": "agent not registered"},
                )
            req = req.model_copy(update={"home_node_id": record.node_id or record.agent_id})

        # Node 会话：Home 必须与当前 Node 共享至少一个 discovery_group
        if auth.session_kind == "node" or (auth.agent_id and not auth.is_admin):
            caller_id = (auth.agent_id or extract_agent_id(request) or "").strip()
            if registry is not None and caller_id:
                caller = registry.get(caller_id)
                home = registry.get(req.home_node_id.strip())
                caller_groups = set(caller.discovery_group) if caller else set()
                home_groups = set(home.discovery_group) if home else set()
                if not caller_groups or not (caller_groups & home_groups):
                    raise HTTPException(
                        status_code=403,
                        detail={
                            "code": "not_authorized",
                            "message": "home_node must share a discovery_group with the caller node",
                        },
                    )

        try:
            member = store.create_member(workgroup_id, req)
            loop.enqueue_agent_session_open(workgroup_id, member.member_id)
            member = store.get_member(member.member_id) or member
        except WorkgroupError as exc:
            raise _http_error(exc) from exc

        audit.record(
            actor=audit_actor(request, auth),
            action="workgroup.member.create",
            target_agent_id=member.member_id,
            detail={"home_node_id": member.home_node_id},
        )
        return {"member": member}

    @router.patch("/{workgroup_id}/members/{member_id}", response_model=dict)
    def patch_member(
        workgroup_id: str, member_id: str, req: MemberPatchRequest, request: Request
    ) -> dict:
        auth = authenticate(request)
        try:
            member = store.update_member(workgroup_id, member_id, req)
            loop.enqueue_agent_session_open(workgroup_id, member.member_id)
            member = store.get_member(member.member_id) or member
        except WorkgroupError as exc:
            raise _http_error(exc) from exc
        audit.record(
            actor=audit_actor(request, auth),
            action="workgroup.member.patch",
            target_agent_id=member.member_id,
        )
        return {"member": member}

    @router.get("/{workgroup_id}/members", response_model=list[WorkGroupMember])
    def list_members(workgroup_id: str, request: Request) -> list[WorkGroupMember]:
        authenticate(request)
        # 修复历史缺口：创建成员时未自动订阅 Home 的工作组
        try:
            added = store.ensure_member_homes_subscribed(workgroup_id)
        except WorkgroupError:
            added = []
        if added and hub is not None:
            for nid in added:
                hub.request_resume(nid, workgroup_id)
        return store.list_members(workgroup_id)

    @router.post("/{workgroup_id}/members/{member_id}/archive", response_model=WorkGroupMember)
    def archive_member(workgroup_id: str, member_id: str, request: Request) -> WorkGroupMember:
        auth = authenticate(request)
        try:
            existing = store.get_member(member_id)
            was_archived = existing is not None and existing.status == "archived"
            member = store.archive_member(workgroup_id, member_id)
            if not was_archived:
                loop.enqueue_agent_session_close(workgroup_id, member_id)
        except WorkgroupError as exc:
            raise _http_error(exc) from exc
        audit.record(
            actor=audit_actor(request, auth),
            action="workgroup.member.archive",
            target_agent_id=member_id,
        )
        return member

    @router.post("/{workgroup_id}/assigns", response_model=Assign)
    def create_assign(workgroup_id: str, req: AssignCreateRequest, request: Request) -> Assign:
        """Persist an explicit queued assignment for control-plane clients."""
        auth = authenticate(request)
        try:
            assign = kernel.assign_member(workgroup_id, req)
        except WorkgroupError as exc:
            raise _http_error(exc) from exc
        audit.record(
            actor=audit_actor(request, auth),
            action="workgroup.assign.create",
            target_agent_id=assign.assign_id,
        )
        return assign

    @router.post("/{workgroup_id}/runs", response_model=ActorRun)
    def create_run(workgroup_id: str, req: ActorRunCreateRequest, request: Request) -> ActorRun:
        auth = authenticate(request)
        try:
            run = store.create_actor_run(workgroup_id, req)
        except WorkgroupError as exc:
            raise _http_error(exc) from exc
        audit.record(actor=audit_actor(request, auth), action="workgroup.run.create", target_agent_id=run.run_id)
        return run

    def _profile_id_for_actor(workgroup_id: str, actor_id: str) -> str:
        group = store.get_workgroup(workgroup_id)
        return str(group.llm_profile_id if group else "default") or "default"

    def _llm_meta(workgroup_id: str, actor_id: str = "leader") -> dict:
        return describe_llm_resolution(
            llm_store,
            profile_id=_profile_id_for_actor(workgroup_id, actor_id),
            mock=bool(getattr(kernel, "_mock_llm", False)),
        )

    @router.get("/{workgroup_id}/runs")
    def list_runs(
        workgroup_id: str,
        request: Request,
        actor_id: str | None = None,
        limit: int = 20,
    ) -> dict:
        authenticate(request)
        if store.get_workgroup(workgroup_id) is None:
            raise HTTPException(status_code=404, detail={"code": "not_found", "message": "workgroup not found"})
        runs = store.list_actor_runs(workgroup_id, actor_id=actor_id, limit=limit)
        return {
            "runs": runs,
            "llm": _llm_meta(workgroup_id, actor_id or "leader"),
        }

    @router.get("/{workgroup_id}/runs/{run_id}/history")
    def get_run_history(workgroup_id: str, run_id: str, request: Request) -> dict:
        authenticate(request)
        if store.get_workgroup(workgroup_id) is None:
            raise HTTPException(status_code=404, detail={"code": "not_found", "message": "workgroup not found"})
        run = store.get_actor_run(run_id)
        if run is None or run.workgroup_id != workgroup_id:
            raise HTTPException(status_code=404, detail={"code": "not_found", "message": "actor run not found"})
        hist = store.get_run_history(run_id)
        return {
            "run": run,
            "history": hist,
            "llm": _llm_meta(workgroup_id, run.actor_id),
        }

    @router.get("/{workgroup_id}/projector")
    def projector(
        workgroup_id: str,
        request: Request,
        actor_id: str = "leader",
        run_id: str | None = None,
        member_id: str | None = None,
    ) -> dict:
        authenticate(request)
        if store.get_workgroup(workgroup_id) is None:
            raise HTTPException(status_code=404, detail={"code": "not_found", "message": "workgroup not found"})
        return kernel.project(actor_id=actor_id, run_id=run_id, member_id=member_id)

    # --- D4: 持久订阅（与 WS 连接分离）---

    @router.post("/{workgroup_id}/subscribe", response_model=Subscription)
    def subscribe_workgroup(
        workgroup_id: str, req: SubscribeRequest, request: Request
    ) -> Subscription:
        auth = authenticate(request)
        ensure_node_identity(request, req.node_id, auth)
        try:
            sub = store.subscribe(workgroup_id, req.node_id)
        except WorkgroupError as exc:
            raise _http_error(exc) from exc
        audit.record(
            actor=audit_actor(request, auth, fallback_agent_id=req.node_id),
            action="workgroup.subscribe",
            target_agent_id=workgroup_id,
        )
        return sub

    @router.delete("/{workgroup_id}/subscribe")
    def unsubscribe_workgroup(
        workgroup_id: str, request: Request, node_id: str | None = None
    ) -> dict:
        auth = authenticate(request)
        nid = (node_id or extract_agent_id(request) or "").strip()
        if not nid:
            raise HTTPException(
                status_code=400,
                detail={"code": "schema_mismatch", "message": "node_id required"},
            )
        ensure_node_identity(request, nid, auth)
        try:
            store.unsubscribe(workgroup_id, nid)
        except WorkgroupError as exc:
            raise _http_error(exc) from exc
        audit.record(
            actor=audit_actor(request, auth, fallback_agent_id=nid),
            action="workgroup.unsubscribe",
            target_agent_id=workgroup_id,
        )
        return {"ok": True, "workgroup_id": workgroup_id, "node_id": nid}

    @router.get("/{workgroup_id}/subscribers", response_model=list[Subscription])
    def list_subscribers(workgroup_id: str, request: Request) -> list[Subscription]:
        authenticate(request)
        if store.get_workgroup(workgroup_id) is None:
            raise HTTPException(status_code=404, detail={"code": "not_found", "message": "workgroup not found"})
        return store.list_subscribers(workgroup_id)

    # --- Timeline / Outbox / HITL / AgentRef events ---

    @router.post("/{workgroup_id}/messages")
    def post_human_message(
        workgroup_id: str, req: HumanPostRequest, request: Request
    ) -> dict:
        auth = authenticate(request)
        ensure_node_identity(request, req.from_node_id, auth)
        try:
            result = kernel.handle_human_message(
                workgroup_id,
                text=req.text,
                from_node_id=req.from_node_id,
                client_message_id=req.client_message_id,
                disable_tools=req.disable_tools,
                direct_member_id=req.direct_member_id,
            )
        except WorkgroupError as exc:
            raise _http_error(exc) from exc
        audit.record(
            actor=audit_actor(request, auth, fallback_agent_id=req.from_node_id),
            action="workgroup.message.human",
            target_agent_id=workgroup_id,
        )
        if result.get("queued"):
            return {
                "queued": True,
                "queue_id": result.get("queue_id"),
                "position": result.get("position"),
                "text": result.get("text"),
                "from_node_id": result.get("from_node_id"),
                "client_message_id": result.get("client_message_id"),
                "direct_member_id": result.get("direct_member_id"),
                "queue": result.get("queue") or kernel.list_human_queue(workgroup_id),
            }
        return {
            "timeline_event": result["timeline_event"],
            "leader_run": result["leader_run"],
            "loop": {
                "steps": result["loop"].get("steps"),
                "status": result["loop"].get("status"),
                "final_text": result["loop"].get("final_text"),
            },
            "mode": result.get("mode") or "leader",
        }

    @router.get("/{workgroup_id}/human-queue")
    def get_human_queue(workgroup_id: str, request: Request) -> dict:
        authenticate(request)
        try:
            return kernel.list_human_queue(workgroup_id)
        except WorkgroupError as exc:
            raise _http_error(exc) from exc

    @router.patch("/{workgroup_id}/human-queue/{queue_id}")
    def patch_human_queue_item(
        workgroup_id: str, queue_id: str, req: QueuedHumanPatchRequest, request: Request
    ) -> dict:
        auth = authenticate(request)
        try:
            item = kernel.patch_human_queue_item(workgroup_id, queue_id, text=req.text)
        except WorkgroupError as exc:
            raise _http_error(exc) from exc
        audit.record(
            actor=audit_actor(request, auth),
            action="workgroup.human_queue.patch",
            target_agent_id=workgroup_id,
        )
        return item

    @router.delete("/{workgroup_id}/human-queue/{queue_id}")
    def delete_human_queue_item(workgroup_id: str, queue_id: str, request: Request) -> dict:
        auth = authenticate(request)
        try:
            out = kernel.cancel_human_queue_item(workgroup_id, queue_id)
        except WorkgroupError as exc:
            raise _http_error(exc) from exc
        audit.record(
            actor=audit_actor(request, auth),
            action="workgroup.human_queue.cancel",
            target_agent_id=workgroup_id,
        )
        return out

    @router.post("/{workgroup_id}/human-queue/{queue_id}/send-now")
    def send_human_queue_item_now(workgroup_id: str, queue_id: str, request: Request) -> dict:
        auth = authenticate(request)
        try:
            out = kernel.send_human_queue_item_now(workgroup_id, queue_id)
        except WorkgroupError as exc:
            raise _http_error(exc) from exc
        audit.record(
            actor=audit_actor(request, auth),
            action="workgroup.human_queue.send_now",
            target_agent_id=workgroup_id,
        )
        return out

    @router.post("/{workgroup_id}/turn/cancel", response_model=TurnCancelResponse)
    def cancel_workgroup_turn(
        workgroup_id: str, request: Request, req: TurnCancelRequest | None = None
    ) -> TurnCancelResponse:
        _ = req
        auth = authenticate(request)
        try:
            out = kernel.cancel_turn(workgroup_id)
        except WorkgroupError as exc:
            raise _http_error(exc) from exc
        audit.record(
            actor=audit_actor(request, auth),
            action="workgroup.turn.cancel",
            target_agent_id=workgroup_id,
        )
        return TurnCancelResponse.model_validate(out)

    @router.post("/{workgroup_id}/assigns/{assign_id}/cancel", response_model=dict)
    def cancel_workgroup_assign(workgroup_id: str, assign_id: str, request: Request) -> dict:
        auth = authenticate(request)
        try:
            out = kernel.cancel_assign(workgroup_id, assign_id)
        except WorkgroupError as exc:
            raise _http_error(exc) from exc
        audit.record(
            actor=audit_actor(request, auth),
            action="workgroup.assign.cancel",
            target_agent_id=assign_id,
        )
        return out

    @router.post(
        "/{workgroup_id}/assigns/{assign_id}/tools/{tool_call_id}/cancel",
        response_model=dict,
    )
    def cancel_workgroup_tool(
        workgroup_id: str, assign_id: str, tool_call_id: str, request: Request
    ) -> dict:
        auth = authenticate(request)
        assign = store.get_assign(assign_id)
        if assign is None or assign.workgroup_id != workgroup_id:
            raise HTTPException(status_code=404, detail={"code": "not_found", "message": "assign not found"})
        try:
            frame = loop.enqueue_agent_tool_cancel(workgroup_id, assign_id, tool_call_id)
        except WorkgroupError as exc:
            raise _http_error(exc) from exc
        audit.record(
            actor=audit_actor(request, auth),
            action="workgroup.tool.cancel",
            target_agent_id=tool_call_id,
        )
        return {"cancelled": True, "assign_id": assign_id, "tool_call_id": tool_call_id, "outbox_seq": frame.delivery_seq}

    @router.post("/{workgroup_id}/messages/stream")
    def post_human_message_stream(
        workgroup_id: str, req: HumanPostRequest, request: Request
    ) -> StreamingResponse:
        auth = authenticate(request)
        ensure_node_identity(request, req.from_node_id, auth)

        def event_gen():
            events = kernel.handle_human_message_events(
                workgroup_id,
                text=req.text,
                from_node_id=req.from_node_id,
                client_message_id=req.client_message_id,
                disable_tools=req.disable_tools,
                direct_member_id=req.direct_member_id,
            )
            try:
                for item in events:
                    ev = str(item.get("event") or "message")
                    raw = item.get("data")
                    if hasattr(raw, "model_dump"):
                        data = raw.model_dump(mode="json")
                    elif isinstance(raw, dict):
                        data = {
                            k: (v.model_dump(mode="json") if hasattr(v, "model_dump") else v)
                            for k, v in raw.items()
                        }
                    else:
                        data = raw
                    yield _sse_pack(ev, data)
                yield _sse_pack("done", {})
            except WorkgroupError as exc:
                yield _sse_pack("error", exc.as_body())
            except Exception as exc:  # noqa: BLE001 — 流式通道需收口
                yield _sse_pack("error", {"code": "internal", "message": str(exc)})
            finally:
                close = getattr(events, "close", None)
                if callable(close):
                    close()

        audit.record(
            actor=audit_actor(request, auth, fallback_agent_id=req.from_node_id),
            action="workgroup.message.human.stream",
            target_agent_id=workgroup_id,
        )
        return StreamingResponse(
            event_gen(),
            media_type="text/event-stream",
            headers={
                "Cache-Control": "no-cache",
                "Connection": "keep-alive",
                "X-Accel-Buffering": "no",
            },
        )

    @router.get("/{workgroup_id}/timeline", response_model=list[TimelineEvent])
    def get_timeline(workgroup_id: str, request: Request, limit: int = 0) -> list[TimelineEvent]:
        authenticate(request)
        if store.get_workgroup(workgroup_id) is None:
            raise HTTPException(status_code=404, detail={"code": "not_found", "message": "workgroup not found"})
        if limit < 0 or limit > 5000:
            raise HTTPException(status_code=422, detail="limit must be between 0 and 5000")
        events = _timeline_for_ui(store, store.list_timeline(workgroup_id))
        if limit > 0:
            events = events[-limit:]
        return events

    @router.get("/{workgroup_id}/events")
    def workgroup_events(
        workgroup_id: str,
        request: Request,
        after_seq: int = 0,
    ) -> StreamingResponse:
        """浏览器工作组事件流。

        Node 与 Manage 继续使用独立的强身份 WebSocket；浏览器只订阅
        Manage 已鉴权、已持久化的 Timeline/HITL/队列投影。``after_seq``
        是 Timeline seq，断线重连时由客户端补发，浏览器不参与 Node outbox ack。
        """
        authenticate(request)
        if store.get_workgroup(workgroup_id) is None:
            raise HTTPException(
                status_code=404,
                detail={"code": "not_found", "message": "workgroup not found"},
            )
        if after_seq < 0:
            raise HTTPException(status_code=422, detail="after_seq must be >= 0")
        if hub is None:
            raise HTTPException(
                status_code=503,
                detail={"code": "unavailable", "message": "workgroup event hub unavailable"},
            )

        connection, replay, pending = hub.subscribe_browser(
            workgroup_id,
            after_seq=after_seq,
        )

        async def event_gen():
            try:
                for event in replay:
                    yield _sse_pack(
                        "timeline.event",
                        event.model_dump(mode="json"),
                        event_id=str(event.seq),
                    )
                yield _sse_pack(
                    "ready",
                    {
                        "workgroup_id": workgroup_id,
                        "after_seq": after_seq,
                        "timeline_seq": max(
                            [int(getattr(event, "seq", 0) or 0) for event in replay]
                            + [after_seq]
                        ),
                        "pending_hitl": [
                            item.model_dump(mode="json") for item in pending
                        ],
                    },
                )
                while True:
                    if await request.is_disconnected():
                        break

                    def take_message():
                        try:
                            return connection.queue.get(timeout=15)
                        except QueueEmpty:
                            return "__heartbeat__"

                    message = await asyncio.to_thread(take_message)
                    if message == "__heartbeat__":
                        yield ": heartbeat\n\n"
                        continue
                    if message is None:
                        break
                    if not isinstance(message, dict):
                        continue
                    event_type = str(message.get("type") or "message")
                    payload = message.get("payload")
                    event_id = None
                    if event_type == "timeline.event" and isinstance(payload, dict):
                        event_id = str(payload.get("seq") or "") or None
                    yield _sse_pack(event_type, payload or {}, event_id=event_id)
                    if event_type == "workgroup.resync_required":
                        break
            finally:
                hub.unsubscribe_browser(connection)

        return StreamingResponse(
            event_gen(),
            media_type="text/event-stream",
            headers={
                "Cache-Control": "no-cache",
                "Connection": "keep-alive",
                "X-Accel-Buffering": "no",
            },
        )

    @router.get("/{workgroup_id}/timeline/export.jsonl")
    def export_timeline(workgroup_id: str, request: Request, limit: int = 5000) -> Response:
        """Export the durable Timeline as bounded, line-delimited JSON."""

        authenticate(request)
        if store.get_workgroup(workgroup_id) is None:
            raise HTTPException(
                status_code=404,
                detail={"code": "not_found", "message": "workgroup not found"},
            )
        if limit < 0 or limit > 5000:
            raise HTTPException(status_code=422, detail="limit must be between 0 and 5000")
        events = store.list_timeline(workgroup_id)
        if limit > 0:
            events = events[-limit:]
        body = "".join(
            json.dumps(event.model_dump(mode="json"), ensure_ascii=False, separators=(",", ":"))
            + "\n"
            for event in events
        )
        return Response(
            content=body,
            media_type="application/x-ndjson",
            headers={
                "Content-Disposition": f'attachment; filename="{workgroup_id}-timeline.jsonl"',
            },
        )

    @router.get("/{workgroup_id}/outbox", response_model=list[OutboxFrame])
    def get_outbox(
        workgroup_id: str, request: Request, unacked_only: bool = False
    ) -> list[OutboxFrame]:
        authenticate(request)
        if store.get_workgroup(workgroup_id) is None:
            raise HTTPException(status_code=404, detail={"code": "not_found", "message": "workgroup not found"})
        return store.list_outbox(workgroup_id, unacked_only=unacked_only)

    @router.post("/{workgroup_id}/outbox/{delivery_seq}/ack", response_model=OutboxFrame)
    def ack_outbox(workgroup_id: str, delivery_seq: int, request: Request) -> OutboxFrame:
        authenticate(request)
        try:
            return store.ack_outbox(workgroup_id, delivery_seq)
        except WorkgroupError as exc:
            raise _http_error(exc) from exc

    @router.get("/{workgroup_id}/hitl", response_model=list[HITLRequest])
    def list_hitl(
        workgroup_id: str, request: Request, pending_only: bool = True
    ) -> list[HITLRequest]:
        authenticate(request)
        if store.get_workgroup(workgroup_id) is None:
            raise HTTPException(status_code=404, detail={"code": "not_found", "message": "workgroup not found"})
        return store.list_hitl(workgroup_id, pending_only=pending_only)

    @router.post("/{workgroup_id}/hitl", response_model=HITLRequest)
    def create_hitl(workgroup_id: str, req: HITLCreateRequest, request: Request) -> HITLRequest:
        auth = authenticate(request)
        try:
            hitl = loop.create_info_hitl(workgroup_id, req)
        except WorkgroupError as exc:
            raise _http_error(exc) from exc
        audit.record(
            actor=audit_actor(request, auth),
            action="workgroup.hitl.create",
            target_agent_id=hitl.hitl_id,
        )
        return hitl

    @router.post("/{workgroup_id}/hitl/{hitl_id}/resolve", response_model=HITLRequest)
    def resolve_hitl(
        workgroup_id: str, hitl_id: str, req: HITLResolveRequest, request: Request
    ) -> HITLRequest:
        auth = authenticate(request)
        try:
            hitl = loop.resolve_info_hitl(workgroup_id, hitl_id, req)
        except WorkgroupError as exc:
            raise _http_error(exc) from exc
        audit.record(
            actor=audit_actor(request, auth),
            action="workgroup.hitl.resolve",
            target_agent_id=hitl_id,
        )
        return hitl

    return router


def _timeline_for_ui(store: Any, events: list[TimelineEvent]) -> list[TimelineEvent]:
    """编排态 assign_started：用 Assign.instruction 展开完整任务正文。"""
    out: list[TimelineEvent] = []
    for ev in events:
        if (
            ev.type == "assign_started"
            and ev.assign_id
            and str(ev.actor_id or "").strip() == "leader"
        ):
            assign = store.get_assign(ev.assign_id)
            instruction = (assign.instruction if assign else "") or ""
            instruction = instruction.strip()
            if assign and instruction:
                member = store.get_member(assign.member_id)
                display = ((member.display_name if member else "") or assign.member_id).strip()
                ev = ev.model_copy(update={"text": f"@{display}\n{instruction}"})
        out.append(ev)
    return out
