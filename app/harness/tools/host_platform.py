"""宿主机操作系统识别（供 Agent 与 bash 等工具对齐执行环境）。"""

from __future__ import annotations

import json
import platform
import sys
from enum import Enum
from typing import Any

from app.context.models import OpenAIConversationContext
from app.harness.tools.tooling import tool


class HostOsKind(str, Enum):
    """粗粒度 OS 分类，用于分支 shell 与路径语义。

    取值与 `detect_host_os` 一致；`OTHER` 覆盖 BSD、AIX 等未单独建模的平台。
    """

    WINDOWS = "windows"
    LINUX = "linux"
    DARWIN = "darwin"
    OTHER = "other"


def detect_host_os() -> HostOsKind:
    """判定当前 Python 进程所见的宿主机 OS 类别。

    职责：为 `bash_run`、路径处理等提供统一的分支依据。

    逻辑：
    1. 优先根据 `sys.platform` 判断（进程实际运行的 OS API）；
    2. `win32` / `cygwin` / `msys` 归为 Windows 系；
    3. `darwin` 为 macOS；
    4. `linux*` 为 Linux（含 WSL1/2 内 Linux、多数容器内 Linux）；
    5. 其余归为 `OTHER`。

    关键分支：
    - 在 WSL 内运行的 Python 会得到 `LINUX`，而非 Windows（符合进程视角）；
    - 若需「物理机是否为 Windows」，应通过其它渠道采集，本函数不区分。

    副作用：无。
    """
    p = sys.platform
    if p == "win32" or p.startswith("cygwin") or p == "msys":
        return HostOsKind.WINDOWS
    if p == "darwin":
        return HostOsKind.DARWIN
    if p.startswith("linux"):
        return HostOsKind.LINUX
    return HostOsKind.OTHER


def host_platform_facts() -> dict[str, Any]:
    """返回宿主机与 Python 运行环境的结构化事实。

    逻辑：
    1. 调用 `detect_host_os` 写入 `os_kind`；
    2. 补充 `platform` 模块常用字段与 `sys.platform` 原文。

    与外部交互：无；仅读取标准库。

    Returns:
        可 JSON 序列化的字典，便于日志与 Agent 消费。
    """
    return {
        "os_kind": detect_host_os().value,
        "system": platform.system(),
        "release": platform.release(),
        "version": platform.version(),
        "machine": platform.machine(),
        "python_sys_platform": sys.platform,
        "python_version": sys.version.split()[0],
    }


def host_platform_summary_text() -> str:
    """生成供模型阅读的简短宿主机说明文本。

    逻辑：
    1. 取 `host_platform_facts()`；
    2. 格式化为固定字段的多行文本，便于复制到对话上下文。

    Returns:
        人类可读、无 JSON 也可扫读的摘要字符串。
    """
    facts = host_platform_facts()
    lines = [
        "[HOST_PLATFORM]",
        f"os_kind={facts['os_kind']}",
        f"system={facts['system']!r} release={facts['release']!r}",
        f"machine={facts['machine']!r}",
        f"python_sys_platform={facts['python_sys_platform']!r}",
        f"python_version={facts['python_version']!r}",
        "说明：os_kind 表示当前 Python 进程所在环境；WSL 内一般为 linux。",
    ]
    return "\n".join(lines)


@tool("host_platform")
def host_platform(context: OpenAIConversationContext | None = None) -> str:
    """使用场景：在执行 shell/路径相关工具前，先确认当前进程视角的操作系统类型。

    字段说明：
    - 无入参。
    - 返回中的 `os_kind` 可能为 `windows/linux/darwin/other`。

    返回说明：
    - 成功：返回 JSON + 文本摘要，含 `os_kind/system/release/machine` 等字段。
    - 失败：返回 `ERROR: ...`。

    调用范例：
    - `host_platform({})`
    - `host_platform()`
    """
    try:
        facts = host_platform_facts()
        payload = json.dumps(facts, ensure_ascii=False, indent=2)
        return f"{payload}\n\n{host_platform_summary_text()}"
    except Exception as exc:
        return f"ERROR: host_platform 失败: {exc}"
