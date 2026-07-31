"""Workgroup HTTP API（D1 基座 + D3 Timeline/HITL/outbox）。"""

from __future__ import annotations

from fastapi import APIRouter, HTTPException, Request
from pydantic import BaseModel, Field

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
    SubscribeRequest,
    Subscription,
    TimelineEvent,
    ToolResultApplyRequest,
)
from manage.workgroup.errors import WorkgroupError
from manage.workgroup.models import (
    ActorRun,
    ActorRunCreateRequest,
    Assign,
    AssignCreateRequest,
    ACLPatchRequest,
    GrantInviteRequest,
    MemberCreateRequest,
    MemberSpec,
    NodeExecutionGrant,
    WorkGroup,
    WorkGroupACL,
    WorkGroupCreateRequest,
    WorkGroupMember,
)
from manage.workgroup.store import WorkGroupStore
from manage.workgroup.turn_kernel import TurnKernel
from manage.workgroup.vertical import VerticalLoop

_SHA = r"^sha256:[0-9a-f]{64}$"


class GrantAcceptRequest(BaseModel):
    member_spec_digest: str | None = Field(default=None, pattern=_SHA)


class DispatchReadFileRequest(BaseModel):
    member_id: str
    instruction: str = Field(min_length=1)
    path: str = "README"


class ReconcileMissingJournalRequest(BaseModel):
    assign_id: str
    command_id: str
    member_id: str
    side_effect_started: bool = True


def _http_error(exc: WorkgroupError) -> HTTPException:
    return HTTPException(status_code=exc.http_status, detail=exc.as_body())


def build_workgroup_router(store: WorkGroupStore, audit: AuditLog) -> APIRouter:
    router = APIRouter(prefix="/v1/workgroups", tags=["workgroups"])
    kernel = TurnKernel(store)
    loop = VerticalLoop(store)

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
    ) -> list[WorkGroup]:
        authenticate(request)
        return store.list_workgroups(subscribed_by=subscribed_by, acl_member=acl_member)

    @router.get("/{workgroup_id}", response_model=WorkGroup)
    def get_workgroup(workgroup_id: str, request: Request) -> WorkGroup:
        authenticate(request)
        group = store.get_workgroup(workgroup_id)
        if group is None:
            raise HTTPException(status_code=404, detail={"code": "not_found", "message": "workgroup not found"})
        return group

    @router.post("/{workgroup_id}/archive", response_model=WorkGroup)
    def archive_workgroup(workgroup_id: str, request: Request) -> WorkGroup:
        auth = authenticate(request)
        try:
            group = store.begin_archive(workgroup_id)
        except WorkgroupError as exc:
            raise _http_error(exc) from exc
        audit.record(actor=audit_actor(request, auth), action="workgroup.archive", target_agent_id=workgroup_id)
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
        try:
            member, spec = store.create_member(workgroup_id, req)
        except WorkgroupError as exc:
            raise _http_error(exc) from exc
        audit.record(
            actor=audit_actor(request, auth),
            action="workgroup.member.create",
            target_agent_id=member.member_id,
        )
        return {"member": member, "spec": spec}

    @router.get("/{workgroup_id}/members", response_model=list[WorkGroupMember])
    def list_members(workgroup_id: str, request: Request) -> list[WorkGroupMember]:
        authenticate(request)
        return store.list_members(workgroup_id)

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

    @router.post("/{workgroup_id}/grants", response_model=NodeExecutionGrant)
    def invite_grant(workgroup_id: str, req: GrantInviteRequest, request: Request) -> NodeExecutionGrant:
        auth = authenticate(request)
        try:
            grant = store.invite_grant(workgroup_id, req)
        except WorkgroupError as exc:
            raise _http_error(exc) from exc
        audit.record(actor=audit_actor(request, auth), action="workgroup.grant.invite", target_agent_id=grant.grant_id)
        return grant

    @router.get("/{workgroup_id}/grants", response_model=list[NodeExecutionGrant])
    def list_grants(workgroup_id: str, request: Request) -> list[NodeExecutionGrant]:
        authenticate(request)
        return store.list_grants(workgroup_id)

    @router.post("/{workgroup_id}/grants/{grant_id}/accept", response_model=NodeExecutionGrant)
    def accept_grant(
        workgroup_id: str,
        grant_id: str,
        request: Request,
        req: GrantAcceptRequest = GrantAcceptRequest(),
    ) -> NodeExecutionGrant:
        auth = authenticate(request)
        home = extract_agent_id(request)
        if not home:
            raise HTTPException(
                status_code=400,
                detail={"code": "schema_mismatch", "message": "x-dagents-agent-id required"},
            )
        ensure_node_identity(request, home, auth)
        digest = req.member_spec_digest
        try:
            grant = store.accept_grant(grant_id, home_node_id=home, member_spec_digest=digest)
        except WorkgroupError as exc:
            raise _http_error(exc) from exc
        if grant.workgroup_id != workgroup_id:
            raise HTTPException(status_code=404, detail={"code": "not_found", "message": "grant not found"})
        audit.record(
            actor=audit_actor(request, auth, fallback_agent_id=home),
            action="workgroup.grant.accept",
            target_agent_id=grant_id,
        )
        return grant

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

    @router.post("/{workgroup_id}/runs", response_model=ActorRun)
    def create_run(workgroup_id: str, req: ActorRunCreateRequest, request: Request) -> ActorRun:
        auth = authenticate(request)
        try:
            run = store.create_actor_run(workgroup_id, req)
        except WorkgroupError as exc:
            raise _http_error(exc) from exc
        audit.record(actor=audit_actor(request, auth), action="workgroup.run.create", target_agent_id=run.run_id)
        return run

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

    @router.post("/{workgroup_id}/messages", response_model=TimelineEvent)
    def post_human_message(
        workgroup_id: str, req: HumanPostRequest, request: Request
    ) -> TimelineEvent:
        auth = authenticate(request)
        ensure_node_identity(request, req.from_node_id, auth)
        try:
            return loop.post_human(workgroup_id, req)
        except WorkgroupError as exc:
            raise _http_error(exc) from exc

    @router.get("/{workgroup_id}/timeline", response_model=list[TimelineEvent])
    def get_timeline(workgroup_id: str, request: Request) -> list[TimelineEvent]:
        authenticate(request)
        if store.get_workgroup(workgroup_id) is None:
            raise HTTPException(status_code=404, detail={"code": "not_found", "message": "workgroup not found"})
        return store.list_timeline(workgroup_id)

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

    @router.post("/{workgroup_id}/vertical/dispatch-read-file")
    def dispatch_read_file(
        workgroup_id: str, req: DispatchReadFileRequest, request: Request
    ) -> dict:
        """D3 测试/骨架入口：assign + 下发 read_file outbox（无 bridge 时仅入队）。"""
        auth = authenticate(request)
        try:
            result = loop.assign_and_dispatch_read_file(
                workgroup_id,
                member_id=req.member_id,
                instruction=req.instruction,
                path=req.path,
            )
        except WorkgroupError as exc:
            raise _http_error(exc) from exc
        audit.record(
            actor=audit_actor(request, auth),
            action="workgroup.vertical.dispatch_read_file",
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
