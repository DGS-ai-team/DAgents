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
DEFAULT_SEARCH_INDEX_OFFSET = 0
DEFAULT_SEARCH_COUNT_LIMIT = 5
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
    """
    读取文件，并返回带行号文本。如果一次读取的文件内容超过max_bytes，则返回截断提示。
    如果读取的文件内容超过line_limit，则返回截断提示。
    通过调整line_offset和line_limit可以调整读取的行数和偏移量以读取未被展示的内容。
    行号说明: 
        每行使用 `行号>内容` 格式。
    字段说明：
    - `path`：文件路径。
    - `max_bytes`：最大读取字节数，默认3000。
    - `line_offset`：行偏移量，默认1。
    - `line_limit`：行限制，默认100。
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
        st = target.stat()
        dt = datetime.fromtimestamp(st.st_mtime).astimezone()
        mtime_txt = f"{dt.isoformat(timespec='seconds')}（unix {st.st_mtime:.6f}）"
        header = [
            f"文件修改时间: {mtime_txt}",
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
    """使用场景：在 **`FS_ROOT`** 内对已有文本文件做按行删/改/插。

    字段说明：
    - `path`：工作区内路径。
    - `edits_json`：JSON 对象，**仅** `{"edits":[...]}`；行号从 1 起；`delete`/`replace` 用 **`start_line`～`end_line` 闭区间**；`insert` 在 **`start_line` 行前**插入；多条按 **`edits`** 数组顺序依次执行。

    返回说明：
    - 字符串：**头部**为 **`成功: 是|否`**、**`路径: ...`**，失败时另有 **`错误: ...`**；一行 **`---`** 后为 **正文**（类 **`diff -u`**）；失败时正文为空。

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
            return f"成功: 否\n路径: {path}\n错误: 文件不存在：{path!r}\n---\n"
        if target.is_dir():
            return f"成功: 否\n路径: {path}\n错误: 目标是目录，无法编辑：{path!r}\n---\n"
        payload = json.loads(edits_json)
        # 仅接受 {"edits":[...]}，避免与顶层数组两种写法长期分叉。
        if not isinstance(payload, dict):
            return (
                f"成功: 否\n路径: {path}\n"
                f'错误: edits_json 必须是 JSON 对象，形如 {{"edits":[...]}}。\n---\n'
            )
        if "edits" not in payload:
            return f'成功: 否\n路径: {path}\n错误: edits_json 缺少 "edits" 字段。\n---\n'
        edits = payload["edits"]
        if not isinstance(edits, list):
            return f'成功: 否\n路径: {path}\n错误: "edits" 必须是数组。\n---\n'
        raw_text = target.read_text(encoding="utf-8", errors="replace")
        old_lines = raw_text.splitlines()
        new_lines = _apply_line_edits(old_lines, [dict(item) for item in edits if isinstance(item, dict)])
        new_text = "\n".join(new_lines)
        if raw_text.endswith("\n"):
            new_text += "\n"
        joined = "\n".join(
            difflib.unified_diff(
                old_lines,
                new_lines,
                fromfile=f"a/{path}",
                tofile=f"b/{path}",
                lineterm="",
            )
        )
        # 与 `diff -u` 同类；无差异时仍给提示，避免正文空串。
        diff_text = joined if joined.strip() else "(无可见差异：编辑前后行序列一致。)"
        target.write_text(new_text, encoding="utf-8")
        return f"成功: 是\n路径: {path}\n---\n{diff_text}"
    except Exception as exc:
        return f"成功: 否\n路径: {path}\n错误: edit_file 失败: {exc}\n---\n"


@tool("search_file")
def search_file(
    path: str,
    keyword: str,
    index_offset: Optional[int] = 0,
    count_limit: Optional[int] = 5,
    context: OpenAIConversationContext | None = None,
) -> str:
    """查找关键字，按**命中顺序**分页展示（**`index_offset`/`count_limit`**）。
    可以通过调整index_offset和count_limit来调整展示的命中条数和偏移量以展示未被展示的内容。
    字段说明：
    - `path`：工作区内路径。
    - `keyword`：非空关键字子串。
    - `index_offset`：跳过前多少个命中（默认 0）；过大时**自动收紧**到仍可展示至少一条命中。
    - `count_limit`：本页最多展示多少处命中（默认 5，至少 1，**不超过全文件命中总数**）。

    逻辑：在全文件行内搜索；头部给出命中统计与前后是否仍有命中；每处命中带上下文行。
    """
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
        all_lines = _read_text_lines(target)
        total = len(all_lines)
        hit_indexes = [idx for idx, line in enumerate(all_lines) if needle in line]
        total_hits = len(hit_indexes)
        io_raw = DEFAULT_SEARCH_INDEX_OFFSET if index_offset is None else max(0, int(index_offset))
        cl_raw = DEFAULT_SEARCH_COUNT_LIMIT if count_limit is None else max(1, int(count_limit))

        base_header = [
            f"文件: {target}",
            f"关键字: {needle!r}",
            f"文件总行数: {total}",
            f"全文件命中数: {total_hits}",
        ]
        if total_hits > 0:
            # 本页条数至多 total_hits；起始偏移至多 total_hits-1，保证至少展示 1 条命中。
            cl = min(cl_raw, total_hits)
            io = min(io_raw, total_hits - 1)
            shown_hits = hit_indexes[io : io + cl]
            has_earlier = io > 0
            has_later = io + len(shown_hits) < total_hits
            page_desc = f"第 {io + 1}-{io + len(shown_hits)} 处"
        else:
            io = 0
            shown_hits = []
            has_earlier = False
            has_later = False
            page_desc = "无"

        blocks: list[str] = []
        for seq, hit_idx in enumerate(shown_hits, start=1):
            global_rank = io + seq
            start = max(hit_idx - DEFAULT_SEARCH_CONTEXT_LINES, 0)
            end = min(hit_idx + DEFAULT_SEARCH_CONTEXT_LINES + 1, total)
            blocks.append(
                f"命中#{global_rank}/{total_hits}: 行 {hit_idx + 1}（展示区间 {start + 1}-{end}）\n"
                + _line_numbered_text(all_lines[start:end], start + 1)
            )
        header = base_header + [
            f"本页命中: {page_desc} / 共 {total_hits}",
            f"前方是否还有命中: {'是' if has_earlier else '否'}",
            f"后方是否还有命中: {'是' if has_later else '否'}",
            "---",
        ]
        header_text = "\n".join(header)
        if not blocks:
            return header_text
        return "\n\n".join([header_text] + blocks)
    except Exception as exc:
        return f"ERROR: search_file 失败: {exc}"

