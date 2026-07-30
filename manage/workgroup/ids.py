"""D0.5 前缀 ID 生成（26 位小写 hex，满足 [0-9a-z]{26}）。"""

from __future__ import annotations

import secrets


def new_ulid() -> str:
    # token_hex(13) → 26 个小写 hex；是 ULID 契约 pattern 的合法子集。
    return secrets.token_hex(13)


def new_id(prefix: str) -> str:
    return f"{prefix}_{new_ulid()}"


def workgroup_id() -> str:
    return new_id("wg")


def member_id() -> str:
    return new_id("mb")


def grant_id() -> str:
    return new_id("gr")


def lease_id() -> str:
    return new_id("ls")


def assign_id() -> str:
    return new_id("as")


def run_id() -> str:
    return new_id("rn")


def event_id() -> str:
    return new_id("ev")


def hitl_id() -> str:
    return new_id("ht")
