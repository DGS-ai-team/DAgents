"""文件工具四件套（read/edit/search/write）。"""

from __future__ import annotations

import difflib
import json
import os
from datetime import datetime
from pathlib import Path
from typing import Any, Optional

from app.context.models import OpenAIConversationContext
from app.harness.tools.tool import tool

DEFAULT_MAX_READ_BYTES = 3000
DEFAULT_LINE_OFFSET = 1
DEFAULT_LINE_LIMIT = 100
DEFAULT_SEARCH_CONTEXT_LINES = 10
DEFAULT_SEARCH_MAX_HITS = 5
_TEXT_SUFFIXES = {
    ".txt",
    ".md",
    ".py",
    ".json",
    ".yaml",
    ".yml",
    ".toml",
    ".ini",
    ".cfg",
    ".sh",
    ".bat",
    ".ps1",
    ".log",
    ".csv",
    ".ts",
    ".tsx",
    ".js",
    ".jsx",
}


def _workspace_root() -> Path:
    """返回文件工具允许访问的根目录。

    逻辑：
    1. 读取 `FS_ROOT` 环境变量；若配置则使用该目录；
    2. 未配置时回退到当前进程工作目录；
    3. 统一转绝对路径并返回，供后续路径校验复用。
    """
    fs_root = os.environ.get("FS_ROOT", "").strip()
    if fs_root:
        return Path(fs_root).expanduser().resolve()
    return Path.cwd().resolve()


def _resolve_under_root(path: str) -> Path:
    """将用户路径解析为根目录内的绝对路径，越界时抛异常。

    逻辑：
    1. 计算工作区根目录；
    2. 相对路径按根目录拼接，绝对路径直接使用；
    3. `resolve()` 后校验目标路径必须在根目录内；
    4. 越界访问时抛 `ValueError`，阻断读写与编辑。
    """
    root = _workspace_root()
    candidate = Path(path).expanduser()
    resolved = candidate.resolve() if candidate.is_absolute() else (root / candidate).resolve()
    try:
        resolved.relative_to(root)
    except ValueError as exc:
        raise ValueError(f"path 越界，必须位于 FS_ROOT 内：{path!r}") from exc
    return resolved


def _line_numbered_text(lines: list[str], start_line: int) -> str:
    """将文本行转换为带行号格式 `行号>内容`。"""

    return "\n".join(f"{start_line + idx}>{line}" for idx, line in enumerate(lines))


def _read_text_lines(path: Path) -> list[str]:
    """读取文本文件并返回行数组（保留空行，不保留行尾换行符）。"""

    suffix = path.suffix.lower()
    if suffix == ".json":
        obj = json.loads(path.read_text(encoding="utf-8", errors="replace"))
        pretty = json.dumps(obj, ensure_ascii=False, indent=2)
        return pretty.splitlines()
    if suffix in _TEXT_SUFFIXES:
        return path.read_text(encoding="utf-8", errors="replace").splitlines()
    raise ValueError(f"不支持读取该后缀文件：{suffix or '<no-suffix>'}")


def _window_lines(lines: list[str], line_offset: int, line_limit: int) -> tuple[int, int]:
    """计算读取窗口（1-based 输入，返回 0-based [start, end)）。"""

    total = len(lines)
    if line_offset > 0:
        start = max(line_offset - 1, 0)
    else:
        start = max(total + line_offset, 0)
    end = min(start + line_limit, total)
    return start, end


def _format_fs_mtime(mtime: float) -> str:
    """将 `Path.stat().st_mtime` 格式化为易读字符串。

    逻辑：
    1. **`datetime.fromtimestamp(...).astimezone()`** 得到带偏移的本地时间；
    2. 使用 **`isoformat(timespec="seconds")`** 输出 **ISO 8601**（含 `+08:00` 等）；
    3. 附加原始 **Unix 浮点秒**，便于与日志/系统工具对照。
    """

    dt = datetime.fromtimestamp(mtime).astimezone()
    return f"{dt.isoformat(timespec='seconds')}"


@tool("read_file")
def read_file(
    path: str,
    max_bytes: Optional[int] = 3000,
    line_offset: Optional[int] = 1,
    line_limit: Optional[int] = 100,
    context: OpenAIConversationContext | None = None,
) -> str:
    """读取文件（按后缀选择读取策略），并返回带行号文本。

    输出头部包含：
    - 行号说明；
    - 文件最后修改时间（**ISO 8601 带时区偏移** + **unix 浮点秒**）；
    - 当前展示行区间；
    - 后方是否仍有未读取行。
    """
    try:
        del context
        target = _resolve_under_root(path)
        if not target.exists():
            return f"ERROR: 文件不存在：{path!r}"
        if target.is_dir():
            return f"ERROR: 目标是目录，无法读取：{path!r}"

        limit = DEFAULT_MAX_READ_BYTES if max_bytes is None else max(1, int(max_bytes))
        all_lines = _read_text_lines(target)
        offset = DEFAULT_LINE_OFFSET if line_offset is None else int(line_offset)
        count = DEFAULT_LINE_LIMIT if line_limit is None else max(1, int(line_limit))
        start, end = _window_lines(all_lines, offset, count)
        window_lines = all_lines[start:end]
        total = len(all_lines)
        has_more_after = end < total
        header = [
            "行号说明: 每行使用 `行号>内容` 格式",
            f"文件修改时间: {_format_fs_mtime(target.stat().st_mtime)}",
            f"展示行区间: {start + 1}-{end} / {total}",
            f"后方是否还有未读取行: {'是' if has_more_after else '否'}",
            "---",
        ]
        text = "\n".join(header) + "\n" + _line_numbered_text(window_lines, start + 1)
        data = text.encode("utf-8")
        if len(data) > limit:
            text = data[:limit].decode("utf-8", errors="replace")
            text += f"\n\n[TRUNCATED] 输出超过 {limit} bytes，已截断。"
        return text
    except Exception as exc:
        return f"ERROR: read_file 失败: {exc}"


@tool("write_file")
def write_file(
    path: str,
    content: str,
    context: OpenAIConversationContext | None = None,
) -> str:
    """覆盖写入文件。"""
    try:
        del context
        target = _resolve_under_root(path)
        target.parent.mkdir(parents=True, exist_ok=True)

        if target.exists() and target.is_dir():
            return f"ERROR: 目标是目录，无法写入：{path!r}"

        target.write_text(content, encoding="utf-8")
        return f"OK: 已写入 {path!r} ({len(content.encode('utf-8'))} bytes)"
    except Exception as exc:
        return f"ERROR: write_file 失败: {exc}"


def _apply_line_edits(lines: list[str], edits: list[dict[str, Any]]) -> list[str]:
    """按编辑动作数组执行行级修改。"""

    updated = list(lines)
    for item in edits:
        action = str(item.get("action") or "").strip().lower()
        start_line = int(item.get("start_line") or 0)
        end_line = int(item.get("end_line") or start_line)
        if start_line <= 0 or end_line <= 0 or end_line < start_line:
            raise ValueError(f"非法行区间: {start_line}-{end_line}")
        start_idx = start_line - 1
        end_idx = end_line
        if action == "delete":
            del updated[start_idx:end_idx]
        elif action == "replace":
            content = str(item.get("content") or "")
            replacement = content.splitlines()
            updated[start_idx:end_idx] = replacement
        elif action == "insert":
            content = str(item.get("content") or "")
            replacement = content.splitlines()
            updated[start_idx:start_idx] = replacement
        else:
            raise ValueError(f"不支持的编辑动作: {action!r}")
    return updated


def _unified_diff_text(label: str, before: list[str], after: list[str]) -> str:
    """生成 unified diff（`diff -u` 风格，`@@` 行含旧/新文件起始行号）。

    逻辑：
    1. **`difflib.unified_diff`** 产出与 Linux **`diff -u`** 同类的 `-`/`+`/上下文行；
    2. 完全无差异时返回固定提示，避免工具输出空串。
    """

    joined = "\n".join(
        difflib.unified_diff(
            before,
            after,
            fromfile=f"a/{label}",
            tofile=f"b/{label}",
            lineterm="",
        )
    )
    return joined if joined.strip() else "(无可见差异：编辑前后行序列一致。)"


@tool("edit_file")
def edit_file(path: str, edits_json: str, context: OpenAIConversationContext | None = None) -> dict[str, Any]:
    """使用场景：在 **`FS_ROOT`** 内对已有文本文件做按行删/改/插。

    字段说明：
    - `path`：工作区内路径。
    - `edits_json`：JSON 对象，**仅** `{"edits":[...]}`；行号从 1 起；`delete`/`replace` 用 **`start_line`～`end_line` 闭区间**；`insert` 在 **`start_line` 行前**插入；多条按 **`edits`** 数组顺序依次执行。

    返回说明：
    - 成功：`ok=true`，含 **`path`**、**`diff`**（类 **`diff -u`**，**`@@` 内为行号范围**）。
    - 失败：`ok=false`，**`error`** 说明原因。

    调用范例（`edits_json`）：
    - `{"edits":[{"action":"delete","start_line":3,"end_line":5}]}`
    - `{"edits":[{"action":"replace","start_line":10,"end_line":12,"content":"# A\\nB"}]}`
    - `{"edits":[{"action":"insert","start_line":1,"content":"#!/usr/bin/env python3\\n"}]}`
    - `{"edits":[{"action":"delete","start_line":2,"end_line":2},{"action":"insert","start_line":2,"content":"# x"}]}`
    """
    try:
        del context
        target = _resolve_under_root(path)
        if not target.exists():
            return {"ok": False, "error": f"文件不存在：{path!r}"}
        if target.is_dir():
            return {"ok": False, "error": f"目标是目录，无法编辑：{path!r}"}
        payload = json.loads(edits_json)
        # 仅接受 {"edits":[...]}，避免与顶层数组两种写法长期分叉。
        if not isinstance(payload, dict):
            return {"ok": False, "error": 'edits_json 必须是 JSON 对象，形如 {"edits":[...]}。'}
        if "edits" not in payload:
            return {"ok": False, "error": 'edits_json 缺少 "edits" 字段。'}
        edits = payload["edits"]
        if not isinstance(edits, list):
            return {"ok": False, "error": '"edits" 必须是数组。'}
        raw_text = target.read_text(encoding="utf-8", errors="replace")
        old_lines = raw_text.splitlines()
        new_lines = _apply_line_edits(old_lines, [dict(item) for item in edits if isinstance(item, dict)])
        new_text = "\n".join(new_lines)
        if raw_text.endswith("\n"):
            new_text += "\n"
        diff_text = _unified_diff_text(path, old_lines, new_lines)
        target.write_text(new_text, encoding="utf-8")
        return {
            "ok": True,
            "path": path,
            "diff": diff_text,
        }
    except Exception as exc:
        return {"ok": False, "error": f"edit_file 失败: {exc}"}


@tool("search_file")
def search_file(path: str, keyword: str, context: OpenAIConversationContext | None = None) -> str:
    """查找关键字，返回命中点前后各 10 行（最多展示 5 处命中）。"""

    try:
        del context
        target = _resolve_under_root(path)
        if not target.exists():
            return f"ERROR: 文件不存在：{path!r}"
        if target.is_dir():
            return f"ERROR: 目标是目录，无法搜索：{path!r}"
        needle = str(keyword or "")
        if not needle:
            return "ERROR: keyword 不能为空。"
        lines = _read_text_lines(target)
        hit_indexes = [idx for idx, line in enumerate(lines) if needle in line]
        shown_hits = hit_indexes[:DEFAULT_SEARCH_MAX_HITS]
        blocks: list[str] = []
        for seq, hit_idx in enumerate(shown_hits, start=1):
            start = max(hit_idx - DEFAULT_SEARCH_CONTEXT_LINES, 0)
            end = min(hit_idx + DEFAULT_SEARCH_CONTEXT_LINES + 1, len(lines))
            blocks.append(
                f"命中#{seq}: 行 {hit_idx + 1}（展示区间 {start + 1}-{end}）\n"
                + _line_numbered_text(lines[start:end], start + 1)
            )
        header = [
            f"文件: {target}",
            f"关键字: {needle!r}",
            f"总命中数: {len(hit_indexes)}",
            f"展示命中数: {len(shown_hits)}（最多 {DEFAULT_SEARCH_MAX_HITS}）",
            f"上下文行数: 前后各 {DEFAULT_SEARCH_CONTEXT_LINES} 行",
            "---",
        ]
        if not blocks:
            return "\n".join(header + ["未命中。"])
        return "\n\n".join(["\n".join(header)] + blocks)
    except Exception as exc:
        return f"ERROR: search_file 失败: {exc}"

