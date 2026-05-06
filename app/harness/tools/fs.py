"""文件工具四件套（read/edit/search/write）。"""

from __future__ import annotations

import json
import os
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
    - 文件最后修改时间；
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
            f"文件修改时间: {target.stat().st_mtime}",
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


@tool("edit_file")
def edit_file(path: str, edits_json: str, context: OpenAIConversationContext | None = None) -> str:
    """按行编辑文件。

    `edits_json` 支持两种格式：
    1) `[{...}, {...}]`
    2) `{"edits":[{...}, {...}]}`

    动作支持：
    - `delete`：删除 `start_line-end_line`
    - `replace`：替换 `start_line-end_line` 为 `content`
    - `insert`：在 `start_line` 前插入 `content`
    """
    try:
        del context
        target = _resolve_under_root(path)
        if not target.exists():
            return f"ERROR: 文件不存在：{path!r}"
        if target.is_dir():
            return f"ERROR: 目标是目录，无法编辑：{path!r}"
        payload = json.loads(edits_json)
        if isinstance(payload, dict):
            edits = payload.get("edits", [])
        elif isinstance(payload, list):
            edits = payload
        else:
            return "ERROR: edits_json 必须是数组或包含 edits 的对象。"
        if not isinstance(edits, list):
            return "ERROR: edits_json.edits 必须是数组。"
        old_lines = target.read_text(encoding="utf-8", errors="replace").splitlines()
        new_lines = _apply_line_edits(old_lines, [dict(item) for item in edits if isinstance(item, dict)])
        new_text = "\n".join(new_lines)
        if target.read_text(encoding="utf-8", errors="replace").endswith("\n"):
            new_text += "\n"
        target.write_text(new_text, encoding="utf-8")
        return f"OK: 已编辑 {path!r}（行数 {len(old_lines)} -> {len(new_lines)}）"
    except Exception as exc:
        return f"ERROR: edit_file 失败: {exc}"


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

