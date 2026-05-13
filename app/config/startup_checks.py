"""启动阶段的轻量环境提示（不阻断进程）。"""

from __future__ import annotations

import logging

from app.config.host_snapshot import get_host_snapshot

_logger = logging.getLogger(__name__)


def emit_linux_cross_user_shell_startup_hints() -> None:
    """在 Linux 上根据有效 UID 输出跨用户 shell（如 `su -` / `sudo -u`）相关提示。

    逻辑：
    1. 读取 **`get_host_snapshot()`**（启动时已采集则不再探测 OS/用户）；
    2. 非 Linux 直接返回；
    3. 无有效 UID（非 POSIX）时返回；
    4. **root** / **非 root**：各输出一条多行 **`logging`** 记录（单一通道，避免再 **`stderr.write`** 与默认 **`logging`** 打到同一终端导致重复）。

    关键分支与边界：
    - 仅提示，不抛异常、不修改进程权限；
    - 使用 **`WARNING`**：在未配置 handler 时 Python **`logging`** 的 **`lastResort`** 仍会落入 stderr，root / 非 root 分支均可见且各只出现一次。

    与外部交互：
    - 仅调用 **`logging`**。

    副作用说明：
    - 每次启动至多触发一条分支对应的输出。
    """

    snap = get_host_snapshot()
    if snap.os_kind != "linux":
        return
    euid = snap.effective_uid
    if euid is None:
        return
    if euid == 0:
        lines = (
            "[dagents][startup] 当前运行在 Linux 且进程有效用户为 root。\n"
            f"[dagents][startup] 进程登录名（快照）：{snap.login_name!r}；有效 UID={euid}。\n"
            "[dagents][startup] 跨用户执行 shell（如 `su - <user>`）时约束相对较少；"
            "仍建议以低权限专用账户运行 API，仅在必要时使用特权命令。\n"
        )
        _logger.warning("%s", lines.strip())
    else:
        lines = (
            "[dagents][startup] 当前运行在 Linux 且未使用 root 权限。\n"
            f"[dagents][startup] 进程登录名（快照）：{snap.login_name!r}；有效 UID={euid}。\n"
            "[dagents][startup] 若需要通过 subprocess 跨用户执行 shell（例如其他 Unix 账户下的 `su - <user>`），\n"
            "[dagents][startup] 通常需要为该进程的运行用户在 sudoers 中配置受限免密规则（推荐配合 `sudo -n -u`，详见仓库 README「Linux 与跨用户 shell / sudoers」）。\n"
        )
        _logger.warning("%s", lines.strip())
