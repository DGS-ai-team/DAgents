"""Manage Admin API（审计等；A2A Task 观测已随 inbox 退役移除）。"""

from __future__ import annotations

from fastapi import APIRouter

from manage.registry.store import AgentRegistryStore


def build_admin_router(registry: AgentRegistryStore) -> APIRouter:
    del registry  # 保留参数以兼容 create_app 装配签名
    return APIRouter(prefix="/v1/admin", tags=["admin"])
