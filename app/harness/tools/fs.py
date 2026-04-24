"""文件工具三件套（read/write/edit）。

当前版本提供可用的本地文件操作能力，并通过工作区根路径限制避免越界访问。
"""

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


def _replace_with_patch(text: str, patch: str) -> str:
    """按 patch 描述执行文本替换，返回替换后的内容。

    逻辑：
    1. 优先尝试 JSON patch：`{\"old\": ..., \"new\": ..., \"count\": ...}`；
    2. 若不是 JSON，则尝试分隔符格式：`<old>\\n===\\n<new>`；
    3. 校验 `old` 非空且必须命中，否则抛异常，避免误改整文件；
    4. 使用 `str.replace` 执行替换并返回结果。

    关键边界：
    - `old` 为空直接拒绝；
    - 未命中目标文本时拒绝，避免 silent failure。
    """
    old = ""
    new = ""
    count: int = -1

    try:
        maybe_json: Any = json.loads(patch)
        if isinstance(maybe_json, dict):
            old = str(maybe_json.get("old", ""))
            new = str(maybe_json.get("new", ""))
            raw_count = maybe_json.get("count", -1)
            count = int(raw_count) if raw_count is not None else -1
    except Exception:
        pass

    if not old and "\n===\n" in patch:
        old, new = patch.split("\n===\n", 1)

    if not old:
        raise ValueError("patch 格式无效：需要提供 old/new（JSON）或使用 '\\n===\\n' 分隔。")
    if old not in text:
        raise ValueError("未找到 old 文本，编辑已取消。")

    if count >= 0:
        return text.replace(old, new, count)
    return text.replace(old, new)


@tool("fs_read")
def fs_read(
    path: str,
    max_bytes: Optional[int] = 3000,
    line_offset: Optional[int] = 1,
    line_limit: Optional[int] = 100,
    context: OpenAIConversationContext | None = None,
) -> str:
    """使用场景：读取工作区内文本文件内容，支持整段读取或按行窗口读取。

    字段说明：
    - `path`：文件路径（必填，必须位于 `FS_ROOT` 内）。
    - `max_bytes`：返回大小上限（可选，默认 3000，最小按 1 处理）。
    - `line_offset`：按行起始位置（可选，默认 1；负数表示从末尾倒数）。
    - `line_limit`：按行读取数量（可选，默认 100，最小按 1 处理）。

    返回说明：
    - 成功：返回文件内容；按行模式会附带 `[LINES]` 头。
    - 失败：返回 `ERROR: ...`。

    调用范例：
    - `fs_read({"path":"README.md"})`
    - `fs_read({"path":"app/main.py","line_offset":1,"line_limit":50})`
    """
    try:
        target = _resolve_under_root(path)
        if not target.exists():
            return f"ERROR: 文件不存在：{path!r}"
        if target.is_dir():
            return f"ERROR: 目标是目录，无法读取：{path!r}"

        limit = DEFAULT_MAX_READ_BYTES if max_bytes is None else max(1, int(max_bytes))

        if line_offset is not None or line_limit is not None:
            all_lines = target.read_text(encoding="utf-8", errors="replace").splitlines()
            total = len(all_lines)
            offset = DEFAULT_LINE_OFFSET if line_offset is None else int(line_offset)
            count = DEFAULT_LINE_LIMIT if line_limit is None else max(1, int(line_limit))

            if offset > 0:
                start = offset - 1
            else:
                start = max(total + offset, 0)
            end = min(start + count, total)

            window = all_lines[start:end]
            numbered_lines = [
                f"{start + idx + 1}|{line}"
                for idx, line in enumerate(window)
            ]
            text = "\n".join(numbered_lines)
            text = f"[LINES] total={total}, start={start + 1}, end={end}\n{text}"
            data = text.encode("utf-8")
            if len(data) > limit:
                text = data[:limit].decode("utf-8", errors="replace")
                text += f"\n\n[TRUNCATED] 行模式结果超过 {limit} bytes，已截断。"
            return text

        data = target.read_bytes()
        truncated = len(data) > limit
        text = data[:limit].decode("utf-8", errors="replace")
        if truncated:
            text += f"\n\n[TRUNCATED] 仅返回前 {limit} bytes。"
        return text
    except Exception as exc:
        return f"ERROR: fs_read 失败: {exc}"


@tool("fs_write")
def fs_write(
    path: str,
    content: str,
    overwrite: bool = True,
    context: OpenAIConversationContext | None = None,
) -> str:
    """使用场景：在工作区内新建或覆盖文本文件。

    字段说明：
    - `path`：目标文件路径（必填，必须位于 `FS_ROOT` 内）。
    - `content`：写入内容（必填，按 UTF-8 写入）。
    - `overwrite`：是否允许覆盖（可选，默认 `True`）。

    返回说明：
    - 成功：返回 `OK: ...`，包含写入字节数。
    - 失败：返回 `ERROR: ...`。

    调用范例：
    - `fs_write({"path":"tmp/a.txt","content":"hello"})`
    - `fs_write({"path":"config.json","content":"{}","overwrite":false})`
    """
    try:
        target = _resolve_under_root(path)
        target.parent.mkdir(parents=True, exist_ok=True)

        if target.exists() and target.is_dir():
            return f"ERROR: 目标是目录，无法写入：{path!r}"
        if target.exists() and not overwrite:
            return f"ERROR: 文件已存在（overwrite=False）：{path!r}"

        target.write_text(content, encoding="utf-8")
        return f"OK: 已写入 {path!r} ({len(content.encode('utf-8'))} bytes)"
    except Exception as exc:
        return f"ERROR: fs_write 失败: {exc}"


@tool("fs_edit")
def fs_edit(path: str, patch: str, context: OpenAIConversationContext | None = None) -> str:
    """使用场景：对工作区文本文件做基于旧文本匹配的精确替换。

    字段说明：
    - `path`：文件路径（必填，必须位于 `FS_ROOT` 内）。
    - `patch`：替换规则（必填），支持：
      1) JSON：`{"old":"旧文本","new":"新文本","count":1}`
      2) 分隔符：`旧文本\\n===\\n新文本`

    返回说明：
    - 成功：返回 `OK: ...`，含修改前后长度。
    - 失败：返回 `ERROR: ...`。

    调用范例：
    - `fs_edit({"path":"a.txt","patch":"旧\\n===\\n新"})`
    - `fs_edit({"path":"a.txt","patch":"{\\"old\\":\\"x\\",\\"new\\":\\"y\\",\\"count\\":1}"})`
    """
    try:
        target = _resolve_under_root(path)
        if not target.exists():
            return f"ERROR: 文件不存在：{path!r}"
        if target.is_dir():
            return f"ERROR: 目标是目录，无法编辑：{path!r}"

        old_text = target.read_text(encoding="utf-8")
        new_text = _replace_with_patch(old_text, patch)
        target.write_text(new_text, encoding="utf-8")
        return f"OK: 已编辑 {path!r}（长度 {len(old_text)} -> {len(new_text)}）"
    except Exception as exc:
        return f"ERROR: fs_edit 失败: {exc}"

