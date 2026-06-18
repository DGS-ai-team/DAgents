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


def policy_decision_label(decision: str) -> str:
    if decision == "allow_auto":
        return "白名单"
    if decision == "deny":
        return "黑名单"
    return "需审批"


@dataclass
class PolicyViewState:
    mode: bool = False
    snapshot: dict[str, Any] | None = None
    tab: str = "tools"
    shell_type: str = "bash"
    cursor: int = 0
    pending_decision: str = ""
    shell_show_all: bool = False
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
        self.pending_decision = ""
        self.shell_show_all = False
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
        self.pending_decision = ""
        self.shell_show_all = False
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
                rows.append({"tool_name": name, "command": "", "decision": str(item.get("decision") or "")})
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
            decision = str(item.get("decision") or "")
            if not filt and not self.shell_show_all:
                if decision not in {"allow_auto", "deny"}:
                    continue
            if filt and filt not in command.lower():
                continue
            rows.append({"tool_name": "", "command": command, "decision": decision})
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
        self.pending_decision = ""

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
            if not self.shell_show_all:
                header += " · 仅白+黑"
        if self.filter_text.strip():
            header += f" · 过滤: {self.filter_text.strip()}"
        lines = [header, ""]
        if not rows:
            lines.append("（无匹配项）")
        else:
            for i in range(start, end):
                row = rows[i]
                label = row["tool_name"] or row["command"]
                decision = row["decision"]
                if i == self.cursor and self.pending_decision:
                    decision = self.pending_decision
                prefix = "> " if i == self.cursor else "  "
                lines.append(f"{prefix}{label:<22} {policy_decision_label(decision)}")
            if len(rows) > page:
                lines.extend(["", f"显示 {start + 1}-{end} / {len(rows)} · ↑↓ 移动"])
        if self.error_message:
            lines.extend(["", f"[error] {self.error_message}"])
        elif self.status_message:
            lines.extend(["", self.status_message])
        return "\n".join(lines)

    def apply_local_update(self, *, tool_name: str, command: str, decision: str) -> None:
        if not isinstance(self.snapshot, dict):
            return
        if tool_name:
            tools = self.snapshot.setdefault("tools", [])
            if not isinstance(tools, list):
                tools = []
                self.snapshot["tools"] = tools
            for item in tools:
                if isinstance(item, dict) and item.get("name") == tool_name:
                    item["decision"] = decision
                    item["configured"] = True
                    return
            tools.append({"name": tool_name, "decision": decision, "configured": True})
            return
        shell = self.snapshot.setdefault("shell", {})
        if not isinstance(shell, dict):
            shell = {}
            self.snapshot["shell"] = shell
        items = shell.setdefault(self.shell_type, [])
        if not isinstance(items, list):
            items = []
            shell[self.shell_type] = items
        for item in items:
            if isinstance(item, dict) and item.get("command") == command:
                item["decision"] = decision
                item["configured"] = True
                return
        items.append({"command": command, "decision": decision, "configured": True})
