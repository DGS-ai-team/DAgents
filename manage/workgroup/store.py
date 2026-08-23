"""WorkGroup 存储（内存 + SQLite payload_json）。"""

from __future__ import annotations

import logging
import threading
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Callable

from manage.storage.sqlite import SQLiteDatabase
from manage.workgroup.digest import sha256_digest
from manage.workgroup.errors import WorkgroupError
from manage.workgroup import ids
from manage.workgroup.d3_models import (
    HITLRequest,
    OutboxFrame,
    QueuedHumanRecord,
    Subscription,
    TimelineEvent,
    TurnCheckpoint,
)
from manage.workgroup.history import ActorRunHistory, RunHistoryMessage
from manage.workgroup.context_compression import ActorContextSnapshot
from manage.workgroup.protocol_names import protocol_name_for_actor
from manage.workgroup.models import (
    ActorRun,
    ActorRunCreateRequest,
    Assign,
    AssignCreateRequest,
    ACLPatchRequest,
    MemberCreateRequest,
    MemberPatchRequest,
    MemberSpec,
    MemberTools,
    MemberWorkspace,
    WorkGroup,
    WorkGroupACL,
    WorkGroupCreateRequest,
    WorkGroupMember,
    WorkGroupPatchRequest,
    WorkgroupWorkspace,
)
from manage.workgroup.workspace import materialize_workgroup_workspace


def _now() -> str:
    return datetime.now(timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z")


def _acl_contains(acl: WorkGroupACL, node_id: str) -> bool:
    return node_id in acl.owners or node_id in acl.collaborators


class WorkGroupStore:
    def __init__(
        self,
        db: SQLiteDatabase | None = None,
        *,
        workspaces_dir: Path | None = None,
    ) -> None:
        self._db = db if (db and db.enabled) else None
        self._workspaces_dir = workspaces_dir
        self._lock = threading.RLock()
        self._groups: dict[str, WorkGroup] = {}
        self._acls: dict[str, WorkGroupACL] = {}
        self._members: dict[str, WorkGroupMember] = {}
        self._specs: dict[str, MemberSpec] = {}
        self._assigns: dict[str, Assign] = {}
        self._runs: dict[str, ActorRun] = {}
        self._run_histories: dict[str, ActorRunHistory] = {}
        self._context_snapshots: dict[str, ActorContextSnapshot] = {}
        self._timeline: dict[str, list[TimelineEvent]] = {}
        self._outbox: dict[str, list[OutboxFrame]] = {}
        self._hitl: dict[str, HITLRequest] = {}
        self._hitl_waiters: dict[str, threading.Event] = {}
        self._hitl_waiting: set[str] = set()
        self._human_queue: dict[str, list[QueuedHumanRecord]] = {}
        self._turn_checkpoints: dict[str, TurnCheckpoint] = {}
        # member_id → {workspace_path, tool_catalog_revision, provision_id}
        self._member_runtime: dict[str, dict[str, str]] = {}
        # workgroup_id → {workspace_path} 组共享工作区（与 WorkGroup.workspace.path 同步）
        self._workgroup_runtime: dict[str, dict[str, str]] = {}
        # workgroup_id → {node_id: Subscription}
        self._subscriptions: dict[str, dict[str, Subscription]] = {}
        self._timeline_listener: Callable[[TimelineEvent], None] | None = None

    def set_timeline_listener(self, listener: Callable[[TimelineEvent], None] | None) -> None:
        """Register a best-effort listener after Timeline + outbox commit."""
        with self._lock:
            self._timeline_listener = listener

    # --- low-level persistence ---

    def _put(
        self,
        table: str,
        key: str,
        payload: str,
        workgroup_id: str | None = None,
        conn: Any | None = None,
    ) -> None:
        if self._db is None:
            return
        if conn is not None:
            self._put_row(conn, table, key, payload, workgroup_id)
            return
        with self._db.connect() as tx:
            self._put_row(tx, table, key, payload, workgroup_id)
            tx.commit()

    def _put_row(self, conn: Any, table: str, key: str, payload: str, workgroup_id: str | None = None) -> None:
        if workgroup_id is None:
            conn.execute(
                f"INSERT INTO {table}(id,payload_json) VALUES(?,?) "
                f"ON CONFLICT(id) DO UPDATE SET payload_json=excluded.payload_json",
                (key, payload),
            )
            return
        conn.execute(
            f"INSERT INTO {table}(id,workgroup_id,payload_json) VALUES(?,?,?) "
            f"ON CONFLICT(id) DO UPDATE SET workgroup_id=excluded.workgroup_id, "
            f"payload_json=excluded.payload_json",
            (key, workgroup_id, payload),
        )

    def _delete(self, table: str, key: str, conn: Any | None = None) -> None:
        if self._db is None:
            return
        if conn is not None:
            conn.execute(f"DELETE FROM {table} WHERE id=?", (key,))
            return
        with self._db.connect() as tx:
            tx.execute(f"DELETE FROM {table} WHERE id=?", (key,))
            tx.commit()

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
            path = (g.workspace.path or "").strip()
            if path:
                self._workgroup_runtime[g.workgroup_id] = {"workspace_path": path}
        for a in self._load_all("workgroup_acls", WorkGroupACL):
            self._acls[a.workgroup_id] = a
        for m in self._load_all("workgroup_members", WorkGroupMember):
            self._members[m.member_id] = m
        for s in self._load_all("member_specs", MemberSpec):
            self._specs[s.member_id] = s
        for a in self._load_all("workgroup_assigns", Assign):
            self._assigns[a.assign_id] = a
        for r in self._load_all("actor_runs", ActorRun):
            self._runs[r.run_id] = r
        for h in self._load_all("actor_run_histories", ActorRunHistory):
            self._run_histories[h.run_id] = h
        for snapshot in self._load_all("actor_context_snapshots", ActorContextSnapshot):
            self._context_snapshots[snapshot.run_id] = snapshot
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
        for item in self._load_all("workgroup_human_queue", QueuedHumanRecord):
            self._human_queue.setdefault(item.workgroup_id, []).append(item)
        for wg, items in self._human_queue.items():
            self._human_queue[wg] = sorted(
                items,
                key=lambda item: (-int(item.priority or 0), item.created_at, item.queue_id),
            )
        for checkpoint in self._load_all("workgroup_turn_checkpoints", TurnCheckpoint):
            self._turn_checkpoints[checkpoint.workgroup_id] = checkpoint
        for sub in self._load_all("workgroup_subscriptions", Subscription):
            self._subscriptions.setdefault(sub.workgroup_id, {})[sub.node_id] = sub

    # --- WorkGroup ---

    def create_workgroup(self, req: WorkGroupCreateRequest) -> tuple[WorkGroup, WorkGroupACL]:
        with self._lock:
            self._ensure_loaded()
            wid = ids.workgroup_id()
            now = _now()
            workspace = WorkgroupWorkspace()
            if self._workspaces_dir is not None:
                ws_path = materialize_workgroup_workspace(self._workspaces_dir, wid)
                workspace = WorkgroupWorkspace(path=str(ws_path))
            group = WorkGroup(
                workgroup_id=wid,
                display_name=req.display_name.strip(),
                status="configuring",
                created_by_node_id=req.created_by_node_id.strip(),
                llm_profile_id=req.llm_profile_id.strip(),
                llm_profile_revision=req.llm_profile_revision.strip(),
                workspace=workspace,
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
            if workspace.path:
                self._workgroup_runtime[wid] = {"workspace_path": workspace.path}
            sub = Subscription(
                workgroup_id=wid,
                node_id=group.created_by_node_id,
                subscribed_at=now,
            )
            self._subscriptions.setdefault(wid, {})[sub.node_id] = sub
            if self._db is None:
                self._put("workgroups", wid, group.model_dump_json())
                self._put("workgroup_acls", wid, acl.model_dump_json(), workgroup_id=wid)
                self._put(
                    "workgroup_subscriptions",
                    f"{wid}:{sub.node_id}",
                    sub.model_dump_json(),
                    workgroup_id=wid,
                )
                return group, acl
            with self._db.connect() as tx:
                self._put("workgroups", wid, group.model_dump_json(), conn=tx)
                self._put("workgroup_acls", wid, acl.model_dump_json(), workgroup_id=wid, conn=tx)
                self._put(
                    "workgroup_subscriptions",
                    f"{wid}:{sub.node_id}",
                    sub.model_dump_json(),
                    workgroup_id=wid,
                    conn=tx,
                )
                tx.commit()
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
            # 新进 ACL 的节点自动订阅，便于后续作为 Home 收 provision / Dialer resume
            for nid in owners + collaborators:
                self._subscribe_unlocked(workgroup_id, nid)
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
            agent_id = (req.agent_id or "").strip() or None
            if agent_id and not home:
                raise WorkgroupError(
                    "schema_mismatch",
                    "home_node_id is required until registry lookup is enabled",
                    http_status=400,
                )
            execution_mode = "agent_ref" if agent_id else "legacy_member"
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
                "agent_id": agent_id,
                "home_node_id": home,
                "display_name": req.display_name.strip(),
                "description": (req.description or "").strip(),
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
                agent_id=agent_id,
                session_id=(f"wg:{workgroup_id}:member:{mid}" if agent_id else None),
                execution_mode=execution_mode,
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
            if self._db is None:
                self._put("member_specs", mid, spec.model_dump_json(), workgroup_id=workgroup_id)
                self._put("workgroup_members", mid, member.model_dump_json(), workgroup_id=workgroup_id)
                self._subscribe_unlocked(workgroup_id, home)
                return member, spec
            with self._db.connect() as tx:
                self._put("member_specs", mid, spec.model_dump_json(), workgroup_id=workgroup_id, conn=tx)
                self._put("workgroup_members", mid, member.model_dump_json(), workgroup_id=workgroup_id, conn=tx)
                tx.commit()
            # Home Node 自动订阅：Dialer resume 才能拉到 pending member.provision
            self._subscribe_unlocked(workgroup_id, home)
            return member, spec

    def update_member(
        self, workgroup_id: str, member_id: str, req: MemberPatchRequest
    ) -> tuple[WorkGroupMember, MemberSpec]:
        """更新 MemberSpec：bump generation、重算 digest，状态回到 provisioning。"""
        with self._lock:
            self.require_mutable(workgroup_id)
            member = self._members.get(member_id)
            if member is None or member.workgroup_id != workgroup_id:
                raise WorkgroupError("not_found", "member not found", http_status=404)
            if member.status == "archived":
                raise WorkgroupError("conflict", "member is archived", http_status=409)
            spec = self._specs.get(member_id)
            if spec is None:
                raise WorkgroupError("not_found", "member spec not found", http_status=404)

            display_name = (
                req.display_name.strip() if req.display_name is not None else spec.display_name
            )
            description = (
                req.description.strip() if req.description is not None else spec.description
            )
            llm_profile_id = (
                req.llm_profile_id.strip() if req.llm_profile_id is not None else spec.llm_profile_id
            )
            llm_profile_revision = (
                req.llm_profile_revision.strip()
                if req.llm_profile_revision is not None
                else spec.llm_profile_revision
            )
            max_tool_loops = (
                int(req.max_tool_loops) if req.max_tool_loops is not None else spec.max_tool_loops
            )
            prompt = req.prompt if req.prompt is not None else spec.prompt
            allow_names = (
                list(req.allow_tool_names)
                if req.allow_tool_names is not None
                else list(spec.tools.allow_names)
            )
            side_effect_classes = (
                list(req.side_effect_classes)
                if req.side_effect_classes is not None
                else list(spec.tools.side_effect_classes)
            )
            policy_ceiling = (
                dict(req.policy_ceiling)
                if req.policy_ceiling is not None
                else dict(spec.policy_ceiling)
            )
            new_gen = int(member.member_generation) + 1
            tools = MemberTools(allow_names=allow_names, side_effect_classes=side_effect_classes)
            draft = {
                "member_id": member_id,
                "workgroup_id": workgroup_id,
				"agent_id": member.agent_id,
                "home_node_id": member.home_node_id,
                "display_name": display_name,
                "description": description,
                "member_generation": new_gen,
                "llm_profile_id": llm_profile_id,
                "llm_profile_revision": llm_profile_revision,
                "max_tool_loops": max_tool_loops,
                "prompt": prompt.model_dump(),
                "memory": spec.memory.model_dump(),
                "tools": tools.model_dump(),
                "policy_ceiling": policy_ceiling,
                "workspace": MemberWorkspace().model_dump(),
                "skills": "disabled",
                "hooks": "disabled",
            }
            digest = sha256_digest(draft)
            new_spec = MemberSpec.model_validate({**draft, "digest": digest})
            updated = member.model_copy(
                update={
                    "display_name": display_name,
                    "member_generation": new_gen,
                    "member_spec_digest": digest,
                    "status": "provisioning",
                }
            )
            self._specs[member_id] = new_spec
            self._members[member_id] = updated
            self._put("member_specs", member_id, new_spec.model_dump_json(), workgroup_id=workgroup_id)
            self._put(
                "workgroup_members", member_id, updated.model_dump_json(), workgroup_id=workgroup_id
            )
            return updated, new_spec

    def _subscribe_unlocked(self, workgroup_id: str, node_id: str) -> Subscription | None:
        """调用方须已持有 self._lock；node 须已在 ACL 内。"""
        nid = (node_id or "").strip()
        if not nid:
            return None
        if self.get_workgroup(workgroup_id) is None:
            return None
        try:
            self.assert_acl_member(workgroup_id, nid)
        except WorkgroupError:
            return None
        bucket = self._subscriptions.setdefault(workgroup_id, {})
        existing = bucket.get(nid)
        if existing is not None:
            return existing
        sub = Subscription(
            workgroup_id=workgroup_id,
            node_id=nid,
            subscribed_at=_now(),
        )
        bucket[nid] = sub
        self._put(
            "workgroup_subscriptions",
            f"{workgroup_id}:{nid}",
            sub.model_dump_json(),
            workgroup_id=workgroup_id,
        )
        return sub

    def ensure_member_homes_subscribed(self, workgroup_id: str) -> list[str]:
        """确保各成员 Home Node 已订阅（Dialer resume 依赖订阅列表）。"""
        with self._lock:
            self._ensure_loaded()
            if self.get_workgroup(workgroup_id) is None:
                raise WorkgroupError("not_found", "workgroup not found", http_status=404)
            added: list[str] = []
            for m in self._members.values():
                if m.workgroup_id != workgroup_id:
                    continue
                if m.status in {"archived"}:
                    continue
                home = (m.home_node_id or "").strip()
                if not home:
                    continue
                before = home in (self._subscriptions.get(workgroup_id) or {})
                self._subscribe_unlocked(workgroup_id, home)
                if not before and home in (self._subscriptions.get(workgroup_id) or {}):
                    added.append(home)
            return added

    def get_member(self, member_id: str) -> WorkGroupMember | None:
        with self._lock:
            self._ensure_loaded()
            return self._members.get(member_id)

    def get_spec(self, member_id: str) -> MemberSpec | None:
        with self._lock:
            self._ensure_loaded()
            return self._specs.get(member_id)

    def list_members(self, workgroup_id: str, *, include_archived: bool = False) -> list[WorkGroupMember]:
        with self._lock:
            self._ensure_loaded()
            items = [m for m in self._members.values() if m.workgroup_id == workgroup_id]
            if not include_archived:
                items = [m for m in items if m.status != "archived"]
            return sorted(items, key=lambda m: m.created_at)

    def archive_member(self, workgroup_id: str, member_id: str) -> WorkGroupMember:
        with self._lock:
            self._ensure_loaded()
            if self.get_workgroup(workgroup_id) is None:
                raise WorkgroupError("not_found", "workgroup not found", http_status=404)
            member = self._members.get(member_id)
            if member is None or member.workgroup_id != workgroup_id:
                raise WorkgroupError("not_found", "member not found", http_status=404)
            if member.status == "archived":
                return member
        return self.mark_member_status(member_id, "archived", workgroup_id=workgroup_id)

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
            # P1：不同成员可以并行执行，但同一成员仍保持单飞，避免
            # 两个 LLM turn 同时修改同一个成员会话/工作区。
            active = [
                a
                for a in self._assigns.values()
                if a.workgroup_id == workgroup_id
                and a.member_id == member.member_id
                and a.status in {"queued", "running", "awaiting_hitl"}
            ]
            if active:
                raise WorkgroupError(
                    "conflict",
                    "member already has an active assign",
                    http_status=409,
                    details={"active_assign_id": active[0].assign_id},
                )
            if req.leader_run_id is None:
                req = req.model_copy(
                    update={
                        "leader_run_id": self.get_or_create_actor_session(
                            workgroup_id,
                            actor_id="leader",
                            llm_profile_revision=self._groups[workgroup_id].llm_profile_revision,
                        ).run_id
                    }
                )
            leader_run_id = req.leader_run_id or ids.run_id()
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
            # Keep the member occupancy durable from the moment the assign is
            # created.  A queued assign must not race with another assign for
            # the same member, even if its Home Node is temporarily offline.
            if member.active_assign_id != assign.assign_id:
                member_update: dict[str, Any] = {"active_assign_id": assign.assign_id}
                if member.status == "ready":
                    member_update["status"] = "busy"
                occupied = member.model_copy(update=member_update)
                self._members[member.member_id] = occupied
                self._put(
                    "workgroup_members",
                    member.member_id,
                    occupied.model_dump_json(),
                    workgroup_id=workgroup_id,
                )
            return assign

    def get_assign(self, assign_id: str) -> Assign | None:
        with self._lock:
            self._ensure_loaded()
            return self._assigns.get(assign_id)

    def list_assigns(self, workgroup_id: str, *, active_only: bool = False) -> list[Assign]:
        with self._lock:
            self._ensure_loaded()
            rows = [a for a in self._assigns.values() if a.workgroup_id == workgroup_id]
            if active_only:
                rows = [a for a in rows if a.status in {"queued", "running", "awaiting_hitl"}]
            return sorted(rows, key=lambda a: (a.created_at, a.assign_id))

    def get_actor_run(self, run_id: str) -> ActorRun | None:
        with self._lock:
            self._ensure_loaded()
            return self._runs.get(run_id)

    def list_actor_runs(
        self,
        workgroup_id: str,
        *,
        actor_id: str | None = None,
        limit: int = 20,
    ) -> list[ActorRun]:
        with self._lock:
            self._ensure_loaded()
            rows = [r for r in self._runs.values() if r.workgroup_id == workgroup_id]
            aid = str(actor_id or "").strip()
            if aid:
                rows = [r for r in rows if r.actor_id == aid]
            if aid == "leader" and rows:
                # Supervisor is one persistent session. Legacy duplicate rows
                # remain addressable by run id, but are not listed as sessions.
                canonical = self._canonical_actor_run_unlocked(
                    workgroup_id,
                    actor_id="leader",
                )
                if canonical is not None:
                    self._consolidate_actor_session_history_unlocked(
                        workgroup_id,
                        actor_id="leader",
                        target=canonical,
                    )
                    canonical = self._runs.get(canonical.run_id) or canonical
                rows = [canonical] if canonical is not None else []
            elif not aid:
                leader_rows = [row for row in rows if row.actor_id == "leader"]
                if leader_rows:
                    canonical = self._canonical_actor_run_unlocked(
                        workgroup_id,
                        actor_id="leader",
                    )
                    if canonical is not None:
                        self._consolidate_actor_session_history_unlocked(
                            workgroup_id,
                            actor_id="leader",
                            target=canonical,
                        )
                        canonical = self._runs.get(canonical.run_id) or canonical
                        rows = [row for row in rows if row.actor_id != "leader"]
                        rows.append(canonical)
            rows.sort(key=lambda r: r.created_at, reverse=True)
            lim = max(1, min(int(limit or 20), 100))
            return rows[:lim]

    def _canonical_actor_run_unlocked(
        self,
        workgroup_id: str,
        *,
        actor_id: str,
    ) -> ActorRun | None:
        rows = [
            r
            for r in self._runs.values()
            if r.workgroup_id == workgroup_id and r.actor_id == actor_id
        ]
        if not rows:
            return None
        rows.sort(key=lambda r: (r.created_at, r.run_id))
        if actor_id != "leader":
            return rows[-1]
        # Pre-fix direct mentions could create an empty leader placeholder.
        # Prefer the oldest row that has real history, otherwise the oldest row.
        with_history = [
            r
            for r in rows
            if self._run_histories.get(r.run_id) is not None
            and bool(self._run_histories[r.run_id].messages)
        ]
        return (with_history or rows)[0]

    def find_latest_actor_run(self, workgroup_id: str, *, actor_id: str) -> ActorRun | None:
        """Return the persistent session for an actor, if one exists.

        ActorRun is the on-disk session record in the current D0.5 model.  A
        completed run remains reusable; only an active run is considered busy
        by TurnKernel's turn gate.
        """
        with self._lock:
            self._ensure_loaded()
            aid = str(actor_id or "").strip()
            rows = [
                r
                for r in self._runs.values()
                if r.workgroup_id == workgroup_id and r.actor_id == aid
            ]
            if not rows:
                return None
            rows.sort(key=lambda r: (r.created_at, r.run_id), reverse=True)
            return rows[0]

    def get_or_create_actor_session(
        self,
        workgroup_id: str,
        *,
        actor_id: str,
        llm_profile_revision: str | None = None,
    ) -> ActorRun:
        """Get the workgroup-local persistent session for one actor."""
        with self._lock:
            self._ensure_loaded()
            existing = self._canonical_actor_run_unlocked(workgroup_id, actor_id=actor_id)
            if existing is not None:
                self._consolidate_actor_session_history_unlocked(
                    workgroup_id,
                    actor_id=actor_id,
                    target=existing,
                )
                return existing
            return self.create_actor_run(
                workgroup_id,
                ActorRunCreateRequest(
                    actor_id=actor_id,
                    llm_profile_revision=llm_profile_revision,
                ),
            )

    def _consolidate_actor_session_history_unlocked(
        self,
        workgroup_id: str,
        *,
        actor_id: str,
        target: ActorRun,
    ) -> None:
        """Lazily merge pre-session ActorRuns into the persistent session.

        Older builds created one ActorRun per turn.  Keeping those records is
        useful for audit, but the next persistent session must see their full
        message sequence.  This migration is idempotent because the merged
        sequence is written to the target history as one snapshot.
        """
        rows = [
            r
            for r in self._runs.values()
            if r.workgroup_id == workgroup_id and r.actor_id == str(actor_id or "").strip()
        ]
        rows.sort(key=lambda r: (r.created_at, r.run_id))
        current = self._run_histories.get(target.run_id)
        merged: list[RunHistoryMessage] = []
        max_watermark = 0
        fingerprints: set[str] = set()

        def add_message(message: RunHistoryMessage) -> None:
            if message.timeline_event_seq is not None and any(
                existing.timeline_event_seq == message.timeline_event_seq
                for existing in merged
            ):
                return
            if message.assign_id and any(
                existing.assign_id == message.assign_id and existing.role == message.role
                for existing in merged
            ):
                return
            fingerprint = message.model_dump_json()
            if fingerprint in fingerprints:
                return
            fingerprints.add(fingerprint)
            merged.append(message)

        for row in rows:
            history = self._run_histories.get(row.run_id)
            if history is not None:
                for message in history.messages:
                    add_message(message)
                max_watermark = max(max_watermark, int(history.timeline_watermark_seq or 0))
            max_watermark = max(max_watermark, int(row.timeline_watermark_seq or 0))

        if actor_id == "leader":
            timeline = sorted(
                self._timeline.get(workgroup_id, []),
                key=lambda event: event.seq,
            )
            from manage.workgroup.history import extract_assign_ids_from_tool_results

            covered_assigns = {
                message.assign_id
                for message in merged
                if message.assign_id
            }
            covered_assigns.update(extract_assign_ids_from_tool_results(merged))
            for event in timeline:
                if event.type == "human_message":
                    if any(
                        existing.timeline_event_seq == event.seq
                        or (
                            existing.timeline_event_seq is None
                            and existing.role == "user"
                            and existing.name == event.protocol_name
                            and existing.content == event.text
                        )
                        for existing in merged
                    ):
                        continue
                    add_message(
                        RunHistoryMessage(
                            role="user",
                            name=event.protocol_name,
                            content=event.text,
                            timeline_event_seq=event.seq,
                        )
                    )
                    continue
                # Direct @member turns bypass Supervisor's tool call. Their
                # member result must still be visible to future Supervisor
                # turns; regular assignments already have a tool result.
                if (
                    event.type == "actor_final_text"
                    and event.actor_id != "leader"
                    and event.assign_id
                    and event.assign_id not in covered_assigns
                ):
                    add_message(
                        RunHistoryMessage(
                            role="user",
                            name=event.protocol_name,
                            content=event.text,
                            timeline_event_seq=event.seq,
                            assign_id=event.assign_id,
                        )
                    )
                    covered_assigns.add(event.assign_id)
            max_watermark = max(max_watermark, max((event.seq for event in timeline), default=0))

        updated_history = ActorRunHistory(
            run_id=target.run_id,
            workgroup_id=workgroup_id,
            actor_id=target.actor_id,
            messages=merged,
            timeline_watermark_seq=max_watermark,
            legacy_runs_consolidated=True,
        )
        if current is not None and current.model_dump() == updated_history.model_dump():
            return
        self._run_histories[target.run_id] = updated_history
        self._put(
            "actor_run_histories",
            target.run_id,
            updated_history.model_dump_json(),
            workgroup_id=workgroup_id,
        )
        updated_run = target.model_copy(
            update={
                "timeline_watermark_seq": max_watermark,
                "checkpoint_ordinal": max(target.checkpoint_ordinal, len(merged)),
            }
        )
        self._runs[target.run_id] = updated_run
        self._put(
            "actor_runs",
            target.run_id,
            updated_run.model_dump_json(),
            workgroup_id=workgroup_id,
        )

    def prepare_actor_session(
        self,
        run_id: str,
        *,
        assign_id: str | None = None,
    ) -> ActorRun:
        """Reopen a persistent actor session for the next Turn/Assign."""
        with self._lock:
            self._ensure_loaded()
            run = self._runs.get(run_id)
            if run is None:
                raise WorkgroupError("not_found", "actor run not found", http_status=404)
            updated = run.model_copy(update={"status": "running", "assign_id": assign_id})
            self._runs[run_id] = updated
            self._put("actor_runs", run_id, updated.model_dump_json(), workgroup_id=updated.workgroup_id)
            return updated

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

    def get_context_snapshot(self, run_id: str) -> ActorContextSnapshot | None:
        with self._lock:
            self._ensure_loaded()
            return self._context_snapshots.get(run_id)

    def save_context_snapshot(self, snapshot: ActorContextSnapshot) -> ActorContextSnapshot:
        with self._lock:
            self._ensure_loaded()
            self._context_snapshots[snapshot.run_id] = snapshot
            self._put(
                "actor_context_snapshots",
                snapshot.run_id,
                snapshot.model_dump_json(),
                workgroup_id=snapshot.workgroup_id,
            )
            return snapshot

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
            updated = hist.model_copy(
                update={
                    "messages": new_msgs,
                    "timeline_watermark_seq": wm,
                    "legacy_runs_consolidated": True,
                }
            )
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
				"agent_id": member.agent_id or "",
				"session_id": member.session_id or "",
				"execution_mode": member.execution_mode,
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
        error_code: str | None = None,
        error_message: str | None = None,
    ) -> WorkGroupMember:
        with self._lock:
            self._ensure_loaded()
            member = self._members.get(member_id)
            if member is None:
                raise WorkgroupError("not_found", "member not found", http_status=404)
            if workgroup_id and member.workgroup_id != workgroup_id:
                raise WorkgroupError("not_found", "member not found", http_status=404)
            # A provision result may arrive after the member was archived,
            # especially when the Home Node replays an unacked provision
            # frame during WS resume.  Archive is terminal; a late result
            # must never resurrect the member.
            if member.status == "archived" and status != "archived":
                return member
            update: dict[str, Any] = {"status": status}
            if status == "error":
                update["error_code"] = (error_code or "").strip() or None
                update["error_message"] = (error_message or "").strip() or None
            elif status == "ready":
                update["error_code"] = None
                update["error_message"] = None
            updated = member.model_copy(update=update)
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

    def workgroup_runtime(self, workgroup_id: str) -> dict[str, str]:
        with self._lock:
            self._ensure_loaded()
            group = self._groups.get(workgroup_id)
            if group is not None and (group.workspace.path or "").strip():
                return {"workspace_path": group.workspace.path.strip()}
            return dict(self._workgroup_runtime.get(workgroup_id) or {})

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
            if status in {"succeeded", "failed", "indeterminate", "canceled"}:
                member = self._members.get(updated.member_id)
                if member is not None and member.active_assign_id == updated.assign_id:
                    member_status = "ready" if member.status not in {"archived", "error"} else member.status
                    released = member.model_copy(
                        update={"active_assign_id": None, "status": member_status}
                    )
                    self._members[member.member_id] = released
                    self._put(
                        "workgroup_members",
                        member.member_id,
                        released.model_dump_json(),
                        workgroup_id=member.workgroup_id,
                    )
            return updated

    # --- Turn recovery / human queue persistence ---

    def list_human_queue_records(self, workgroup_id: str) -> list[QueuedHumanRecord]:
        with self._lock:
            self._ensure_loaded()
            return list(self._human_queue.get(workgroup_id) or [])

    def list_human_queue_workgroups(self) -> list[str]:
        with self._lock:
            self._ensure_loaded()
            return list(self._human_queue.keys())

    def save_human_queue_record(self, record: QueuedHumanRecord) -> QueuedHumanRecord:
        with self._lock:
            self._ensure_loaded()
            if self.get_workgroup(record.workgroup_id) is None:
                raise WorkgroupError("not_found", "workgroup not found", http_status=404)
            bucket = self._human_queue.setdefault(record.workgroup_id, [])
            for index, existing in enumerate(bucket):
                if existing.queue_id == record.queue_id:
                    bucket[index] = record
                    break
            else:
                bucket.append(record)
            bucket.sort(
                key=lambda item: (-int(item.priority or 0), item.created_at, item.queue_id)
            )
            self._put(
                "workgroup_human_queue",
                record.queue_id,
                record.model_dump_json(),
                workgroup_id=record.workgroup_id,
            )
            return record

    def delete_human_queue_record(self, workgroup_id: str, queue_id: str) -> None:
        with self._lock:
            self._ensure_loaded()
            bucket = self._human_queue.get(workgroup_id) or []
            bucket = [item for item in bucket if item.queue_id != queue_id]
            if bucket:
                self._human_queue[workgroup_id] = bucket
            else:
                self._human_queue.pop(workgroup_id, None)
            self._delete("workgroup_human_queue", queue_id)

    def save_turn_checkpoint(self, checkpoint: TurnCheckpoint) -> TurnCheckpoint:
        with self._lock:
            self._ensure_loaded()
            if self.get_workgroup(checkpoint.workgroup_id) is None:
                raise WorkgroupError("not_found", "workgroup not found", http_status=404)
            self._turn_checkpoints[checkpoint.workgroup_id] = checkpoint
            self._put(
                "workgroup_turn_checkpoints",
                checkpoint.workgroup_id,
                checkpoint.model_dump_json(),
                workgroup_id=checkpoint.workgroup_id,
            )
            return checkpoint

    def get_turn_checkpoint(self, workgroup_id: str) -> TurnCheckpoint | None:
        with self._lock:
            self._ensure_loaded()
            return self._turn_checkpoints.get(workgroup_id)

    def clear_turn_checkpoint(self, workgroup_id: str) -> None:
        with self._lock:
            self._ensure_loaded()
            self._turn_checkpoints.pop(workgroup_id, None)
            self._delete("workgroup_turn_checkpoints", workgroup_id)

    def reconcile_inflight_runs(self) -> dict[str, Any]:
        """Fence process-local work after a Manage restart.

        ActorRun/Assign records are durable, but their worker threads and
        command waiters are not.  Never leave those records looking active:
        mark them indeterminate and release the member lease.  Pending HITL
        rows are intentionally preserved for an explicit user decision.
        """
        with self._lock:
            self._ensure_loaded()
            run_ids: list[str] = []
            assign_ids: list[str] = []
            hitl_recovery_run_ids: set[str] = set()
            for hitl in self._hitl.values():
                if not hitl.run_id or not hitl.tool_call_id:
                    continue
                if hitl.status == "pending":
                    hitl_recovery_run_ids.add(str(hitl.run_id))
                    continue
                # A resolved HITL may have been committed just before the
                # process died, before its synthetic tool result was appended.
                # Keep that run recoverable; the kernel will append the result
                # idempotently and continue it on startup.
                history = self._run_histories.get(str(hitl.run_id))
                has_result = bool(
                    history
                    and any(
                        message.role == "tool"
                        and message.tool_call_id == hitl.tool_call_id
                        for message in history.messages
                    )
                )
                if hitl.status == "resolved" and not has_result:
                    hitl_recovery_run_ids.add(str(hitl.run_id))
            checkpoint_ids = list(self._turn_checkpoints.keys())
            for workgroup_id in checkpoint_ids:
                self._turn_checkpoints.pop(workgroup_id, None)
                self._delete("workgroup_turn_checkpoints", workgroup_id)
            for run in list(self._runs.values()):
                if run.status not in {"running", "awaiting_hitl"}:
                    continue
                if run.run_id in hitl_recovery_run_ids:
                    if run.status != "awaiting_hitl":
                        waiting = run.model_copy(update={"status": "awaiting_hitl"})
                        self._runs[run.run_id] = waiting
                        self._put(
                            "actor_runs",
                            run.run_id,
                            waiting.model_dump_json(),
                            workgroup_id=run.workgroup_id,
                        )
                    continue
                updated = run.model_copy(update={"status": "indeterminate"})
                self._runs[run.run_id] = updated
                self._put(
                    "actor_runs",
                    run.run_id,
                    updated.model_dump_json(),
                    workgroup_id=run.workgroup_id,
                )
                run_ids.append(run.run_id)
            for member in list(self._members.values()):
                if member.status != "busy":
                    continue
                active = self._assigns.get(member.active_assign_id or "")
                if active is not None and active.status in {"queued", "running", "awaiting_hitl"}:
                    continue
                released = member.model_copy(update={"active_assign_id": None, "status": "ready"})
                self._members[member.member_id] = released
                self._put(
                    "workgroup_members",
                    member.member_id,
                    released.model_dump_json(),
                    workgroup_id=member.workgroup_id,
                )
            for assign in list(self._assigns.values()):
                if assign.status not in {"queued", "running", "awaiting_hitl"}:
                    continue
                updated = assign.model_copy(
                    update={
                        "status": "indeterminate",
                        "result_summary": "Manage restarted before the assignment completed",
                        "error_code": "manage_restarted",
                    }
                )
                self._assigns[assign.assign_id] = updated
                self._put(
                    "workgroup_assigns",
                    assign.assign_id,
                    updated.model_dump_json(),
                    workgroup_id=assign.workgroup_id,
                )
                member = self._members.get(assign.member_id)
                if member is not None and (
                    member.active_assign_id == assign.assign_id or member.status == "busy"
                ):
                    released = member.model_copy(
                        update={"active_assign_id": None, "status": "ready"}
                    )
                    self._members[member.member_id] = released
                    self._put(
                        "workgroup_members",
                        member.member_id,
                        released.model_dump_json(),
                        workgroup_id=member.workgroup_id,
                    )
                assign_ids.append(assign.assign_id)
            return {
                "run_ids": run_ids,
                "assign_ids": assign_ids,
                "checkpoint_workgroup_ids": checkpoint_ids,
            }

    def fail_active_assigns(
        self,
        workgroup_id: str,
        *,
        reason: str = "assign interrupted",
        error_code: str = "canceled",
        leader_tool_call_ids: set[str] | None = None,
        exclude_assign_ids: set[str] | None = None,
    ) -> list[str]:
        """将组内仍 active 的 Assign 置为 failed；可按 leader_tool_call_id 过滤。"""
        with self._lock:
            self._ensure_loaded()
            failed: list[str] = []
            for assign in list(self._assigns.values()):
                if assign.workgroup_id != workgroup_id:
                    continue
                if assign.status not in {"queued", "running", "awaiting_hitl"}:
                    continue
                if exclude_assign_ids and assign.assign_id in exclude_assign_ids:
                    continue
                if leader_tool_call_ids is not None:
                    if (assign.leader_tool_call_id or "") not in leader_tool_call_ids:
                        continue
                updated = assign.model_copy(
                    update={
                        "status": "failed",
                        "result_summary": reason,
                        "error_code": error_code,
                    }
                )
                self._assigns[assign.assign_id] = updated
                self._put(
                    "workgroup_assigns",
                    assign.assign_id,
                    updated.model_dump_json(),
                    workgroup_id=workgroup_id,
                )
                member = self._members.get(assign.member_id)
                if member is not None and member.active_assign_id == assign.assign_id:
                    mem = member.model_copy(update={"active_assign_id": None, "status": "ready"})
                    self._members[member.member_id] = mem
                    self._put(
                        "workgroup_members",
                        member.member_id,
                        mem.model_dump_json(),
                        workgroup_id=workgroup_id,
                    )
                failed.append(assign.assign_id)
            return failed

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
        direct_member_id: str | None = None,
    ) -> TimelineEvent:
        listener: Callable[[TimelineEvent], None] | None = None
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
                direct_member_id=direct_member_id,
            )
            events.append(event)
            frame = self._new_timeline_outbox_frame_unlocked(event)
            try:
                if self._db is None:
                    self._put(
                        "workgroup_timeline",
                        event.event_id,
                        event.model_dump_json(),
                        workgroup_id=workgroup_id,
                    )
                    self._put(
                        "workgroup_outbox",
                        f"{workgroup_id}:{frame.delivery_seq}",
                        frame.model_dump_json(),
                        workgroup_id=workgroup_id,
                    )
                else:
                    with self._db.connect() as tx:
                        self._put(
                            "workgroup_timeline",
                            event.event_id,
                            event.model_dump_json(),
                            workgroup_id=workgroup_id,
                            conn=tx,
                        )
                        self._put(
                            "workgroup_outbox",
                            f"{workgroup_id}:{frame.delivery_seq}",
                            frame.model_dump_json(),
                            workgroup_id=workgroup_id,
                            conn=tx,
                        )
                        tx.commit()
            except Exception:
                if events and events[-1] is event:
                    events.pop()
                frames = self._outbox.get(workgroup_id) or []
                if frames and frames[-1] is frame:
                    frames.pop()
                raise
            listener = self._timeline_listener
        if listener is not None:
            try:
                listener(event)
            except Exception:  # noqa: BLE001 - Timeline persistence must not be rolled back by fan-out
                logging.getLogger(__name__).exception(
                    "workgroup timeline listener failed",
                    extra={"workgroup_id": event.workgroup_id, "event_id": event.event_id},
                )
        return event

    def _new_timeline_outbox_frame_unlocked(self, event: TimelineEvent) -> OutboxFrame:
        frames = self._outbox.setdefault(event.workgroup_id, [])
        seq = (frames[-1].delivery_seq + 1) if frames else 1
        frame = OutboxFrame(
            delivery_seq=seq,
            workgroup_id=event.workgroup_id,
            type="timeline.event",
            payload=event.model_dump(mode="json"),
            created_at=event.created_at,
            acked=False,
        )
        frames.append(frame)
        return frame

    def get_timeline_outbox(self, workgroup_id: str, event_id: str) -> OutboxFrame | None:
        with self._lock:
            self._ensure_loaded()
            wid = str(workgroup_id or "").strip()
            target = str(event_id or "").strip()
            if not wid or not target:
                return None
            for frame in self._outbox.get(wid) or []:
                if frame.type == "timeline.event" and str(frame.payload.get("event_id") or "") == target:
                    return frame
            return None

    def reconcile_timeline_outbox(self, workgroup_id: str | None = None) -> int:
        """Backfill Timeline outbox rows created before atomic Timeline/outbox writes."""
        with self._lock:
            self._ensure_loaded()
            workgroups = [str(workgroup_id or "").strip()] if workgroup_id else list(self._timeline)
            missing: list[TimelineEvent] = []
            for wid in workgroups:
                if not wid:
                    continue
                existing = {
                    str(frame.payload.get("event_id") or "")
                    for frame in self._outbox.get(wid) or []
                    if frame.type == "timeline.event"
                }
                missing.extend(
                    event
                    for event in self._timeline.get(wid) or []
                    if event.event_id not in existing
                )
            if not missing:
                return 0

            frames = [self._new_timeline_outbox_frame_unlocked(event) for event in missing]
            try:
                if self._db is None:
                    for frame in frames:
                        self._put(
                            "workgroup_outbox",
                            f"{frame.workgroup_id}:{frame.delivery_seq}",
                            frame.model_dump_json(),
                            workgroup_id=frame.workgroup_id,
                        )
                else:
                    with self._db.connect() as tx:
                        for frame in frames:
                            self._put(
                                "workgroup_outbox",
                                f"{frame.workgroup_id}:{frame.delivery_seq}",
                                frame.model_dump_json(),
                                workgroup_id=frame.workgroup_id,
                                conn=tx,
                            )
                        tx.commit()
            except Exception:
                for frame in reversed(frames):
                    stored = self._outbox.get(frame.workgroup_id) or []
                    if stored and stored[-1] is frame:
                        stored.pop()
                raise
            return len(frames)

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

    def create_hitl(
        self,
        workgroup_id: str,
        *,
        prompt: str,
        run_id: str | None = None,
        tool_call_id: str | None = None,
        reserve_waiter: bool = False,
    ) -> HITLRequest:
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
                run_id=(run_id or "").strip() or None,
                tool_call_id=(tool_call_id or "").strip() or None,
            )
            self._hitl[hitl.hitl_id] = hitl
            self._put(
                "workgroup_hitl",
                hitl.hitl_id,
                hitl.model_dump_json(),
                workgroup_id=workgroup_id,
            )
            self._hitl_waiters.setdefault(hitl.hitl_id, threading.Event())
            if reserve_waiter:
                # Reserve the in-process path before the request can resolve.
                # This closes the create -> wait race used by the native tool.
                self._hitl_waiting.add(hitl.hitl_id)
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

    def list_pending_hitls(self) -> list[HITLRequest]:
        with self._lock:
            self._ensure_loaded()
            return sorted(
                [h for h in self._hitl.values() if h.status == "pending"],
                key=lambda h: (h.created_at, h.hitl_id),
            )

    def list_resolved_bound_hitls(self) -> list[HITLRequest]:
        """Return resolved in-loop HITLs that can be replayed after a restart."""
        with self._lock:
            self._ensure_loaded()
            return sorted(
                [
                    h
                    for h in self._hitl.values()
                    if h.status == "resolved" and h.run_id and h.tool_call_id
                ],
                key=lambda h: (h.resolved_at or h.created_at, h.hitl_id),
            )

    def has_hitl_waiter(self, hitl_id: str) -> bool:
        with self._lock:
            return (hitl_id or "").strip() in self._hitl_waiting

    def wait_hitl_resolved(
        self,
        hitl_id: str,
        *,
        timeout_s: float = 300.0,
    ) -> HITLRequest:
        """阻塞直到 HITL 被 resolve（或超时）。供 Leader ask_workgroup_user 使用。"""
        hid = (hitl_id or "").strip()
        timeout = max(0.1, float(timeout_s))
        with self._lock:
            self._ensure_loaded()
            hitl = self._hitl.get(hid)
            if hitl is None:
                raise WorkgroupError("not_found", "hitl not found", http_status=404)
            if hitl.status == "resolved":
                self._hitl_waiting.discard(hid)
                return hitl
            ev = self._hitl_waiters.setdefault(hid, threading.Event())
            self._hitl_waiting.add(hid)
        try:
            if not ev.wait(timeout):
                raise WorkgroupError(
                    "conflict",
                    f"hitl timed out after {timeout:g}s",
                    http_status=409,
                    retryable=True,
                    details={"hitl_id": hid},
                )
        finally:
            with self._lock:
                self._hitl_waiting.discard(hid)
        with self._lock:
            hitl = self._hitl.get(hid)
            if hitl is None:
                raise WorkgroupError("not_found", "hitl not found", http_status=404)
            if hitl.status != "resolved":
                raise WorkgroupError(
                    "conflict",
                    "hitl waiter woke but status is not resolved",
                    http_status=409,
                    details={"hitl_id": hid, "status": hitl.status},
                )
            return hitl

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
            waiter = self._hitl_waiters.pop(hitl_id, None)
            if waiter is not None:
                waiter.set()
            return updated

    def cancel_pending_hitls(self, workgroup_id: str) -> list[str]:
        """取消组内 pending HITL（turn cancel 时唤醒 ask_workgroup_user 等待）。"""
        canceled: list[str] = []
        with self._lock:
            self._ensure_loaded()
            pending = [
                h
                for h in self._hitl.values()
                if h.workgroup_id == workgroup_id and h.status == "pending"
            ]
        for hitl in pending:
            try:
                self.resolve_hitl_cas(
                    workgroup_id,
                    hitl.hitl_id,
                    resolution={"canceled": True, "answer": ""},
                )
                canceled.append(hitl.hitl_id)
            except WorkgroupError:
                continue
        return canceled

    def subscribe(self, workgroup_id: str, node_id: str) -> Subscription:
        with self._lock:
            self._ensure_loaded()
            if self.get_workgroup(workgroup_id) is None:
                raise WorkgroupError("not_found", "workgroup not found", http_status=404)
            self.assert_acl_member(workgroup_id, node_id)
            sub = self._subscribe_unlocked(workgroup_id, node_id)
            if sub is None:
                raise WorkgroupError("not_authorized", "cannot subscribe", http_status=403)
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
