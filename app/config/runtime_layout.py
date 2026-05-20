"""运行根下固定相对路径（不由环境变量覆盖）。

逻辑：
1. 各常量均为相对 **`resolve_runtime_root()`** 的 POSIX 风格片段；
2. **`skills_dir()`** 等与 **`resolve_runtime_root()`** 拼接后 **`resolve`**，供 skills / sqlite / JSONL / agent_id 等模块统一引用；
3. 单测可通过 **`patch`** **`resolve_runtime_root`** 将 IO 隔离到临时目录。

关键分支/边界：
- **不**解析 `~` 以外的环境变量覆盖；部署若需多实例隔离，应使用不同运行根或进程级 **`AGENT_ID`**。

与外部交互：
- 仅路径计算；除调用方外不写盘。

异常说明：
- 无显式异常；路径非法由下游 IO 暴露。
"""

from __future__ import annotations

from pathlib import Path

from app.config.env import resolve_runtime_root

# 相对运行根的固定片段（与 `packaging/runtime/` 预置布局一致）。
SKILLS_RELATIVE = Path(".runtime/skills")
RAW_MESSAGE_HISTORY_RELATIVE = Path(".runtime/history")
SESSION_SQLITE_RELATIVE = Path(".runtime/memory/session.sqlite3")
AGENT_ID_FILE_RELATIVE = Path(".runtime/agent/agent_id")
SHELL_POLICY_RELATIVE = Path(".runtime/policy/shell")
TOOL_POLICY_FILE_RELATIVE = Path(".runtime/policy/tool.approval.txt")
TRIGGERS_STORE_RELATIVE = Path(".runtime/triggers/triggers.json")


def skills_dir() -> Path:
    """技能根目录：`<运行根>/.runtime/skills`。"""
    return (resolve_runtime_root() / SKILLS_RELATIVE).resolve()


def raw_message_history_dir() -> Path:
    """原始消息 JSONL 根目录：`<运行根>/.runtime/history`。"""
    return (resolve_runtime_root() / RAW_MESSAGE_HISTORY_RELATIVE).resolve()


def session_sqlite_path() -> Path:
    """会话 SQLite 文件路径：`<运行根>/.runtime/memory/session.sqlite3`。"""
    return (resolve_runtime_root() / SESSION_SQLITE_RELATIVE).resolve()


def agent_id_file_path() -> Path:
    """Agent ID 持久化文件：`<运行根>/.runtime/agent/agent_id`。"""
    return (resolve_runtime_root() / AGENT_ID_FILE_RELATIVE).resolve()


def shell_policy_dir() -> Path:
    """Shell 审批策略目录：`<运行根>/.runtime/policy/shell`。"""
    return (resolve_runtime_root() / SHELL_POLICY_RELATIVE).resolve()


def tool_policy_file_path() -> Path:
    """工具审批策略文件：`<运行根>/.runtime/policy/tool.approval.txt`。"""
    return (resolve_runtime_root() / TOOL_POLICY_FILE_RELATIVE).resolve()


def triggers_store_path() -> Path:
    """触发器 JSON 存储文件：`<运行根>/.runtime/triggers/triggers.json`。"""
    return (resolve_runtime_root() / TRIGGERS_STORE_RELATIVE).resolve()
