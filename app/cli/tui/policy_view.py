"""Policy 全屏视图状态与渲染（/policy TUI）。"""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any

PROTECTED_POLICY_TOOL = "ask_user_information"
POLICY_SHELL_ORDER = ("bash", "cmd", "powershell")
# render_text 中 header、空行、分页指示等非列表行占用高度。
_POLICY_RENDER_CHROME_LINES = 3


def policy_list_page_size(viewport_rows: int, *, extra_footer: bool = False) -> int:
    """根据 RichLog 可见行数计算单页可展示的策略行数。"""
    chrome = _POLICY_RENDER_CHROME_LINES + (1 if extra_footer else 0)
    if viewport_rows <= 0:
        viewport_rows = 20
    return max(1, viewport_rows - chrome)


def decision_to_mode(decision: str) -> str:
    if decision == "allow_auto":
        return "never"
    if decision == "deny":
        return "deny"
    if decision == "require_approval":
        return "always"
    return "rule"


def entry_mode(item: dict[str, Any]) -> str:
    mode = str(item.get("mode") or "").strip()
    if mode:
        return mode
    return decision_to_mode(str(item.get("decision") or ""))


def policy_mode_label(mode: str) -> str:
    if mode == "never":
        return "自动允许"
    if mode == "always":
        return "需审批"
    if mode == "rule":
        return "特殊规则"
    if mode == "deny":
        return "禁止"
    return mode or "—"


def policy_decision_label(decision: str) -> str:
    """兼容旧 decision 字段。"""
    return policy_mode_label(decision_to_mode(decision))


def normalize_shell_command(raw: str) -> str:
    text = str(raw or "").strip()
    if not text:
        return ""
    return text.split()[0].lower()


def mode_to_legacy_decision(mode: str) -> str:
    if mode == "never":
        return "allow_auto"
    if mode == "deny":
        return "deny"
    if mode == "always":
        return "require_approval"
    return "require_approval"


@dataclass
class PolicyViewState:
    mode: bool = False
    snapshot: dict[str, Any] | None = None
    tab: str = "tools"
    shell_type: str = "bash"
    cursor: int = 0
    pending_mode: str = ""
    filter_text: str = ""
    scroll_offset: int = 0
    status_message: str = ""
    error_message: str = ""

    def reset(self) -> None:
        self.mode = False
        self.snapshot = None
        self.tab = "tools"
        self.shell_type = "bash"
        self.cursor = 0
        self.pending_mode = ""
        self.filter_text = ""
        self.scroll_offset = 0
        self.status_message = ""
        self.error_message = ""

    def load_snapshot(self, data: dict[str, Any]) -> None:
        self.snapshot = data
        platform = data.get("platform") if isinstance(data.get("platform"), dict) else {}
        default_shell = str(platform.get("default_shell") or "bash").strip() or "bash"
        self.shell_type = default_shell
        self.tab = "tools"
        self.cursor = 0
        self.pending_mode = ""
        self.filter_text = ""
        self.scroll_offset = 0
        goos = str(platform.get("goos") or "-")
        self.status_message = f"策略管理 · Node {goos} · 默认 shell={default_shell}"

    def visible_rows(self) -> list[dict[str, str]]:
        if not isinstance(self.snapshot, dict):
            return []
        filt = self.filter_text.strip().lower()
        if self.tab == "tools":
            tools = self.snapshot.get("tools")
            rows: list[dict[str, str]] = []
            if not isinstance(tools, list):
                return rows
            for item in tools:
                if not isinstance(item, dict):
                    continue
                name = str(item.get("name") or "")
                if filt and filt not in name.lower():
                    continue
                rows.append({"tool_name": name, "command": "", "mode": entry_mode(item)})
            return rows
        shell = self.snapshot.get("shell")
        items: list[Any] = []
        if isinstance(shell, dict):
            raw = shell.get(self.shell_type)
            if isinstance(raw, list):
                items = raw
        rows = []
        for item in items:
            if not isinstance(item, dict):
                continue
            command = str(item.get("command") or "")
            if filt and filt not in command.lower():
                continue
            rows.append({"tool_name": "", "command": command, "mode": entry_mode(item)})
        return rows

    def clamp_cursor(self) -> None:
        rows = self.visible_rows()
        if not rows:
            self.cursor = 0
            self.scroll_offset = 0
            return
        if self.cursor >= len(rows):
            self.cursor = len(rows) - 1

    def ensure_scroll_visible(self, viewport_rows: int) -> None:
        """保证 cursor 落在当前分页窗口内。"""
        rows = self.visible_rows()
        if not rows:
            self.scroll_offset = 0
            return
        page = policy_list_page_size(
            viewport_rows,
            extra_footer=bool(self.error_message or self.status_message),
        )
        if self.cursor < self.scroll_offset:
            self.scroll_offset = self.cursor
        elif self.cursor >= self.scroll_offset + page:
            self.scroll_offset = self.cursor - page + 1
        max_offset = max(0, len(rows) - page)
        if self.scroll_offset > max_offset:
            self.scroll_offset = max_offset

    def cycle_shell(self, delta: int) -> None:
        if self.tab != "shell":
            return
        try:
            idx = POLICY_SHELL_ORDER.index(self.shell_type)
        except ValueError:
            idx = 0
        idx = (idx + delta) % len(POLICY_SHELL_ORDER)
        self.shell_type = POLICY_SHELL_ORDER[idx]
        self.cursor = 0
        self.scroll_offset = 0
        self.pending_mode = ""

    def render_text(self, viewport_rows: int = 20) -> str:
        rows = self.visible_rows()
        self.clamp_cursor()
        extra_footer = bool(self.error_message or self.status_message)
        page = policy_list_page_size(viewport_rows, extra_footer=extra_footer)
        self.ensure_scroll_visible(viewport_rows)
        start = self.scroll_offset
        end = min(start + page, len(rows))
        tab_tools = ">工具<" if self.tab == "tools" else "[工具]"
        tab_shell = ">Shell<" if self.tab == "shell" else "[Shell]"
        header = f"Tab {tab_tools} {tab_shell}"
        if self.tab == "shell":
            header += f" · shell={self.shell_type}"
            header += " · 未列出默认需审批"
        if self.filter_text.strip():
            header += f" · 过滤: {self.filter_text.strip()}"
        lines = [header, ""]
        if not rows:
            lines.append("（无匹配项）")
        else:
            for i in range(start, end):
                row = rows[i]
                label = row["tool_name"] or row["command"]
                mode = row["mode"]
                if i == self.cursor and self.pending_mode:
                    mode = self.pending_mode
                prefix = "> " if i == self.cursor else "  "
                lines.append(f"{prefix}{label:<22} {policy_mode_label(mode)}")
            if len(rows) > page:
                lines.extend(["", f"显示 {start + 1}-{end} / {len(rows)} · ↑↓ 移动"])
        if self.error_message:
            lines.extend(["", f"[error] {self.error_message}"])
        elif self.status_message:
            lines.extend(["", self.status_message])
        return "\n".join(lines)

    def apply_local_update(self, *, tool_name: str, command: str, mode: str) -> None:
        if not isinstance(self.snapshot, dict):
            return
        if tool_name:
            tools = self.snapshot.setdefault("tools", [])
            if not isinstance(tools, list):
                tools = []
                self.snapshot["tools"] = tools
            for item in tools:
                if isinstance(item, dict) and item.get("name") == tool_name:
                    item["mode"] = mode
                    item["decision"] = mode_to_legacy_decision(mode)
                    item["configured"] = True
                    return
            tools.append(
                {
                    "name": tool_name,
                    "mode": mode,
                    "decision": mode_to_legacy_decision(mode),
                    "configured": True,
                }
            )
            return
        shell = self.snapshot.setdefault("shell", {})
        if not isinstance(shell, dict):
            shell = {}
            self.snapshot["shell"] = shell
        items = shell.setdefault(self.shell_type, [])
        if not isinstance(items, list):
            items = []
            shell[self.shell_type] = items
        cmd = normalize_shell_command(command)
        for item in items:
            if isinstance(item, dict) and normalize_shell_command(str(item.get("command") or "")) == cmd:
                item["command"] = cmd
                item["mode"] = mode
                item["decision"] = mode_to_legacy_decision(mode)
                item["configured"] = True
                return
        items.append(
            {
                "command": cmd,
                "mode": mode,
                "decision": mode_to_legacy_decision(mode),
                "configured": True,
            }
        )

    def remove_local_shell_entry(self, *, command: str) -> None:
        if not isinstance(self.snapshot, dict):
            return
        cmd = normalize_shell_command(command)
        shell = self.snapshot.setdefault("shell", {})
        if not isinstance(shell, dict):
            shell = {}
            self.snapshot["shell"] = shell
        items = shell.setdefault(self.shell_type, [])
        if not isinstance(items, list):
            items = []
            shell[self.shell_type] = items
        shell[self.shell_type] = [
            item
            for item in items
            if not (isinstance(item, dict) and normalize_shell_command(str(item.get("command") or "")) == cmd)
        ]
