"""Workgroup HTTP API（D1 基座）。"""

from __future__ import annotations

from fastapi import APIRouter, HTTPException, Request
from pydantic import BaseModel, Field

from manage.platform.audit import AuditLog
from manage.platform.auth import audit_actor, authenticate, ensure_node_identity, extract_agent_id
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

_SHA = r"^sha256:[0-9a-f]{64}$"


class GrantAcceptRequest(BaseModel):
    member_spec_digest: str | None = Field(default=None, pattern=_SHA)


def _http_error(exc: WorkgroupError) -> HTTPException:
    return HTTPException(status_code=exc.http_status, detail=exc.as_body())


def build_workgroup_router(store: WorkGroupStore, audit: AuditLog) -> APIRouter:
    router = APIRouter(prefix="/v1/workgroups", tags=["workgroups"])
    kernel = TurnKernel(store)

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
    def list_workgroups(request: Request) -> list[WorkGroup]:
        authenticate(request)
        return store.list_workgroups()

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

    return router
