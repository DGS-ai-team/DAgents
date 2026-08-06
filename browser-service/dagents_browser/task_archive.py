"""浏览器任务过程归档：落盘详情供伴生 read_file，并维护最近 N 次索引。"""

from __future__ import annotations

import json
import time
from pathlib import Path
from typing import Any

RECENT_LIMIT = 3


def tasks_dir(agent_fs: str | Path) -> Path:
    d = Path(agent_fs) / "tasks"
    d.mkdir(parents=True, exist_ok=True)
    return d


def _rel_under_agent_fs(agent_fs: Path, path: Path) -> str:
    try:
        return str(path.relative_to(agent_fs)).replace("\\", "/")
    except ValueError:
        return str(path)


def archive_task(
    *,
    agent_fs: str | Path,
    task_id: str,
    task: str,
    status: str,
    result: dict[str, Any] | None,
    error: str | None = None,
    session_key: str = "",
    max_steps: int = 0,
) -> dict[str, Any]:
    """写入 tasks/{task_id}.json + .md，并更新 recent.json。返回归档元数据。"""
    root = Path(agent_fs)
    root.mkdir(parents=True, exist_ok=True)
    tdir = tasks_dir(root)
    result = dict(result or {})
    now = time.time()
    record: dict[str, Any] = {
        "task_id": task_id,
        "session_key": session_key,
        "task": task,
        "status": status,
        "max_steps": max_steps,
        "error": error,
        "archived_at": now,
        **{k: result.get(k) for k in (
            "summary",
            "final_result",
            "success",
            "done",
            "steps",
            "urls",
            "last_url",
            "screenshot_paths",
            "action_names",
            "errors",
            "has_errors",
            "duration_seconds",
            "step_trace",
        ) if k in result or result.get(k) is not None},
    }
    # 保证关键字段存在
    for k in ("summary", "success", "steps", "urls", "action_names", "errors", "step_trace"):
        record.setdefault(k, result.get(k))

    json_path = tdir / f"{task_id}.json"
    md_path = tdir / f"{task_id}.md"
    json_path.write_text(json.dumps(record, ensure_ascii=False, indent=2), encoding="utf-8")
    md_path.write_text(_render_task_markdown(record), encoding="utf-8")

    meta = {
        "task_id": task_id,
        "task": (task or "")[:200],
        "status": status,
        "summary": (record.get("summary") or "")[:300] if record.get("summary") else None,
        "success": record.get("success"),
        "steps": record.get("steps"),
        "detail_json": _rel_under_agent_fs(root, json_path),
        "detail_md": _rel_under_agent_fs(root, md_path),
        "archived_at": now,
    }
    _push_recent(tdir, meta)
    return meta


def _render_task_markdown(record: dict[str, Any]) -> str:
    lines = [
        f"# Browser 任务 {record.get('task_id')}",
        "",
        f"- 状态: `{record.get('status')}`",
        f"- 成功: `{record.get('success')}`",
        f"- 步数: `{record.get('steps')}`",
        "",
        "## 目标",
        "",
        str(record.get("task") or "(空)"),
        "",
        "## 结论",
        "",
        str(record.get("summary") or record.get("final_result") or "(无)"),
        "",
    ]
    urls = record.get("urls") or []
    if urls:
        lines += ["## 访问过的 URL", ""]
        for u in urls:
            lines.append(f"- {u}")
        lines.append("")
    actions = record.get("action_names") or []
    if actions:
        lines += ["## 动作序列", ""]
        for i, a in enumerate(actions, 1):
            lines.append(f"{i}. `{a}`")
        lines.append("")
    trace = record.get("step_trace") or []
    if trace:
        lines += ["## 过程摘要", ""]
        for item in trace:
            if isinstance(item, dict):
                step = item.get("step", "?")
                goal = item.get("next_goal") or item.get("goal") or ""
                acts = item.get("actions") or item.get("action_names") or []
                ev = item.get("evaluation") or ""
                lines.append(f"### Step {step}")
                if goal:
                    lines.append(f"- 目标: {goal}")
                if ev:
                    lines.append(f"- 评估: {ev}")
                if acts:
                    lines.append(f"- 动作: {', '.join(str(x) for x in acts)}")
                lines.append("")
            else:
                lines.append(f"- {item}")
        lines.append("")
    errs = record.get("errors") or []
    if errs:
        lines += ["## 错误", ""]
        for e in errs:
            lines.append(f"- {e}")
        lines.append("")
    if record.get("error"):
        lines += ["## 失败原因", "", str(record.get("error")), ""]
    return "\n".join(lines).rstrip() + "\n"


def _push_recent(tdir: Path, meta: dict[str, Any]) -> None:
    path = tdir / "recent.json"
    items: list[dict[str, Any]] = []
    if path.is_file():
        try:
            raw = json.loads(path.read_text(encoding="utf-8"))
            if isinstance(raw, list):
                items = [x for x in raw if isinstance(x, dict)]
        except Exception:
            items = []
    # 同 task_id 替换
    tid = meta.get("task_id")
    items = [x for x in items if x.get("task_id") != tid]
    items.insert(0, meta)
    items = items[:RECENT_LIMIT]
    path.write_text(json.dumps(items, ensure_ascii=False, indent=2), encoding="utf-8")


def load_recent_tasks(agent_fs: str | Path, *, limit: int = RECENT_LIMIT) -> list[dict[str, Any]]:
    path = Path(agent_fs) / "tasks" / "recent.json"
    if not path.is_file():
        return []
    try:
        raw = json.loads(path.read_text(encoding="utf-8"))
    except Exception:
        return []
    if not isinstance(raw, list):
        return []
    out = [x for x in raw if isinstance(x, dict)]
    return out[: max(1, limit)]


def format_recent_tasks_for_prompt(recent: list[dict[str, Any]]) -> str:
    """写入 extend_system_message 的最近任务引用块。"""
    if not recent:
        return (
            "<recent_browser_tasks>\n"
            "尚无历史任务。完成后会写入 tasks/ 目录；可用 read_file 阅读 tasks/<task_id>.md。\n"
            "</recent_browser_tasks>"
        )
    lines = [
        "<recent_browser_tasks>",
        f"以下为最近 {len(recent)} 次浏览器任务引用（详情用 read_file 打开对应路径，勿凭记忆编造）：",
    ]
    for i, item in enumerate(recent, 1):
        tid = item.get("task_id") or "?"
        goal = (item.get("task") or "").strip() or "(无目标)"
        if len(goal) > 80:
            goal = goal[:77] + "…"
        summary = (item.get("summary") or "").strip() or "(无摘要)"
        if len(summary) > 100:
            summary = summary[:97] + "…"
        status = item.get("status") or "?"
        success = item.get("success")
        succ = "成功" if success is True else ("失败" if success is False else "未知")
        md = item.get("detail_md") or f"tasks/{tid}.md"
        js = item.get("detail_json") or f"tasks/{tid}.json"
        lines.append(
            f"{i}. [{tid}] status={status} outcome={succ}\n"
            f"   目标: {goal}\n"
            f"   结论: {summary}\n"
            f"   详情(md): {md}\n"
            f"   详情(json): {js}"
        )
    lines.append("</recent_browser_tasks>")
    return "\n".join(lines)
