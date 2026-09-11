"""Workgroup 协作域（Manage 编排基座）。

权威契约：docs/design/workgroup-and-node-gateway.md，以及
Node `internal/workgroup` 与 Manage `workgroup` 的当前实现。
"""

from manage.workgroup.store import WorkGroupStore

__all__ = ["WorkGroupStore"]
