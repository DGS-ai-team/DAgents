"""文件工具（read/search_replace/search/write）。"""

from __future__ import annotations

import difflib
import json
import os
import re
from collections.abc import Iterator
from datetime import datetime
from pathlib import Path
from typing import Optional

from app.config.settings import get_settings
from app.context.models import OpenAIConversationContext
from app.harness.tools.tool import tool

DEFAULT_LINE_OFFSET = 1
DEFAULT_LINE_LIMIT = 100
DEFAULT_SEARCH_CONTEXT_LINES = 10
DEFAULT_SEARCH_INDEX_OFFSET = 0
DEFAULT_SEARCH_COUNT_LIMIT = 5
# 命中索引列表上限（超出仍统计总数，分页仅针对已记录索引）
MAX_SEARCH_HIT_INDEXES = 10_000
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


def _read_file_text(path: Path) -> str:
    """读取工作区内文本文件全文（与 `search_replace` 匹配口径一致）。

    逻辑：
    1. `.json` 先解析再 pretty-print，便于模型阅读与编辑；
    2. 其它允许后缀按 UTF-8 读入，非法字节替换；
    3. 不支持的后缀向上抛 `ValueError`。

    关键边界：
    - 返回字符串保留文件原有换行，不在此层强制追加末尾 `\\n`。
    """
    suffix = path.suffix.lower()
    if suffix == ".json":
        obj = json.loads(path.read_text(encoding="utf-8", errors="replace"))
        return json.dumps(obj, ensure_ascii=False, indent=2)
    if suffix in _TEXT_SUFFIXES:
        return path.read_text(encoding="utf-8", errors="replace")
    raise ValueError(f"不支持读取该后缀文件：{suffix or '<no-suffix>'}")


def _iter_file_lines(path: Path) -> Iterator[str]:
    """按行流式产出文件内容（不含行尾 `\\n`/`\\r`）。

    逻辑：
    1. 非 `.json`：逐行读盘，内存仅保留当前行；
    2. `.json`：为 pretty-print 仍一次性读入后再 `splitlines`（大 JSON 例外，见工具说明）。

    异常说明：
    - 不支持的后缀向上抛 `ValueError`。
    """
    suffix = path.suffix.lower()
    if suffix == ".json":
        for line in _read_file_text(path).splitlines():
            yield line
        return
    if suffix not in _TEXT_SUFFIXES:
        raise ValueError(f"不支持读取该后缀文件：{suffix or '<no-suffix>'}")
    with path.open(encoding="utf-8", errors="replace") as fh:
        for raw in fh:
            yield raw.rstrip("\r\n")


def _window_from_total(total: int, line_offset: int, line_limit: int) -> tuple[int, int]:
    """由总行数计算 0-based 行窗口 `[start, end)`（与 `_window_lines` 语义一致）。"""
    if total <= 0:
        return 0, 0
    if line_offset > 0:
        start = max(line_offset - 1, 0)
    else:
        start = max(total + line_offset, 0)
    end = min(start + line_limit, total)
    return start, end


def _read_line_window(path: Path, line_offset: int, line_limit: int) -> tuple[list[str], int, int, int]:
    """流式读取指定行窗口（避免为取 100 行而载入全文）。

    逻辑：
    1. 第一遍 `_iter_file_lines` 仅计数得 `total`；
    2. `_window_from_total` 得 `[start, end)`；
    3. 第二遍迭代，仅收集 `start <= idx < end` 的行，读到 `end` 即停止。

    返回：
    - `(window_lines, total, start, end)`，均为 0-based 窗口下标。
    """
    total = 0
    for _ in _iter_file_lines(path):
        total += 1
    start, end = _window_from_total(total, line_offset, line_limit)
    if start >= end:
        return [], total, start, end
    window: list[str] = []
    idx = 0
    for line in _iter_file_lines(path):
        if idx >= end:
            break
        if idx >= start:
            window.append(line)
        idx += 1
    return window, total, start, end


def _apply_max_bytes_to_body(
    body: str,
    byte_limit: int,
    *,
    truncate_hint: str,
) -> tuple[str, bool]:
    """按 UTF-8 字节上限截断正文；超限时追加说明行。

    返回 `(body, byte_truncated)`。
    """
    encoded = body.encode("utf-8")
    if len(encoded) <= byte_limit:
        return body, False
    clipped = encoded[:byte_limit].decode("utf-8", errors="replace")
    return clipped + f"\n\n[TRUNCATED] {truncate_hint}", True


def _fs_tool_read_max_bytes() -> int:
    """`read_file` 输出字节上限（来自 `Settings.fs_tool_read_max_bytes`）。"""
    return max(1, int(get_settings().fs_tool_read_max_bytes))


def _fs_tool_search_max_bytes() -> int:
    """`search_file` 输出字节上限（来自 `Settings.fs_tool_search_max_bytes`）。"""
    return max(1, int(get_settings().fs_tool_search_max_bytes))


def _apply_max_bytes_to_output(full_text: str, byte_limit: int) -> tuple[str, bool]:
    """截断整段工具输出（含头与正文），用于 `search_file`。"""
    encoded = full_text.encode("utf-8")
    if len(encoded) <= byte_limit:
        return full_text, False
    clipped = encoded[:byte_limit].decode("utf-8", errors="replace")
    return (
        clipped
        + f"\n\n[TRUNCATED] 输出超过 {byte_limit} bytes；请减小 count_limit 或缩小检索范围，"
        "并使用 next_index_offset 翻页。",
        True,
    )


def _unified_diff_body(old_text: str, new_text: str, *, path: str) -> str:
    """生成类 `diff -u` 的正文，供 `search_replace` 返回块使用。"""
    joined = "\n".join(
        difflib.unified_diff(
            old_text.splitlines(),
            new_text.splitlines(),
            fromfile=f"a/{path}",
            tofile=f"b/{path}",
            lineterm="",
        )
    )
    if joined.strip():
        return joined
    return "(无可见差异：编辑前后内容一致。)"


def _scan_regex_hits(path: Path, regex: re.Pattern[str]) -> tuple[list[int], int, int, bool]:
    """流式扫描命中行下标（0-based）。

    返回 `(stored_hit_indexes, total_hit_count, total_line_count, index_list_capped)`。
    - `stored_hit_indexes` 长度至多 `MAX_SEARCH_HIT_INDEXES`，供分页；
    - `total_hit_count` 为全文件真实命中次数；
    - `total_line_count` 为文件总行数（同次扫描得出，避免再读一遍）；
    - `index_list_capped` 为真表示命中过多，仅前若干下标可分页。
    """
    stored: list[int] = []
    total_hits = 0
    line_idx = 0
    for line in _iter_file_lines(path):
        if regex.search(line):
            total_hits += 1
            if len(stored) < MAX_SEARCH_HIT_INDEXES:
                stored.append(line_idx)
        line_idx += 1
    return stored, total_hits, line_idx, total_hits > len(stored)


def _merge_line_ranges(ranges: list[tuple[int, int]]) -> list[tuple[int, int]]:
    """合并重叠或相邻的半开区间 `[start, end)`。"""
    if not ranges:
        return []
    ordered = sorted(ranges, key=lambda item: item[0])
    merged: list[tuple[int, int]] = [ordered[0]]
    for start, end in ordered[1:]:
        prev_start, prev_end = merged[-1]
        if start <= prev_end:
            merged[-1] = (prev_start, max(prev_end, end))
        else:
            merged.append((start, end))
    return merged


def _load_lines_for_ranges(path: Path, ranges: list[tuple[int, int]]) -> dict[int, str]:
    """第二遍流式读盘，仅 materialize 落在合并区间内行。"""
    if not ranges:
        return {}
    max_end = max(end for _, end in ranges)
    lines: dict[int, str] = {}
    idx = 0
    for line in _iter_file_lines(path):
        if idx >= max_end:
            break
        for start, end in ranges:
            if start <= idx < end:
                lines[idx] = line
                break
        idx += 1
    return lines


def _format_search_blocks(
    *,
    shown_hits: list[int],
    hit_indexes: list[int],
    total_hits: int,
    line_map: dict[int, str],
    context_lines: int,
    total_file_lines: int,
) -> list[str]:
    """将本页命中格式化为合并上下文块（P1：相邻命中共享一段正文）。"""
    if not shown_hits:
        return []
    raw_ranges: list[tuple[int, int]] = []
    for hit_idx in shown_hits:
        start = max(hit_idx - context_lines, 0)
        end = min(hit_idx + context_lines + 1, total_file_lines)
        raw_ranges.append((start, end))
    merged = _merge_line_ranges(raw_ranges)
    blocks: list[str] = []
    for range_start, range_end in merged:
        hits_in_range = [h for h in shown_hits if range_start <= h < range_end]
        rank_parts: list[str] = []
        line_parts: list[str] = []
        for h in hits_in_range:
            global_rank = hit_indexes.index(h) + 1
            rank_parts.append(f"#{global_rank}")
            line_parts.append(str(h + 1))
        context_text = "\n".join(line_map.get(i, "") for i in range(range_start, range_end))
        read_offset = range_start + 1
        read_limit = max(1, range_end - range_start)
        blocks.append(
            f"命中 {'、'.join(rank_parts)}/{total_hits}（原文行 {', '.join(line_parts)}）\n"
            f"建议 read_file: line_offset={read_offset}, line_limit={read_limit}\n"
            f"上下文:\n{context_text}"
        )
    return blocks


@tool("read_file")
def read_file(
    path: str,
    line_offset: Optional[int] = 1,
    line_limit: Optional[int] = 100,
    context: OpenAIConversationContext | None = None,
) -> str:
    """使用场景：在 **`FS_ROOT`** 内按行窗口读取文本，供 **`search_replace`** 复制锚定文本；大文件用 **`line_offset`/`line_limit`** 分段读。

    字段说明：
    - `path`：工作区内路径（必填）。
    - `line_offset`：起始行（1-based，默认 1）；非正整数时从文件末尾倒数起算。
    - `line_limit`：最多读取行数（默认 100），**分页主参数**。

    返回说明：
    - 成功：元数据头（含 **`next_line_offset`**、行区间）+ `---` + **纯文本正文**（无行号前缀）。
    - 单页体积由部署配置 **`FS_TOOL_READ_MAX_BYTES`** 限制，触顶时请减小 `line_limit` 并用 **`next_line_offset`** 翻页。
    - 失败：`ERROR: ...`

    调用范例：
    - `read_file({"path":"app/foo.py"})`
    - `read_file({"path":"big.log","line_offset":101,"line_limit":80})`
    """
    try:
        del context
        target = _resolve_under_root(path)
        if not target.exists():
            return f"ERROR: 文件不存在：{path!r}"
        if target.is_dir():
            return f"ERROR: 目标是目录，无法读取：{path!r}"

        byte_limit = _fs_tool_read_max_bytes()
        offset = DEFAULT_LINE_OFFSET if line_offset is None else int(line_offset)
        count = DEFAULT_LINE_LIMIT if line_limit is None else max(1, int(line_limit))
        window_lines, total, start, end = _read_line_window(target, offset, count)
        has_more_after = end < total
        body = "\n".join(window_lines)
        body, byte_truncated = _apply_max_bytes_to_body(
            body,
            byte_limit,
            truncate_hint=(
                f"当前窗口超过配置上限 {byte_limit} bytes；请减小 line_limit，"
                f"下一页建议 line_offset={end + 1}（若后方仍有行）。"
            ),
        )
        st = target.stat()
        dt = datetime.fromtimestamp(st.st_mtime).astimezone()
        mtime_txt = f"{dt.isoformat(timespec='seconds')}（unix {st.st_mtime:.6f}）"
        next_line = str(end + 1) if has_more_after else "无"
        header = [
            f"文件修改时间: {mtime_txt}",
            f"文件总行数: {total}",
            f"本页行区间: {start + 1}-{end} / {total}",
            f"next_line_offset: {next_line}",
            f"后方是否还有未读取行: {'是' if has_more_after else '否'}",
            f"本页内容是否因体积上限截断: {'是' if byte_truncated else '否'}",
            "---",
        ]
        return "\n".join(header) + "\n" + body
    except Exception as exc:
        return f"ERROR: read_file 失败: {exc}"


@tool("write_file")
def write_file(
    path: str,
    content: str,
    context: OpenAIConversationContext | None = None,
) -> str:
    """使用场景：在 **`FS_ROOT`** 内整文件覆盖写入（新文件或大段重写）。

    字段说明：
    - `path`：工作区内路径（必填）。
    - `content`：写入全文（必填）。

    返回说明：
    - 成功：`OK: 已写入 ...`
    - 失败：`ERROR: ...`

    调用范例：
    - `write_file({"path":"notes.txt","content":"hello"})`
    """
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


@tool("search_replace")
def search_replace(
    path: str,
    old_string: str,
    new_string: str,
    replace_all: bool = False,
    context: OpenAIConversationContext | None = None,
) -> str:
    """使用场景：在 **`FS_ROOT`** 内用精确子串替换修改已有文本（改前请先 **`read_file`**）。

    字段说明：
    - `path`：工作区内路径（必填）。
    - `old_string`：须在磁盘文件中**精确出现**的片段（含空格与换行；勿带行号前缀）。
    - `new_string`：替换结果（可为空字符串表示删除该片段）。
    - `replace_all`：是否替换全部匹配（可选，默认 false）；为 false 时须**恰好 1 处**匹配。

    返回说明：
    - 成功：**`成功: 是`**、**`路径`**、**`---`** 后为 unified diff 正文。
    - 失败：**`成功: 否`** 与 **`错误:`**；正文为空。

    调用范例：
    - `search_replace({"path":"a.py","old_string":"foo","new_string":"bar"})`
    - `search_replace({"path":"a.py","old_string":"// TODO\\n","new_string":"","replace_all":true})`
    """
    try:
        del context
        target = _resolve_under_root(path)
        if not target.exists():
            return f"成功: 否\n路径: {path}\n错误: 文件不存在：{path!r}\n---\n"
        if target.is_dir():
            return f"成功: 否\n路径: {path}\n错误: 目标是目录，无法编辑：{path!r}\n---\n"
        if not old_string:
            return f"成功: 否\n路径: {path}\n错误: old_string 不能为空。\n---\n"

        raw_text = _read_file_text(target)
        hit_count = raw_text.count(old_string)
        if hit_count == 0:
            return (
                f"成功: 否\n路径: {path}\n"
                f"错误: 未找到 old_string（0 处匹配）；请 read_file 核对空白与换行。\n---\n"
            )
        if not replace_all and hit_count != 1:
            return (
                f"成功: 否\n路径: {path}\n"
                f"错误: old_string 匹配 {hit_count} 处；请扩大上下文或设 replace_all=true。\n---\n"
            )

        if replace_all:
            new_text = raw_text.replace(old_string, new_string)
            replaced = hit_count
        else:
            new_text = raw_text.replace(old_string, new_string, 1)
            replaced = 1

        target.write_text(new_text, encoding="utf-8")
        diff_text = _unified_diff_body(raw_text, new_text, path=path)
        head = f"成功: 是\n路径: {path}\n替换次数: {replaced}\n---\n{diff_text}"
        return head
    except Exception as exc:
        return f"成功: 否\n路径: {path}\n错误: search_replace 失败: {exc}\n---\n"


@tool("search_file")
def search_file(
    path: str,
    pattern: str,
    index_offset: Optional[int] = 0,
    count_limit: Optional[int] = 5,
    context: OpenAIConversationContext | None = None,
) -> str:
    """使用场景：在 **`FS_ROOT`** 内按**正则**逐行检索；分页返回命中及合并后的上下文（无行号前缀）。

    字段说明：
    - `path`：工作区内路径（必填）。
    - `pattern`：Python **`re`** 正则（必填，非空）。
    - `index_offset`：跳过前 N 个命中（可选，默认 0）。
    - `count_limit`：本页最多展示命中数（可选，默认 5）。

    返回说明：
    - 成功：元数据（含 **`next_index_offset`**）+ `---` + 命中块（含建议 **`read_file`** 参数）。
    - 整段体积由 **`FS_TOOL_SEARCH_MAX_BYTES`** 限制；触顶时请减小 `count_limit` 并用 **`next_index_offset`** 翻页。
    - 失败：`ERROR: ...`

    调用范例：
    - `search_file({"path":"a.py","pattern":"TODO"})`
    - `search_file({"path":"b.py","pattern":"TODO","index_offset":5,"count_limit":3})`
    """
    try:
        del context
        target = _resolve_under_root(path)
        if not target.exists():
            return f"ERROR: 文件不存在：{path!r}"
        if target.is_dir():
            return f"ERROR: 目标是目录，无法搜索：{path!r}"
        raw_pat = str(pattern or "").strip()
        if not raw_pat:
            return "ERROR: pattern 不能为空。"
        try:
            regex = re.compile(raw_pat)
        except re.error as exc:
            return f"ERROR: 正则无效: {exc}"

        hit_indexes, total_hits, total_file_lines, index_list_capped = _scan_regex_hits(target, regex)
        io_raw = DEFAULT_SEARCH_INDEX_OFFSET if index_offset is None else max(0, int(index_offset))
        cl_raw = DEFAULT_SEARCH_COUNT_LIMIT if count_limit is None else max(1, int(count_limit))
        byte_limit = _fs_tool_search_max_bytes()

        # 分页仅针对已存入 `hit_indexes` 的前若干下标（极端海量命中时见头字段说明）。
        pageable_total = len(hit_indexes)
        base_header = [
            f"文件: {target}",
            f"正则: {raw_pat!r}",
            f"全文件命中数: {total_hits}",
        ]
        if index_list_capped:
            base_header.append(
                f"命中索引列表: 仅保留前 {MAX_SEARCH_HIT_INDEXES} 处供 index_offset 分页；"
                "全文件命中数仍为上值。"
            )
        if pageable_total > 0:
            cl = min(cl_raw, pageable_total)
            io = min(io_raw, pageable_total - 1)
            shown_hits = hit_indexes[io : io + cl]
            has_earlier = io > 0
            has_later = io + len(shown_hits) < pageable_total
            page_desc = f"第 {io + 1}-{io + len(shown_hits)} 处"
            next_index = str(io + len(shown_hits)) if has_later else "无"
        else:
            io = 0
            shown_hits = []
            has_earlier = False
            has_later = False
            page_desc = "无"
            next_index = "无"

        blocks: list[str] = []
        if shown_hits:
            ctx = DEFAULT_SEARCH_CONTEXT_LINES
            raw_ranges = [
                (max(h - ctx, 0), min(h + ctx + 1, total_file_lines)) for h in shown_hits
            ]
            merged_ranges = _merge_line_ranges(raw_ranges)
            line_map = _load_lines_for_ranges(target, merged_ranges)
            blocks = _format_search_blocks(
                shown_hits=shown_hits,
                hit_indexes=hit_indexes,
                total_hits=total_hits,
                line_map=line_map,
                context_lines=ctx,
                total_file_lines=total_file_lines,
            )

        header = base_header + [
            f"本页命中: {page_desc} / 可分页 {pageable_total} 处",
            f"next_index_offset: {next_index}",
            f"前方是否还有命中: {'是' if has_earlier else '否'}",
            f"后方是否还有命中: {'是' if has_later else '否'}",
            "---",
        ]
        header_text = "\n".join(header)
        if not blocks:
            full_out = header_text
        else:
            full_out = "\n\n".join([header_text] + blocks)
        full_out, out_truncated = _apply_max_bytes_to_output(full_out, byte_limit)
        if out_truncated:
            full_out += f"\nnext_index_offset: {next_index}"
        return full_out
    except Exception as exc:
        return f"ERROR: search_file 失败: {exc}"
