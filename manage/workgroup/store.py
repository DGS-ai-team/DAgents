"""WorkGroup 存储（内存 + SQLite payload_json）。"""

from __future__ import annotations

import threading
from datetime import datetime, timezone
from typing import Any

from manage.storage.sqlite import SQLiteDatabase
from manage.workgroup.digest import sha256_digest
from manage.workgroup.errors import WorkgroupError
from manage.workgroup import ids
from manage.workgroup.d3_models import HITLRequest, OutboxFrame, Subscription, TimelineEvent
from manage.workgroup.history import ActorRunHistory, RunHistoryMessage
from manage.workgroup.protocol_names import protocol_name_for_actor
from manage.workgroup.models import (
    ActorRun,
    ActorRunCreateRequest,
    Assign,
    AssignCreateRequest,
    ACLPatchRequest,
    GrantInviteRequest,
    MemberCreateRequest,
    MemberSpec,
    MemberTools,
    MemberWorkspace,
    NodeExecutionGrant,
    WorkGroup,
    WorkGroupACL,
    WorkGroupCreateRequest,
    WorkGroupMember,
    WorkGroupPatchRequest,
)


def _now() -> str:
    return datetime.now(timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z")


def _acl_contains(acl: WorkGroupACL, node_id: str) -> bool:
    return node_id in acl.owners or node_id in acl.collaborators


class WorkGroupStore:
    def __init__(self, db: SQLiteDatabase | None = None) -> None:
        self._db = db if (db and db.enabled) else None
        self._lock = threading.RLock()
        self._groups: dict[str, WorkGroup] = {}
        self._acls: dict[str, WorkGroupACL] = {}
        self._members: dict[str, WorkGroupMember] = {}
        self._specs: dict[str, MemberSpec] = {}
        self._grants: dict[str, NodeExecutionGrant] = {}
        self._assigns: dict[str, Assign] = {}
        self._runs: dict[str, ActorRun] = {}
        self._run_histories: dict[str, ActorRunHistory] = {}
        self._timeline: dict[str, list[TimelineEvent]] = {}
        self._outbox: dict[str, list[OutboxFrame]] = {}
        self._hitl: dict[str, HITLRequest] = {}
        # member_id → {workspace_path, tool_catalog_revision, provision_id}
        self._member_runtime: dict[str, dict[str, str]] = {}
        # workgroup_id → {node_id: Subscription}
        self._subscriptions: dict[str, dict[str, Subscription]] = {}

    # --- low-level persistence ---

    def _put(self, table: str, key: str, payload: str, workgroup_id: str | None = None) -> None:
        if self._db is None:
            return
        with self._db.connect() as conn:
            if workgroup_id is None:
                conn.execute(
                    f"INSERT INTO {table}(id,payload_json) VALUES(?,?) "
                    f"ON CONFLICT(id) DO UPDATE SET payload_json=excluded.payload_json",
                    (key, payload),
                )
            else:
                conn.execute(
                    f"INSERT INTO {table}(id,workgroup_id,payload_json) VALUES(?,?,?) "
                    f"ON CONFLICT(id) DO UPDATE SET workgroup_id=excluded.workgroup_id, "
                    f"payload_json=excluded.payload_json",
                    (key, workgroup_id, payload),
                )
            conn.commit()

    def _delete(self, table: str, key: str) -> None:
        if self._db is None:
            return
        with self._db.connect() as conn:
            conn.execute(f"DELETE FROM {table} WHERE id=?", (key,))
            conn.commit()

    def _load_all(self, table: str, model: type) -> list[Any]:
        if self._db is None:
            return []
        with self._db.connect() as conn:
            rows = conn.execute(f"SELECT payload_json FROM {table}").fetchall()
        return [model.model_validate_json(r["payload_json"]) for r in rows]

    def _ensure_loaded(self) -> None:
        if self._db is None or self._groups:
            return
        # Lazy hydrate once from SQLite into memory maps (process-local cache).
        for g in self._load_all("workgroups", WorkGroup):
            self._groups[g.workgroup_id] = g
        for a in self._load_all("workgroup_acls", WorkGroupACL):
            self._acls[a.workgroup_id] = a
        for m in self._load_all("workgroup_members", WorkGroupMember):
            self._members[m.member_id] = m
        for s in self._load_all("member_specs", MemberSpec):
            self._specs[s.member_id] = s
        for g in self._load_all("execution_grants", NodeExecutionGrant):
            self._grants[g.grant_id] = g
        for a in self._load_all("workgroup_assigns", Assign):
            self._assigns[a.assign_id] = a
        for r in self._load_all("actor_runs", ActorRun):
            self._runs[r.run_id] = r
        for h in self._load_all("actor_run_histories", ActorRunHistory):
            self._run_histories[h.run_id] = h
        for ev in self._load_all("workgroup_timeline", TimelineEvent):
            self._timeline.setdefault(ev.workgroup_id, []).append(ev)
        for wg, events in self._timeline.items():
            self._timeline[wg] = sorted(events, key=lambda e: e.seq)
        for frame in self._load_all("workgroup_outbox", OutboxFrame):
            # OutboxFrame.id in SQLite is f"{wg}:{delivery_seq}"
            self._outbox.setdefault(frame.workgroup_id, []).append(frame)
        for wg, frames in self._outbox.items():
            self._outbox[wg] = sorted(frames, key=lambda f: f.delivery_seq)
        for h in self._load_all("workgroup_hitl", HITLRequest):
            self._hitl[h.hitl_id] = h
        for sub in self._load_all("workgroup_subscriptions", Subscription):
            self._subscriptions.setdefault(sub.workgroup_id, {})[sub.node_id] = sub

    # --- WorkGroup ---

    def create_workgroup(self, req: WorkGroupCreateRequest) -> tuple[WorkGroup, WorkGroupACL]:
        with self._lock:
            self._ensure_loaded()
            wid = ids.workgroup_id()
            now = _now()
            group = WorkGroup(
                workgroup_id=wid,
                display_name=req.display_name.strip(),
                status="configuring",
                created_by_node_id=req.created_by_node_id.strip(),
                llm_profile_id=req.llm_profile_id.strip(),
                llm_profile_revision=req.llm_profile_revision.strip(),
                created_at=now,
            )
            acl = WorkGroupACL(
                workgroup_id=wid,
                owners=[group.created_by_node_id],
                collaborators=[],
                revision=1,
                updated_at=now,
            )
            self._groups[wid] = group
            self._acls[wid] = acl
            self._put("workgroups", wid, group.model_dump_json())
            self._put("workgroup_acls", wid, acl.model_dump_json(), workgroup_id=wid)
            # 创建者自动订阅（§10）
            sub = Subscription(
                workgroup_id=wid,
                node_id=group.created_by_node_id,
                subscribed_at=now,
            )
            self._subscriptions.setdefault(wid, {})[sub.node_id] = sub
            self._put(
                "workgroup_subscriptions",
                f"{wid}:{sub.node_id}",
                sub.model_dump_json(),
                workgroup_id=wid,
            )
            return group, acl

    def list_workgroups(
        self,
        *,
        subscribed_by: str | None = None,
        acl_member: str | None = None,
        include_archived: bool = False,
    ) -> list[WorkGroup]:
        with self._lock:
            self._ensure_loaded()
            groups = list(self._groups.values())
            if not include_archived:
                # 列表展示配置中 + 进行中；归档查询页后续再开 include_archived
                groups = [g for g in groups if g.status in {"configuring", "active"}]
            if subscribed_by:
                nid = subscribed_by.strip()
                groups = [
                    g
                    for g in groups
                    if nid in (self._subscriptions.get(g.workgroup_id) or {})
                ]
            if acl_member:
                nid = acl_member.strip()
                filtered: list[WorkGroup] = []
                for g in groups:
                    acl = self._acls.get(g.workgroup_id)
                    if acl is not None and _acl_contains(acl, nid):
                        filtered.append(g)
                groups = filtered
            return sorted(groups, key=lambda g: g.created_at, reverse=True)

    def get_workgroup(self, workgroup_id: str) -> WorkGroup | None:
        with self._lock:
            self._ensure_loaded()
            return self._groups.get(workgroup_id)

    def require_mutable(self, workgroup_id: str) -> WorkGroup:
        """配置中或已发布：允许改配置 / 成员；已归档不可改。"""
        group = self.get_workgroup(workgroup_id)
        if group is None:
            raise WorkgroupError("not_found", "workgroup not found", http_status=404)
        if group.status not in {"configuring", "active"}:
            raise WorkgroupError(
                "workgroup_archived",
                f"workgroup status={group.status}",
                http_status=409,
            )
        return group

    def require_active(self, workgroup_id: str) -> WorkGroup:
        """已发布：允许对话与运行时编排。"""
        group = self.get_workgroup(workgroup_id)
        if group is None:
            raise WorkgroupError("not_found", "workgroup not found", http_status=404)
        if group.status == "configuring":
            raise WorkgroupError(
                "workgroup_not_published",
                "workgroup must be published before conversation",
                http_status=409,
            )
        if group.status != "active":
            raise WorkgroupError("workgroup_archived", f"workgroup status={group.status}", http_status=409)
        return group

    def publish_workgroup(self, workgroup_id: str) -> WorkGroup:
        with self._lock:
            group = self.get_workgroup(workgroup_id)
            if group is None:
                raise WorkgroupError("not_found", "workgroup not found", http_status=404)
            if group.status == "active":
                return group
            if group.status != "configuring":
                raise WorkgroupError(
                    "conflict",
                    f"cannot publish workgroup status={group.status}",
                    http_status=409,
                )
            group = group.model_copy(update={"status": "active"})
            self._groups[workgroup_id] = group
            self._put("workgroups", workgroup_id, group.model_dump_json())
            return group

    def patch_workgroup(self, workgroup_id: str, req: WorkGroupPatchRequest) -> WorkGroup:
        with self._lock:
            group = self.require_mutable(workgroup_id)
            updates: dict[str, Any] = {}
            if req.display_name is not None:
                updates["display_name"] = req.display_name.strip()
            if req.llm_profile_id is not None:
                updates["llm_profile_id"] = req.llm_profile_id.strip()
            if req.llm_profile_revision is not None:
                updates["llm_profile_revision"] = req.llm_profile_revision.strip()
            elif req.llm_profile_id is not None:
                # 换 LLM 时若未显式传 revision，自增数字 revision（非数字则重置为 1）
                try:
                    updates["llm_profile_revision"] = str(int(group.llm_profile_revision) + 1)
                except ValueError:
                    updates["llm_profile_revision"] = "1"
            if not updates:
                return group
            group = group.model_copy(update=updates)
            self._groups[workgroup_id] = group
            self._put("workgroups", workgroup_id, group.model_dump_json())
            return group

    def begin_archive(self, workgroup_id: str) -> WorkGroup:
        with self._lock:
            self._ensure_loaded()
            group = self._groups.get(workgroup_id)
            if group is None:
                raise WorkgroupError("not_found", "workgroup not found", http_status=404)
            if group.status == "archived":
                return group
            if group.status in {"active", "configuring"}:
                group = group.model_copy(update={"status": "archiving"})
            elif group.status == "archiving":
                group = group.model_copy(update={"status": "archived", "archived_at": _now()})
            self._groups[workgroup_id] = group
            self._put("workgroups", workgroup_id, group.model_dump_json())
            return group

    # --- ACL ---

    def get_acl(self, workgroup_id: str) -> WorkGroupACL | None:
        with self._lock:
            self._ensure_loaded()
            return self._acls.get(workgroup_id)

    def patch_acl(self, workgroup_id: str, req: ACLPatchRequest) -> WorkGroupACL:
        with self._lock:
            self.require_mutable(workgroup_id)
            acl = self._acls.get(workgroup_id)
            if acl is None:
                raise WorkgroupError("not_found", "acl not found", http_status=404)
            if req.expected_revision != acl.revision:
                raise WorkgroupError(
                    "conflict",
                    "acl revision mismatch",
                    http_status=409,
                    details={"expected": req.expected_revision, "actual": acl.revision},
                )
            owners = req.owners if req.owners is not None else list(acl.owners)
            collaborators = req.collaborators if req.collaborators is not None else list(acl.collaborators)
            owners = [n.strip() for n in owners if str(n).strip()]
            collaborators = [n.strip() for n in collaborators if str(n).strip()]
            if not owners:
                raise WorkgroupError("schema_mismatch", "owners must be non-empty")
            # collaborators 不得与 owners 重叠
            owner_set = set(owners)
            collaborators = [n for n in collaborators if n not in owner_set]
            updated = WorkGroupACL(
                workgroup_id=workgroup_id,
                owners=owners,
                collaborators=collaborators,
                revision=acl.revision + 1,
                updated_at=_now(),
            )
            self._acls[workgroup_id] = updated
            self._put("workgroup_acls", workgroup_id, updated.model_dump_json(), workgroup_id=workgroup_id)
            return updated

    def assert_acl_member(self, workgroup_id: str, node_id: str) -> WorkGroupACL:
        acl = self.get_acl(workgroup_id)
        if acl is None:
            raise WorkgroupError("not_found", "acl not found", http_status=404)
        if not _acl_contains(acl, node_id):
            raise WorkgroupError("not_authorized", "node not in workgroup ACL", http_status=403)
        return acl

    # --- Member + MemberSpec ---

    def create_member(self, workgroup_id: str, req: MemberCreateRequest) -> tuple[WorkGroupMember, MemberSpec]:
        with self._lock:
            group = self.require_mutable(workgroup_id)
            home = req.home_node_id.strip()
            # home 必须在 ACL 内才可挂载成员
            self.assert_acl_member(workgroup_id, home)
            mid = ids.member_id()
            now = _now()
            tools = MemberTools(
                allow_names=list(req.allow_tool_names),
                side_effect_classes=list(req.side_effect_classes),
            )
            draft = {
                "member_id": mid,
                "workgroup_id": workgroup_id,
                "home_node_id": home,
                "display_name": req.display_name.strip(),
                "member_generation": 1,
                "llm_profile_id": (req.llm_profile_id or group.llm_profile_id).strip(),
                "llm_profile_revision": (req.llm_profile_revision or group.llm_profile_revision).strip(),
                "max_tool_loops": req.max_tool_loops,
                "prompt": req.prompt.model_dump(),
                "memory": req.memory.model_dump(),
                "tools": tools.model_dump(),
                "policy_ceiling": dict(req.policy_ceiling),
                "workspace": MemberWorkspace().model_dump(),
                "skills": "disabled",
                "hooks": "disabled",
            }
            digest = sha256_digest(draft)
            spec = MemberSpec.model_validate({**draft, "digest": digest})
            member = WorkGroupMember(
                member_id=mid,
                workgroup_id=workgroup_id,
                home_node_id=home,
                display_name=spec.display_name,
                status="provisioning",
                member_generation=1,
                member_spec_digest=digest,
                created_at=now,
            )
            self._specs[mid] = spec
            self._members[mid] = member
            self._member_runtime[mid] = {
                "lease_id": ids.lease_id(),
                "lease_epoch": "1",
            }
            self._put("member_specs", mid, spec.model_dump_json(), workgroup_id=workgroup_id)
            self._put("workgroup_members", mid, member.model_dump_json(), workgroup_id=workgroup_id)
            return member, spec

    def get_member(self, member_id: str) -> WorkGroupMember | None:
        with self._lock:
            self._ensure_loaded()
            return self._members.get(member_id)

    def get_spec(self, member_id: str) -> MemberSpec | None:
        with self._lock:
            self._ensure_loaded()
            return self._specs.get(member_id)

    def list_members(self, workgroup_id: str) -> list[WorkGroupMember]:
        with self._lock:
            self._ensure_loaded()
            return sorted(
                [m for m in self._members.values() if m.workgroup_id == workgroup_id],
                key=lambda m: m.created_at,
            )

    # --- Grant ---

    def invite_grant(self, workgroup_id: str, req: GrantInviteRequest) -> NodeExecutionGrant:
        with self._lock:
            self.require_mutable(workgroup_id)
            member = self._members.get(req.member_id)
            if member is None or member.workgroup_id != workgroup_id:
                raise WorkgroupError("not_found", "member not found", http_status=404)
            spec = self._specs.get(req.member_id)
            if spec is None:
                raise WorkgroupError("not_found", "member spec not found", http_status=404)
            # ACL ≠ Grant：允许邀请前再确认 home 仍在 ACL
            self.assert_acl_member(workgroup_id, member.home_node_id)
            allow = list(req.tool_allow_names) if req.tool_allow_names is not None else list(spec.tools.allow_names)
            spec_allow = set(spec.tools.allow_names)
            if any(name not in spec_allow for name in allow):
                raise WorkgroupError(
                    "digest_mismatch",
                    "grant tool_allow_names must be subset of MemberSpec.tools.allow_names",
                )
            # 同 member 已有 invited/accepted 则冲突
            for existing in self._grants.values():
                if (
                    existing.member_id == member.member_id
                    and existing.status in {"invited", "accepted"}
                ):
                    raise WorkgroupError("conflict", "active grant already exists", http_status=409)
            now = _now()
            grant = NodeExecutionGrant(
                grant_id=ids.grant_id(),
                workgroup_id=workgroup_id,
                member_id=member.member_id,
                home_node_id=member.home_node_id,
                member_spec_digest=member.member_spec_digest,
                status="invited",
                lease_id=ids.lease_id(),
                lease_epoch=1,
                member_generation=member.member_generation,
                tool_allow_names=allow,
                workspace_contract=dict(spec.workspace.model_dump()),
                policy_ceiling=dict(spec.policy_ceiling),
                invited_at=now,
            )
            self._grants[grant.grant_id] = grant
            self._put("execution_grants", grant.grant_id, grant.model_dump_json(), workgroup_id=workgroup_id)
            return grant

    def accept_grant(
        self,
        grant_id: str,
        *,
        home_node_id: str,
        member_spec_digest: str | None = None,
    ) -> NodeExecutionGrant:
        with self._lock:
            self._ensure_loaded()
            grant = self._grants.get(grant_id)
            if grant is None:
                raise WorkgroupError("not_found", "grant not found", http_status=404)
            self.require_mutable(grant.workgroup_id)
            if grant.home_node_id != home_node_id.strip():
                raise WorkgroupError("not_authorized", "only home_node may accept grant", http_status=403)
            if grant.status == "accepted":
                return grant
            if grant.status != "invited":
                raise WorkgroupError("conflict", f"grant status={grant.status}", http_status=409)
            if member_spec_digest and member_spec_digest != grant.member_spec_digest:
                raise WorkgroupError("digest_mismatch", "member_spec_digest mismatch")
            updated = grant.model_copy(update={"status": "accepted", "accepted_at": _now()})
            self._grants[grant_id] = updated
            self._put(
                "execution_grants",
                grant_id,
                updated.model_dump_json(),
                workgroup_id=updated.workgroup_id,
            )
            # 接受后成员进入 provisioning（D2 再真正 provision）
            member = self._members.get(updated.member_id)
            if member and member.status == "requested":
                member = member.model_copy(update={"status": "provisioning"})
                self._members[member.member_id] = member
                self._put(
                    "workgroup_members",
                    member.member_id,
                    member.model_dump_json(),
                    workgroup_id=member.workgroup_id,
                )
            return updated

    def revoke_grant(self, grant_id: str) -> NodeExecutionGrant:
        with self._lock:
            self._ensure_loaded()
            grant = self._grants.get(grant_id)
            if grant is None:
                raise WorkgroupError("not_found", "grant not found", http_status=404)
            if grant.status == "revoked":
                return grant
            updated = grant.model_copy(update={"status": "revoked", "revoked_at": _now()})
            self._grants[grant_id] = updated
            self._put(
                "execution_grants",
                grant_id,
                updated.model_dump_json(),
                workgroup_id=updated.workgroup_id,
            )
            return updated

    def get_grant(self, grant_id: str) -> NodeExecutionGrant | None:
        with self._lock:
            self._ensure_loaded()
            return self._grants.get(grant_id)

    def list_grants(self, workgroup_id: str) -> list[NodeExecutionGrant]:
        with self._lock:
            self._ensure_loaded()
            return sorted(
                [g for g in self._grants.values() if g.workgroup_id == workgroup_id],
                key=lambda g: g.invited_at,
            )

    def has_accepted_grant(self, member_id: str) -> bool:
        with self._lock:
            self._ensure_loaded()
            return any(
                g.member_id == member_id and g.status == "accepted" for g in self._grants.values()
            )

    # --- Turn kernel skeleton ---

    def create_actor_run(self, workgroup_id: str, req: ActorRunCreateRequest) -> ActorRun:
        with self._lock:
            group = self.require_active(workgroup_id)
            run = ActorRun(
                run_id=ids.run_id(),
                workgroup_id=workgroup_id,
                actor_id=req.actor_id.strip(),
                assign_id=req.assign_id,
                status="running",
                llm_profile_revision=(req.llm_profile_revision or group.llm_profile_revision).strip(),
                timeline_watermark_seq=0,
                checkpoint_ordinal=0,
                created_at=_now(),
            )
            self._runs[run.run_id] = run
            self._put("actor_runs", run.run_id, run.model_dump_json(), workgroup_id=workgroup_id)
            return run

    def create_assign(self, workgroup_id: str, req: AssignCreateRequest) -> Assign:
        with self._lock:
            self.require_active(workgroup_id)
            member = self._members.get(req.member_id)
            if member is None or member.workgroup_id != workgroup_id:
                raise WorkgroupError("not_found", "member not found", http_status=404)
            if member.status in {"archived", "error"}:
                raise WorkgroupError(
                    "conflict",
                    f"member status={member.status}",
                    http_status=409,
                )
            # v1：全组最多一个 active assign
            active = [
                a
                for a in self._assigns.values()
                if a.workgroup_id == workgroup_id
                and a.status in {"queued", "running", "awaiting_hitl"}
            ]
            if active:
                raise WorkgroupError(
                    "conflict",
                    "workgroup already has an active assign",
                    http_status=409,
                    details={"active_assign_id": active[0].assign_id},
                )
            leader_run_id = req.leader_run_id or ids.run_id()
            if req.leader_run_id is None:
                # 骨架：自动创建一个 leader run 占位
                leader = ActorRun(
                    run_id=leader_run_id,
                    workgroup_id=workgroup_id,
                    actor_id="leader",
                    status="running",
                    llm_profile_revision=self._groups[workgroup_id].llm_profile_revision,
                    created_at=_now(),
                )
                self._runs[leader_run_id] = leader
                self._put("actor_runs", leader_run_id, leader.model_dump_json(), workgroup_id=workgroup_id)
            assign = Assign(
                assign_id=ids.assign_id(),
                workgroup_id=workgroup_id,
                member_id=member.member_id,
                leader_run_id=leader_run_id,
                leader_tool_call_id=req.leader_tool_call_id.strip(),
                status="queued",
                instruction=req.instruction,
                created_at=_now(),
            )
            self._assigns[assign.assign_id] = assign
            self._put("workgroup_assigns", assign.assign_id, assign.model_dump_json(), workgroup_id=workgroup_id)
            return assign

    def get_assign(self, assign_id: str) -> Assign | None:
        with self._lock:
            self._ensure_loaded()
            return self._assigns.get(assign_id)

    def get_actor_run(self, run_id: str) -> ActorRun | None:
        with self._lock:
            self._ensure_loaded()
            return self._runs.get(run_id)

    def update_actor_run(
        self,
        run_id: str,
        *,
        status: str | None = None,
        timeline_watermark_seq: int | None = None,
        checkpoint_ordinal: int | None = None,
    ) -> ActorRun:
        with self._lock:
            self._ensure_loaded()
            run = self._runs.get(run_id)
            if run is None:
                raise WorkgroupError("not_found", "actor run not found", http_status=404)
            update: dict[str, Any] = {}
            if status is not None:
                update["status"] = status
            if timeline_watermark_seq is not None:
                update["timeline_watermark_seq"] = timeline_watermark_seq
            if checkpoint_ordinal is not None:
                update["checkpoint_ordinal"] = checkpoint_ordinal
            updated = run.model_copy(update=update)
            self._runs[run_id] = updated
            self._put("actor_runs", run_id, updated.model_dump_json(), workgroup_id=updated.workgroup_id)
            return updated

    def find_running_leader_run(self, workgroup_id: str) -> ActorRun | None:
        with self._lock:
            self._ensure_loaded()
            candidates = [
                r
                for r in self._runs.values()
                if r.workgroup_id == workgroup_id
                and r.actor_id == "leader"
                and r.status in {"running", "awaiting_hitl"}
            ]
            if not candidates:
                return None
            return sorted(candidates, key=lambda r: r.created_at)[-1]

    def get_run_history(self, run_id: str) -> ActorRunHistory | None:
        with self._lock:
            self._ensure_loaded()
            return self._run_histories.get(run_id)

    def ensure_run_history(self, run: ActorRun) -> ActorRunHistory:
        with self._lock:
            self._ensure_loaded()
            existing = self._run_histories.get(run.run_id)
            if existing is not None:
                return existing
            hist = ActorRunHistory(
                run_id=run.run_id,
                workgroup_id=run.workgroup_id,
                actor_id=run.actor_id,
                messages=[],
                timeline_watermark_seq=run.timeline_watermark_seq,
            )
            self._run_histories[run.run_id] = hist
            self._put(
                "actor_run_histories",
                run.run_id,
                hist.model_dump_json(),
                workgroup_id=run.workgroup_id,
            )
            return hist

    def append_run_history(
        self,
        run_id: str,
        messages: list[RunHistoryMessage] | list[dict[str, Any]],
        *,
        timeline_watermark_seq: int | None = None,
    ) -> ActorRunHistory:
        with self._lock:
            self._ensure_loaded()
            hist = self._run_histories.get(run_id)
            run = self._runs.get(run_id)
            if hist is None:
                if run is None:
                    raise WorkgroupError("not_found", "actor run not found", http_status=404)
                hist = ActorRunHistory(
                    run_id=run.run_id,
                    workgroup_id=run.workgroup_id,
                    actor_id=run.actor_id,
                    messages=[],
                    timeline_watermark_seq=run.timeline_watermark_seq,
                )
            added = [
                m if isinstance(m, RunHistoryMessage) else RunHistoryMessage.model_validate(m)
                for m in messages
            ]
            new_msgs = list(hist.messages) + added
            wm = hist.timeline_watermark_seq if timeline_watermark_seq is None else timeline_watermark_seq
            updated = hist.model_copy(update={"messages": new_msgs, "timeline_watermark_seq": wm})
            self._run_histories[run_id] = updated
            self._put(
                "actor_run_histories",
                run_id,
                updated.model_dump_json(),
                workgroup_id=updated.workgroup_id,
            )
            if run is not None and timeline_watermark_seq is not None:
                run2 = run.model_copy(
                    update={
                        "timeline_watermark_seq": timeline_watermark_seq,
                        "checkpoint_ordinal": run.checkpoint_ordinal + len(added),
                    }
                )
                self._runs[run_id] = run2
                self._put("actor_runs", run_id, run2.model_dump_json(), workgroup_id=run2.workgroup_id)
            return updated

    def active_grant_for_member(self, member_id: str) -> NodeExecutionGrant | None:
        """兼容旧数据；新产品路径不再依赖 Grant。"""
        with self._lock:
            self._ensure_loaded()
            for g in self._grants.values():
                if g.member_id == member_id and g.status == "accepted":
                    return g
            return None

    def member_execution_context(self, member_id: str) -> dict[str, Any]:
        """派活/provision 用的成员执行上下文（替代 ExecutionGrant）。"""
        with self._lock:
            self._ensure_loaded()
            member = self._members.get(member_id)
            if member is None:
                raise WorkgroupError("not_found", "member not found", http_status=404)
            spec = self._specs.get(member_id)
            runtime = dict(self._member_runtime.get(member_id) or {})
            lease_id = str(runtime.get("lease_id") or "").strip() or ids.lease_id()
            try:
                lease_epoch = int(runtime.get("lease_epoch") or 1)
            except (TypeError, ValueError):
                lease_epoch = 1
            if "lease_id" not in runtime or "lease_epoch" not in runtime:
                runtime["lease_id"] = lease_id
                runtime["lease_epoch"] = str(lease_epoch)
                self._member_runtime[member_id] = runtime
            return {
                "home_node_id": member.home_node_id,
                "member_spec_digest": member.member_spec_digest,
                "member_generation": member.member_generation,
                "lease_id": lease_id,
                "lease_epoch": lease_epoch,
                "tool_allow_names": list(spec.tools.allow_names) if spec is not None else [],
            }

    def mark_member_status(
        self,
        member_id: str,
        status: str,
        *,
        workgroup_id: str | None = None,
        workspace_path: str = "",
        tool_catalog_revision: str = "",
        provision_id: str = "",
    ) -> WorkGroupMember:
        with self._lock:
            self._ensure_loaded()
            member = self._members.get(member_id)
            if member is None:
                raise WorkgroupError("not_found", "member not found", http_status=404)
            if workgroup_id and member.workgroup_id != workgroup_id:
                raise WorkgroupError("not_found", "member not found", http_status=404)
            updated = member.model_copy(update={"status": status})
            self._members[member_id] = updated
            self._put(
                "workgroup_members",
                member_id,
                updated.model_dump_json(),
                workgroup_id=updated.workgroup_id,
            )
            runtime = dict(self._member_runtime.get(member_id) or {})
            if workspace_path:
                runtime["workspace_path"] = workspace_path
            if tool_catalog_revision:
                runtime["tool_catalog_revision"] = tool_catalog_revision
            if provision_id:
                runtime["provision_id"] = provision_id
            if runtime:
                self._member_runtime[member_id] = runtime
            return updated

    def member_runtime(self, member_id: str) -> dict[str, str]:
        with self._lock:
            self._ensure_loaded()
            return dict(self._member_runtime.get(member_id) or {})

    def set_assign_status(
        self,
        assign_id: str,
        status: str,
        *,
        result_summary: str | None = None,
        error_code: str | None = None,
    ) -> Assign:
        with self._lock:
            self._ensure_loaded()
            assign = self._assigns.get(assign_id)
            if assign is None:
                raise WorkgroupError("not_found", "assign not found", http_status=404)
            update: dict[str, Any] = {"status": status}
            if result_summary is not None:
                update["result_summary"] = result_summary
            if error_code is not None or status in {"succeeded", "failed", "indeterminate", "canceled"}:
                update["error_code"] = error_code
            updated = assign.model_copy(update=update)
            self._assigns[assign_id] = updated
            self._put(
                "workgroup_assigns",
                assign_id,
                updated.model_dump_json(),
                workgroup_id=updated.workgroup_id,
            )
            return updated

    # --- Timeline / Outbox / HITL (D3) ---

    def append_timeline(
        self,
        workgroup_id: str,
        *,
        type: str,
        actor_id: str,
        text: str = "",
        client_message_id: str | None = None,
        protocol_name: str | None = None,
        assign_id: str | None = None,
    ) -> TimelineEvent:
        with self._lock:
            self._ensure_loaded()
            if self.get_workgroup(workgroup_id) is None:
                raise WorkgroupError("not_found", "workgroup not found", http_status=404)
            events = self._timeline.setdefault(workgroup_id, [])
            if client_message_id:
                for ev in events:
                    if ev.client_message_id == client_message_id:
                        return ev
            seq = (events[-1].seq + 1) if events else 1
            pname = (protocol_name or "").strip() or protocol_name_for_actor(actor_id)
            event = TimelineEvent(
                event_id=ids.event_id(),
                workgroup_id=workgroup_id,
                seq=seq,
                type=type,  # type: ignore[arg-type]
                actor_id=actor_id,
                text=text,
                created_at=_now(),
                client_message_id=client_message_id,
                protocol_name=pname,
                assign_id=assign_id,
            )
            events.append(event)
            self._put(
                "workgroup_timeline",
                event.event_id,
                event.model_dump_json(),
                workgroup_id=workgroup_id,
            )
            return event

    def list_timeline(self, workgroup_id: str) -> list[TimelineEvent]:
        with self._lock:
            self._ensure_loaded()
            return list(self._timeline.get(workgroup_id) or [])

    def enqueue_outbox(
        self,
        workgroup_id: str,
        *,
        type: str,
        payload: dict[str, Any],
    ) -> OutboxFrame:
        with self._lock:
            self._ensure_loaded()
            if self.get_workgroup(workgroup_id) is None:
                raise WorkgroupError("not_found", "workgroup not found", http_status=404)
            frames = self._outbox.setdefault(workgroup_id, [])
            seq = (frames[-1].delivery_seq + 1) if frames else 1
            frame = OutboxFrame(
                delivery_seq=seq,
                workgroup_id=workgroup_id,
                type=type,
                payload=dict(payload),
                created_at=_now(),
                acked=False,
            )
            frames.append(frame)
            key = f"{workgroup_id}:{seq}"
            self._put(
                "workgroup_outbox",
                key,
                frame.model_dump_json(),
                workgroup_id=workgroup_id,
            )
            return frame

    def list_outbox(self, workgroup_id: str, *, unacked_only: bool = False) -> list[OutboxFrame]:
        with self._lock:
            self._ensure_loaded()
            frames = list(self._outbox.get(workgroup_id) or [])
            if unacked_only:
                return [f for f in frames if not f.acked]
            return frames

    def frames_after(self, workgroup_id: str, *, after_seq: int) -> list[OutboxFrame]:
        """resume gap-fill：返回 delivery_seq > after_seq 的帧（含已 ack，按序重放）。"""
        with self._lock:
            self._ensure_loaded()
            frames = list(self._outbox.get(workgroup_id) or [])
            return [f for f in frames if f.delivery_seq > after_seq]

    def ack_outbox(self, workgroup_id: str, delivery_seq: int) -> OutboxFrame:
        with self._lock:
            self._ensure_loaded()
            frames = self._outbox.get(workgroup_id) or []
            for i, frame in enumerate(frames):
                if frame.delivery_seq == delivery_seq:
                    updated = frame.model_copy(update={"acked": True})
                    frames[i] = updated
                    self._put(
                        "workgroup_outbox",
                        f"{workgroup_id}:{delivery_seq}",
                        updated.model_dump_json(),
                        workgroup_id=workgroup_id,
                    )
                    return updated
            raise WorkgroupError("not_found", "outbox frame not found", http_status=404)

    def create_hitl(self, workgroup_id: str, *, prompt: str) -> HITLRequest:
        with self._lock:
            self._ensure_loaded()
            if self.get_workgroup(workgroup_id) is None:
                raise WorkgroupError("not_found", "workgroup not found", http_status=404)
            hitl = HITLRequest(
                hitl_id=ids.hitl_id(),
                workgroup_id=workgroup_id,
                kind="information",
                prompt=prompt,
                status="pending",
                created_at=_now(),
            )
            self._hitl[hitl.hitl_id] = hitl
            self._put(
                "workgroup_hitl",
                hitl.hitl_id,
                hitl.model_dump_json(),
                workgroup_id=workgroup_id,
            )
            return hitl

    def get_hitl(self, hitl_id: str) -> HITLRequest | None:
        with self._lock:
            self._ensure_loaded()
            return self._hitl.get(hitl_id)

    def list_hitl(self, workgroup_id: str, *, pending_only: bool = False) -> list[HITLRequest]:
        with self._lock:
            self._ensure_loaded()
            items = [h for h in self._hitl.values() if h.workgroup_id == workgroup_id]
            if pending_only:
                items = [h for h in items if h.status == "pending"]
            return sorted(items, key=lambda h: h.created_at, reverse=True)

    def resolve_hitl_cas(
        self,
        workgroup_id: str,
        hitl_id: str,
        *,
        resolution: dict[str, Any],
    ) -> HITLRequest:
        with self._lock:
            self._ensure_loaded()
            hitl = self._hitl.get(hitl_id)
            if hitl is None or hitl.workgroup_id != workgroup_id:
                raise WorkgroupError("not_found", "hitl not found", http_status=404)
            if hitl.status == "resolved":
                raise WorkgroupError(
                    "already_resolved",
                    "hitl already resolved",
                    http_status=409,
                )
            updated = hitl.model_copy(
                update={
                    "status": "resolved",
                    "resolution": dict(resolution),
                    "resolved_at": _now(),
                }
            )
            self._hitl[hitl_id] = updated
            self._put(
                "workgroup_hitl",
                hitl_id,
                updated.model_dump_json(),
                workgroup_id=workgroup_id,
            )
            return updated

    def subscribe(self, workgroup_id: str, node_id: str) -> Subscription:
        with self._lock:
            self._ensure_loaded()
            if self.get_workgroup(workgroup_id) is None:
                raise WorkgroupError("not_found", "workgroup not found", http_status=404)
            self.assert_acl_member(workgroup_id, node_id)
            bucket = self._subscriptions.setdefault(workgroup_id, {})
            existing = bucket.get(node_id.strip())
            if existing is not None:
                return existing
            sub = Subscription(
                workgroup_id=workgroup_id,
                node_id=node_id.strip(),
                subscribed_at=_now(),
            )
            bucket[sub.node_id] = sub
            self._put(
                "workgroup_subscriptions",
                f"{workgroup_id}:{sub.node_id}",
                sub.model_dump_json(),
                workgroup_id=workgroup_id,
            )
            return sub

    def unsubscribe(self, workgroup_id: str, node_id: str) -> None:
        with self._lock:
            self._ensure_loaded()
            bucket = self._subscriptions.get(workgroup_id) or {}
            nid = node_id.strip()
            if nid in bucket:
                del bucket[nid]
                self._delete("workgroup_subscriptions", f"{workgroup_id}:{nid}")

    def list_subscribers(self, workgroup_id: str) -> list[Subscription]:
        with self._lock:
            self._ensure_loaded()
            return sorted(
                (self._subscriptions.get(workgroup_id) or {}).values(),
                key=lambda s: s.subscribed_at,
            )

    def is_subscribed(self, workgroup_id: str, node_id: str) -> bool:
        with self._lock:
            self._ensure_loaded()
            return node_id.strip() in (self._subscriptions.get(workgroup_id) or {})

    def bump_lease_epochs(self, workgroup_id: str) -> int:
        """归档时抬升组内成员 lease_epoch；返回抬升后的最大 epoch。"""
        with self._lock:
            self._ensure_loaded()
            max_epoch = 1
            for mid, member in list(self._members.items()):
                if member.workgroup_id != workgroup_id:
                    continue
                runtime = dict(self._member_runtime.get(mid) or {})
                try:
                    cur = int(runtime.get("lease_epoch") or 1)
                except (TypeError, ValueError):
                    cur = 1
                new_epoch = cur + 1
                runtime["lease_epoch"] = str(new_epoch)
                if not runtime.get("lease_id"):
                    runtime["lease_id"] = ids.lease_id()
                self._member_runtime[mid] = runtime
                if new_epoch > max_epoch:
                    max_epoch = new_epoch
            return max_epoch
