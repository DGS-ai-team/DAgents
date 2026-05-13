"""进程级主机环境快照：启动时采集一次，供后续逻辑直接读取。"""

from __future__ import annotations

import getpass
import logging
import os
import platform
import sys
import time
from dataclasses import dataclass

_logger = logging.getLogger(__name__)

_snapshot: HostSnapshot | None = None


@dataclass(frozen=True, slots=True)
class HostSnapshot:
    """启动时刻采集的环境信息（不可变）。

    字段说明：
    - **os_kind**：粗分类，`windows` / `linux` / `darwin` / `other`（与 `sys.platform` 推断一致）；
    - **login_name**：`getpass.getuser()`，失败则为空串；
    - **effective_uid / effective_gid**：POSIX 有效 UID/GID；Windows 等非 POSIX 为 `None`。
    """

    captured_at_unix: float
    os_kind: str
    sys_platform: str
    platform_system: str
    platform_release: str
    machine: str
    login_name: str
    effective_uid: int | None
    effective_gid: int | None


def _infer_os_kind() -> str:
    """与 `detect_host_os` 对齐的粗分类字符串（避免 config 依赖 harness）。"""

    p = sys.platform
    if p == "win32" or p.startswith("cygwin") or p == "msys":
        return "windows"
    if p == "darwin":
        return "darwin"
    if p.startswith("linux"):
        return "linux"
    return "other"


def _build_host_snapshot() -> HostSnapshot:
    """构造当前进程快照（单次探针）。"""

    try:
        login_name = str(getpass.getuser() or "").strip()
    except Exception:
        login_name = ""
    euid: int | None = None
    egid: int | None = None
    try:
        euid = int(os.geteuid())
        egid = int(os.getgid())
    except (AttributeError, OSError, TypeError, ValueError):
        euid = None
        egid = None
    return HostSnapshot(
        captured_at_unix=time.time(),
        os_kind=_infer_os_kind(),
        sys_platform=sys.platform,
        platform_system=platform.system(),
        platform_release=platform.release(),
        machine=platform.machine(),
        login_name=login_name,
        effective_uid=euid,
        effective_gid=egid,
    )


def capture_host_snapshot_at_startup() -> HostSnapshot:
    """在 API 启动路径显式采集并缓存快照，并打一条 INFO 日志。

    逻辑：
    1. 调用 **`_build_host_snapshot`** 写入模块级缓存；
    2. 输出结构化 **`logging`** 记录，便于运维检索；
    3. 返回同一快照对象。

    关键分支：
    - 可重复调用：每次覆盖缓存（适用于调试重启；正式环境通常只调一次）。

    副作用：
    - 修改模块级 **`_snapshot`**。
    """

    global _snapshot
    _snapshot = _build_host_snapshot()
    snap = _snapshot
    _logger.info(
        "[dagents][host_snapshot] os_kind=%s login_name=%r euid=%s egid=%s sys_platform=%r "
        "platform_system=%r platform_release=%r machine=%r captured_at_unix=%s",
        snap.os_kind,
        snap.login_name,
        snap.effective_uid,
        snap.effective_gid,
        snap.sys_platform,
        snap.platform_system,
        snap.platform_release,
        snap.machine,
        snap.captured_at_unix,
    )
    return snap


def get_host_snapshot() -> HostSnapshot:
    """返回主机快照；若尚未显式采集则惰性构建并缓存（不写 INFO 启动日志）。

    逻辑：
    1. 缓存命中直接返回；
    2. 否则 **`_build_host_snapshot`** 写入缓存再返回（单测、工具先于 API 调用时用）。

    副作用：
    - 可能在首次调用时写入 **`_snapshot`**。
    """

    global _snapshot
    if _snapshot is None:
        _snapshot = _build_host_snapshot()
    return _snapshot
