"""Workgroup HTTP API（D1 基座 + D3 Timeline/HITL/outbox）。"""

from __future__ import annotations

from typing import Any

from fastapi import APIRouter, HTTPException, Request
from fastapi.responses import StreamingResponse
from pydantic import BaseModel
import json

from manage.llm.models import LLMConfigMasked
from manage.platform.audit import AuditLog
from manage.platform.auth import audit_actor, authenticate, ensure_node_identity, extract_agent_id
from manage.workgroup.d3_models import (
    HITLCreateRequest,
    HITLRequest,
    HITLResolveRequest,
    HumanPostRequest,
    MemberFinalRequest,
    OutboxFrame,
    ProvisionCompleteRequest,
    QueuedHumanPatchRequest,
    SubscribeRequest,
    Subscription,
    TimelineEvent,
    ToolResultApplyRequest,
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
    MemberSpec,
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


class ReconcileMissingJournalRequest(BaseModel):
    assign_id: str
    command_id: str
    member_id: str
    side_effect_started: bool = True


def _http_error(exc: WorkgroupError) -> HTTPException:
    return HTTPException(status_code=exc.http_status, detail=exc.as_body())


def _sse_json(obj: Any) -> str:
    return json.dumps(obj, ensure_ascii=False, default=str)


def _sse_pack(event: str, data: Any) -> str:
    return f"event: {event}\ndata: {_sse_json(data)}\n\n"


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
        store.reconcile_timeline_outbox()
        store.set_timeline_listener(hub.publish_timeline_event)
        kernel.set_realtime_event_listener(
            lambda workgroup_id, event_type, data, client_message_id=None: hub.publish_realtime_event(
                workgroup_id,
                event_type,
                data,
                client_message_id=client_message_id,
            )
        )
    kernel.set_assign_completer(loop.make_assign_completer(kernel))
    kernel.set_command_cancel_hook(loop.cancel_pending_commands)
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

    @router.get("/meta/member-tools", response_model=dict)
    def get_member_tool_catalog(request: Request) -> dict:
        """Member 可勾选/可执行工具目录（与 shared/workgroup/member_tool_catalog.json 同源）。"""
        authenticate(request)
        from manage.workgroup.member_tools import member_tool_catalog

        return member_tool_catalog()

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
            if not req.allow_tool_names:
                from manage.workgroup.member_tools import default_allow_tool_names

                req = req.model_copy(update={"allow_tool_names": default_allow_tool_names()})
            member, spec = store.create_member(workgroup_id, req)
            if member.execution_mode == "agent_ref":
                loop.enqueue_agent_session_open(workgroup_id, member.member_id)
            else:
                loop.enqueue_provision(workgroup_id, member.member_id)
            member = store.get_member(member.member_id) or member
        except WorkgroupError as exc:
            raise _http_error(exc) from exc

        audit.record(
            actor=audit_actor(request, auth),
            action="workgroup.member.create",
            target_agent_id=member.member_id,
            detail={"home_node_id": member.home_node_id},
        )
        return {"member": member, "spec": spec}

    @router.patch("/{workgroup_id}/members/{member_id}", response_model=dict)
    def patch_member(
        workgroup_id: str, member_id: str, req: MemberPatchRequest, request: Request
    ) -> dict:
        auth = authenticate(request)
        try:
            member, spec = store.update_member(workgroup_id, member_id, req)
            if member.execution_mode == "agent_ref":
                loop.enqueue_agent_session_open(workgroup_id, member.member_id)
            else:
                loop.enqueue_provision(workgroup_id, member.member_id)
            member = store.get_member(member.member_id) or member
        except WorkgroupError as exc:
            raise _http_error(exc) from exc
        audit.record(
            actor=audit_actor(request, auth),
            action="workgroup.member.patch",
            target_agent_id=member.member_id,
        )
        return {"member": member, "spec": spec}

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
                loop.enqueue_member_tombstone(workgroup_id, member_id)
        except WorkgroupError as exc:
            raise _http_error(exc) from exc
        audit.record(
            actor=audit_actor(request, auth),
            action="workgroup.member.archive",
            target_agent_id=member_id,
        )
        return member

    @router.get("/{workgroup_id}/members/{member_id}/spec", response_model=MemberSpec)
    def get_member_spec(workgroup_id: str, member_id: str, request: Request) -> MemberSpec:
        authenticate(request)
        member = store.get_member(member_id)
        if member is None or member.workgroup_id != workgroup_id:
            raise HTTPException(status_code=404, detail={"code": "not_found", "message": "member not found"})
        spec = store.get_spec(member_id)
        if spec is None:
            raise HTTPException(status_code=404, detail={"code": "not_found", "message": "spec not found"})
        return spec

    @router.post("/{workgroup_id}/assigns", response_model=Assign)
    def create_assign(workgroup_id: str, req: AssignCreateRequest, request: Request) -> Assign:
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

    @router.post("/{workgroup_id}/assigns/fail-active")
    def fail_active_assigns(workgroup_id: str, request: Request) -> dict:
        """运维/自愈：释放组内卡住的 active assign。"""
        auth = authenticate(request)
        if store.get_workgroup(workgroup_id) is None:
            raise HTTPException(status_code=404, detail={"code": "not_found", "message": "workgroup not found"})
        try:
            failed = store.fail_active_assigns(
                workgroup_id,
                reason="manual fail-active",
                error_code="canceled",
            )
        except WorkgroupError as exc:
            raise _http_error(exc) from exc
        audit.record(
            actor=audit_actor(request, auth),
            action="workgroup.assign.fail_active",
            target_agent_id=workgroup_id,
        )
        return {"failed_assign_ids": failed, "count": len(failed)}

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
        profile_id = str(group.llm_profile_id if group else "default") or "default"
        aid = str(actor_id or "").strip()
        if aid and aid != "leader":
            spec = store.get_spec(aid)
            if spec is not None and str(getattr(spec, "llm_profile_id", "") or "").strip():
                profile_id = str(spec.llm_profile_id).strip()
        return profile_id

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

    # --- D3: Timeline / Outbox / HITL / provision complete ---

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
        events = _timeline_for_ui(store, store.list_timeline(workgroup_id))
        if limit > 0:
            events = events[-limit:]
        return events

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

    @router.post("/{workgroup_id}/provision-complete")
    def provision_complete(
        workgroup_id: str, req: ProvisionCompleteRequest, request: Request
    ) -> dict:
        auth = authenticate(request)
        try:
            result = loop.complete_provision(workgroup_id, req)
        except WorkgroupError as exc:
            raise _http_error(exc) from exc
        audit.record(
            actor=audit_actor(request, auth),
            action="workgroup.provision.complete",
            target_agent_id=req.member_id,
        )
        return result

    @router.post("/{workgroup_id}/tool-results")
    def apply_tool_result(
        workgroup_id: str, req: ToolResultApplyRequest, request: Request
    ) -> dict:
        auth = authenticate(request)
        try:
            result = loop.apply_tool_result(workgroup_id, req)
        except WorkgroupError as exc:
            raise _http_error(exc) from exc
        audit.record(
            actor=audit_actor(request, auth),
            action="workgroup.tool_result.apply",
            target_agent_id=req.assign_id,
        )
        return result

    @router.post("/{workgroup_id}/member-final")
    def member_final(workgroup_id: str, req: MemberFinalRequest, request: Request) -> dict:
        auth = authenticate(request)
        try:
            result = loop.member_final(workgroup_id, req)
        except WorkgroupError as exc:
            raise _http_error(exc) from exc
        audit.record(
            actor=audit_actor(request, auth),
            action="workgroup.member.final",
            target_agent_id=req.member_id,
        )
        return result

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

    @router.post("/{workgroup_id}/archive-tombstone")
    def archive_tombstone(workgroup_id: str, request: Request) -> dict:
        auth = authenticate(request)
        try:
            result = loop.archive_with_tombstone(workgroup_id)
        except WorkgroupError as exc:
            raise _http_error(exc) from exc
        audit.record(
            actor=audit_actor(request, auth),
            action="workgroup.archive_tombstone",
            target_agent_id=workgroup_id,
        )
        return result

    @router.post("/{workgroup_id}/reconcile-missing-journal")
    def reconcile_missing_journal(
        workgroup_id: str, req: ReconcileMissingJournalRequest, request: Request
    ) -> dict:
        auth = authenticate(request)
        try:
            result = loop.reconcile_missing_journal(
                workgroup_id,
                assign_id=req.assign_id,
                command_id=req.command_id,
                member_id=req.member_id,
                side_effect_started=req.side_effect_started,
            )
        except WorkgroupError as exc:
            raise _http_error(exc) from exc
        audit.record(
            actor=audit_actor(request, auth),
            action="workgroup.reconcile_missing_journal",
            target_agent_id=req.command_id,
        )
        return result

    return router


def _timeline_for_ui(store: Any, events: list[TimelineEvent]) -> list[TimelineEvent]:
    """编排态 assign_started：用 Assign.instruction 展开完整任务正文（兼容历史截断摘要）。"""
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
