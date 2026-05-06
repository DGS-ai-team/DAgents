"""Agent 间交互工具：发现（含卡片信息）、点对点发送、广播、工具审批与任务查询。"""

from __future__ import annotations

import asyncio
import json
import time
import uuid
from typing import Any, Literal
from urllib.parse import urljoin, urlparse

import httpx
from pydantic import BaseModel, ConfigDict, Field

from app.context.models import OpenAIConversationContext
from app.config.settings import get_settings
from app.schemas.agent_peer import (
    AgentPeerError,
    AgentPeerTask,
    AgentPeerTaskState,
    build_agent_peer_envelope,
)
from app.harness.tools.tool import tool

_DEFAULT_HTTP_TIMEOUT_SECONDS = 15.0
_AGENT_LIST_CACHE: list[dict[str, Any]] = []
_AGENT_LIST_CACHE_UPDATED_AT_UNIX_MS = 0


# --------------------------------------------------------------------------- #
# 内部数据模型：远端 SSE 汇总结构                                              #
# --------------------------------------------------------------------------- #


class PeerApprovalEntry(BaseModel):
    """对端 `approval_required` 事件的结构化条目。"""

    model_config = ConfigDict(extra="allow")

    target_session_id: str = Field(description="对端会话 ID（用于审批 resume 路由）。")
    approval_id: str | None = Field(default=None, description="审批批次 ID。")
    approval_type: str = Field(default="execute_tool", description="审批类型。")
    content: str = Field(default="", description="对端审批提示文本。")
    description: str = Field(default="", description="审批描述。")
    approval_args: dict[str, Any] = Field(
        default_factory=dict,
        description="审批参数（含 `tool_calls` 列表，每项 `id/name/arguments/raw_arguments`）。",
    )


class PeerStreamSummary(BaseModel):
    """单次远端 SSE 拉取的汇总结构。"""

    model_config = ConfigDict(extra="allow")

    text: str = Field(default="", description="对 Agent 可读的拼接正文。")
    approvals: list[PeerApprovalEntry] = Field(
        default_factory=list,
        description="对端 `approval_required` 事件汇总；存在即表示对端在等待审批。",
    )
    errors: list[str] = Field(default_factory=list, description="对端 `error` 事件文本列表。")
    final_state: Literal["succeeded", "requires_input", "failed", "truncated"] = Field(
        default="succeeded",
        description="对本次拉取的最终态判定（按事件序与超时综合得出）。",
    )
    truncated: bool = Field(default=False, description="是否因超时截断。")


def _session_id_from_context(context: OpenAIConversationContext | None, fallback_prefix: str) -> str:
    """从工具上下文提取调用方会话 ID，缺失时回退到前缀+随机值。

    逻辑：
    1. 有 context 且 `session_id` 非空时直接复用；
    2. 否则生成 `${fallback_prefix}-${uuid}`，保证工具独立可运行。
    """
    if context is not None and (context.session_id or "").strip():
        return context.session_id.strip()
    return f"{fallback_prefix}-{uuid.uuid4().hex[:8]}"


def _new_peer_session_id(*, caller_session_id: str, target_agent_id: str) -> str:
    """为单次点对点请求生成隔离的对端会话 ID。

    逻辑：
    1. 以 `peer-<caller>-<target>-<short>` 命名，避免对端把多个调用方的请求混入同一会话；
    2. 如果调用方 session_id 缺失，仅用 `peer-<target>-<short>` 兜底；
    3. 短随机段使用 `uuid4` 前 10 位，足以避免冲突。
    """
    short = uuid.uuid4().hex[:10]
    safe_caller = (caller_session_id or "").strip() or "anon"
    safe_target = (target_agent_id or "").strip() or "unknown"
    return f"peer-{safe_caller}-{safe_target}-{short}"


def _cache_agent_list(agents: list[dict[str, Any]]) -> None:
    """更新进程内 agent 列表缓存。

    逻辑：
    1. 以 `agent_id` 去重，忽略无效项；
    2. 用当前发现结果整体替换缓存，避免旧数据残留；
    3. 按 `agent_id` 排序，保证后续匹配可预测。
    """
    by_id: dict[str, dict[str, Any]] = {}
    for item in agents:
        if not isinstance(item, dict):
            continue
        agent_id = str(item.get("agent_id") or "").strip()
        if not agent_id:
            continue
        by_id[agent_id] = dict(item)
    _AGENT_LIST_CACHE.clear()
    _AGENT_LIST_CACHE.extend(sorted(by_id.values(), key=lambda v: str(v.get("agent_id") or "")))
    # 缓存更新时间用于 TTL 失效判断，避免长期使用过期目录。
    global _AGENT_LIST_CACHE_UPDATED_AT_UNIX_MS
    _AGENT_LIST_CACHE_UPDATED_AT_UNIX_MS = int(time.time() * 1000)


def _is_agent_list_cache_stale() -> bool:
    """判断 agent 列表缓存是否过期。

    逻辑：
    1. 缓存为空时直接视为过期，触发首次回源；
    2. 读取当前时间与缓存更新时间；
    3. 与 TTL 阈值比较并返回是否过期。

    关键分支/边界：
    - TTL 最低按 1 秒处理，避免配置异常导致永不过期。
    """
    if not _AGENT_LIST_CACHE:
        return True
    settings = get_settings()
    ttl_seconds = max(1, int(settings.agent_peer_cache_ttl_seconds))
    now_ms = int(time.time() * 1000)
    ttl_ms = ttl_seconds * 1000
    return (now_ms - _AGENT_LIST_CACHE_UPDATED_AT_UNIX_MS) >= ttl_ms


def _refresh_agent_list_for_visible_groups(visible_groups: list[str]) -> list[dict[str, Any]]:
    """按可见分组回源刷新 agent 列表缓存。

    逻辑：
    1. 调用注册中心查询当前可见分组的 Agent 列表；
    2. 使用 `_cache_agent_list` 覆盖进程内缓存与时间戳；
    3. 返回本次回源结果供调用方继续匹配目标 Agent。
    """
    agents = _discover_agents_by_groups(visible_groups)
    _cache_agent_list(agents)
    return agents


def _resolve_target_agent_from_cache(target_agent_id: str, visible_groups: list[str]) -> dict[str, Any] | None:
    """在进程内缓存中解析目标 Agent（命中则避免网络请求）。

    逻辑：
    1. 遍历缓存并按 `agent_id` 精确匹配；
    2. 校验目标 `discovery_group` 与当前可见分组有交集；
    3. 命中则返回该记录，未命中返回 `None`。
    """
    target = target_agent_id.strip()
    visible = set(_stable_groups(visible_groups))
    if not target or not visible:
        return None
    for item in _AGENT_LIST_CACHE:
        if str(item.get("agent_id") or "").strip() != target:
            continue
        groups = _stable_groups(item.get("discovery_group") or [])
        if any(g in visible for g in groups):
            return item
    return None


def _clear_agent_list_cache() -> None:
    """清空 agent 列表缓存（供测试隔离使用）。"""
    _AGENT_LIST_CACHE.clear()
    global _AGENT_LIST_CACHE_UPDATED_AT_UNIX_MS
    _AGENT_LIST_CACHE_UPDATED_AT_UNIX_MS = 0


def _json_text(payload: dict[str, Any]) -> str:
    """将字典转换为稳定 JSON 文本（紧凑、UTF-8 友好）。"""

    return json.dumps(payload, ensure_ascii=False)


def _stable_groups(raw_groups: list[str] | None) -> list[str]:
    """规范化分组列表：去重、去空白、保留首次出现顺序。"""

    if not raw_groups:
        return []
    seen: set[str] = set()
    result: list[str] = []
    for item in raw_groups:
        cleaned = (item or "").strip()
        if not cleaned or cleaned in seen:
            continue
        seen.add(cleaned)
        result.append(cleaned)
    return result


def _build_error_envelope_text(
    *,
    intent: str,
    session_id: str,
    target_agent_id: str | None,
    target_groups: list[str] | None,
    message: str,
    code: str,
    retryable: bool,
    trace_id: str | None = None,
) -> str:
    """构造失败态 AgentPeer 信封并序列化为文本。

    逻辑：
    1. 使用统一 `build_agent_peer_envelope` 组装错误对象；
    2. 在 `payload` 中附带可读失败摘要；
    3. 返回 JSON 字符串供 runtime 透传到 `tool_result`。
    """

    s = get_settings()
    env = build_agent_peer_envelope(
        caller_agent_id=s.agent_id,
        caller_session_id=session_id,
        caller_groups=s.discovery_groups,
        target_agent_id=target_agent_id,
        target_groups=target_groups,
        intent=intent,  # type: ignore[arg-type]
        payload_content={"ok": False, "message": message},
        payload_content_type="application/json",
        trace_id=trace_id,
        error=AgentPeerError(code=code, message=message, retryable=retryable),
    )
    return _json_text(env.model_dump())


def _resolve_registry_url() -> str:
    """读取并校验 Register Center 地址。"""

    s = get_settings()
    return (s.registry_url or "").strip().rstrip("/")


def _require_registry_url() -> str:
    """获取必需的 Register Center 地址；未配置时抛 `ValueError`。"""

    registry_url = _resolve_registry_url()
    if not registry_url:
        raise ValueError("未配置 REGISTRY_URL，无法进行 Agent 发现与转发")
    return registry_url


def _discover_agents_by_groups(groups: list[str]) -> list[dict[str, Any]]:
    """按分组查询 Register Center 并聚合去重。"""

    final_groups = _stable_groups(groups)
    if not final_groups:
        return []
    registry_url = _require_registry_url()
    by_id: dict[str, dict[str, Any]] = {}
    with httpx.Client(timeout=_DEFAULT_HTTP_TIMEOUT_SECONDS) as client:
        for group_id in final_groups:
            resp = client.get(f"{registry_url}/v1/agents", params={"discovery_group": group_id})
            resp.raise_for_status()
            body = resp.json()
            for item in body.get("agents", []):
                agent_id = str(item.get("agent_id") or "").strip()
                if not agent_id:
                    continue
                by_id[agent_id] = item
    return sorted(by_id.values(), key=lambda item: str(item.get("agent_id") or ""))


def _approval_entry_from_event(*, target_session_id: str, data: dict[str, Any]) -> PeerApprovalEntry:
    """从 `approval_required` SSE `data` 字段构造 `PeerApprovalEntry`。"""
    raw_args = data.get("approval_args")
    safe_args = raw_args if isinstance(raw_args, dict) else {}
    return PeerApprovalEntry(
        target_session_id=target_session_id,
        approval_id=(str(data.get("approval_id")) if data.get("approval_id") is not None else None),
        approval_type=str(data.get("approval_type") or "execute_tool"),
        content=str(data.get("content") or ""),
        description=str(data.get("description") or ""),
        approval_args=dict(safe_args),
    )


async def _collect_peer_stream_summary(
    *,
    base_url: str,
    client_id: str,
    session_id: str,
    timeout_seconds: float,
) -> PeerStreamSummary:
    """拉取远端 SSE 并汇总文本/审批/错误/终态。

    逻辑：
    1. 连接 `{base_url}/v1/streams?client_id=...` 订阅事件；
    2. 解析 `event/data` 并仅消费目标 `session_id` 的事件；
    3. 文本类（`assistant`/`reasoning`/`tool_result`）拼接到 `text`，`error` 写入 `errors`；
    4. `approval_required` 转结构化 `PeerApprovalEntry` 写入 `approvals`；
    5. `done` 终止；超时返回 `truncated=True`。

    关键分支/边界：
    - `client_id`/`session_id` 任一为空：直接返回空 summary；
    - 网络异常：以 `error` 事件文本形式记入 `errors` 并视为 `failed`；
    - 终态判定优先级：`approvals` 非空 → `requires_input`；否则 `errors` 非空 → `failed`；
      超时 → `truncated`；其余 → `succeeded`。
    """
    final_base_url = base_url.strip().rstrip("/")
    final_client_id = client_id.strip()
    final_session_id = session_id.strip()
    summary = PeerStreamSummary()
    if not final_base_url or not final_client_id or not final_session_id:
        return summary
    text_lines: list[str] = []
    event_name = ""
    data_lines: list[str] = []
    received_done = False
    try:
        async with asyncio.timeout(max(1.0, timeout_seconds)):
            async with httpx.AsyncClient(timeout=None) as client:
                stream_url = f"{final_base_url}/v1/streams?client_id={final_client_id}"
                async with client.stream("GET", stream_url) as resp:
                    resp.raise_for_status()
                    async for line in resp.aiter_lines():
                        if line.startswith("event:"):
                            event_name = line[len("event:") :].strip()
                            continue
                        if line.startswith("data:"):
                            data_lines.append(line[len("data:") :].lstrip())
                            continue
                        if line != "":
                            continue
                        if not data_lines:
                            event_name = ""
                            continue
                        raw_data = "\n".join(data_lines)
                        try:
                            payload = json.loads(raw_data)
                        except Exception:
                            payload = {}
                        if str(payload.get("session_id", "")).strip() != final_session_id:
                            event_name = ""
                            data_lines = []
                            continue
                        stream_event_name = event_name or str(payload.get("type", "") or "")
                        data = payload.get("data", {})
                        if not isinstance(data, dict):
                            data = {}
                        if stream_event_name in {"assistant", "reasoning", "tool_result"}:
                            piece = str(data.get("content", "") or "").strip()
                            if piece:
                                text_lines.append(piece)
                        elif stream_event_name == "error":
                            err_msg = str(data.get("message", "") or "").strip()
                            if err_msg:
                                summary.errors.append(err_msg)
                                text_lines.append(f"[ERROR] {err_msg}")
                        elif stream_event_name == "approval_required":
                            summary.approvals.append(
                                _approval_entry_from_event(
                                    target_session_id=final_session_id,
                                    data=data,
                                )
                            )
                        elif stream_event_name == "done":
                            received_done = True
                            event_name = ""
                            data_lines = []
                            break
                        event_name = ""
                        data_lines = []
        summary.text = "\n".join(text_lines)
    except TimeoutError:
        # 广播/跨 Agent 场景允许截断返回，保证工具在 SLA 内可收口。
        summary.text = "\n".join(text_lines)
        summary.truncated = True
    except Exception as exc:
        err_text = str(exc).strip() or "读取远端流失败"
        summary.errors.append(err_text)
        text_lines.append(f"[ERROR] {err_text}")
        summary.text = "\n".join(text_lines)

    if summary.truncated:
        summary.final_state = "truncated"
    elif summary.approvals:
        # 审批未决直接判定 `requires_input`；不论是否同时存在 `done`/正文，调用方都需要先处理审批。
        summary.final_state = "requires_input"
    elif summary.errors:
        summary.final_state = "failed"
    elif received_done:
        summary.final_state = "succeeded"
    else:
        # 未 done 也未截断（极端情况，比如远端流提前关闭），保守判 `failed`。
        summary.final_state = "failed"
    return summary


def _peer_state_to_task_state(final_state: str) -> AgentPeerTaskState:
    """把 `PeerStreamSummary.final_state` 映射到 `AgentPeerTaskState`。"""
    if final_state == "succeeded":
        return "succeeded"
    if final_state == "requires_input":
        return "requires_input"
    if final_state == "truncated":
        return "running"
    return "failed"


def _attach_agent_card_summary(agent: dict[str, Any]) -> dict[str, Any]:
    """为单个发现结果补充 Agent Card 摘要字段。

    逻辑：
    1. 从 `base_url` 生成 `/.well-known/agent-card.json` 地址；
    2. 发起 HTTP 请求拉取 card JSON；
    3. 将固定结构 `agent_card` 写回返回项（含访问 URL、端口、card URL、card 内容、错误字段）。
    """
    enriched = dict(agent)
    base = str(enriched.get("base_url") or "").strip().rstrip("/")
    parsed = urlparse(base if "://" in base else f"http://{base}") if base else None
    if parsed is None:
        access_host = ""
        access_port = None
    else:
        access_host = (parsed.hostname or "").strip()
        # 未显式端口时按协议补默认值，保证字段恒定可解析。
        if parsed.port is not None:
            access_port = parsed.port
        elif parsed.scheme == "https":
            access_port = 443
        elif parsed.scheme == "http":
            access_port = 80
        else:
            access_port = None
    card_url = urljoin(f"{base}/", ".well-known/agent-card.json") if base else ""
    card_info: dict[str, Any] = {
        "access_url": base or None,
        "access_host": access_host or None,
        "access_port": access_port,
        "card_url": card_url or None,
        "card_payload": None,
        "error": None,
    }
    if not base:
        card_info["error"] = "base_url 为空，无法读取 agent card"
        enriched["agent_card"] = card_info
        return enriched
    try:
        with httpx.Client(timeout=_DEFAULT_HTTP_TIMEOUT_SECONDS) as client:
            resp = client.get(card_url)
            resp.raise_for_status()
            card = resp.json()
        card_info["card_payload"] = card
        card_info["error"] = None
        enriched["agent_card"] = card_info
        return enriched
    except Exception as exc:
        card_info["card_payload"] = None
        card_info["error"] = str(exc)
        enriched["agent_card"] = card_info
        return enriched


def _resolve_target_agent(target_agent_id: str) -> dict[str, Any]:
    """在调用方可见分组范围内解析目标 Agent。"""

    s = get_settings()
    visible_groups = _stable_groups(s.discovery_groups)
    if not visible_groups:
        raise ValueError("未配置 DISCOVERY_GROUPS，无法解析目标 Agent")
    if _is_agent_list_cache_stale():
        try:
            _refresh_agent_list_for_visible_groups(visible_groups)
        except Exception:
            # 刷新失败时保留旧缓存兜底，避免短时目录抖动导致全量不可用。
            pass
    cached = _resolve_target_agent_from_cache(target_agent_id, visible_groups)
    if cached is not None:
        return cached
    agents = _refresh_agent_list_for_visible_groups(visible_groups)
    for item in agents:
        if str(item.get("agent_id") or "").strip() == target_agent_id.strip():
            return item
    raise ValueError(f"在当前可见分组内未找到目标 Agent: {target_agent_id!r}")


def _build_resume_value(
    *,
    decision: str,
    approved_call_ids: list[str] | None,
    rejected_call_ids: list[str] | None,
) -> dict[str, Any]:
    """根据审批决策构造与 `app.schemas.approval` 对齐的 `resume_value`。

    逻辑：
    1. `approve` → `{type:"approve"}`：批准当前 pending 全部工具；
    2. `reject` → `{type:"reject"}`：拒绝全部并写占位 tool；
    3. `selection` → `{type:"selection", approved, rejected}`：逐条决策。

    关键边界：
    - `selection` 时 `approved`/`rejected` 必须至少有一个非空，否则视为非法决策；
    - 输入清单去空白与去重，避免同一 call_id 出现在两个列表中导致歧义。
    """
    final_decision = (decision or "").strip().lower()
    if final_decision == "approve":
        return {"type": "approve"}
    if final_decision == "reject":
        return {"type": "reject"}
    if final_decision == "selection":
        approved = sorted({(c or "").strip() for c in (approved_call_ids or []) if (c or "").strip()})
        rejected = sorted({(c or "").strip() for c in (rejected_call_ids or []) if (c or "").strip()})
        overlap = set(approved) & set(rejected)
        if overlap:
            raise ValueError(f"审批决策中 approved/rejected 不能有重叠 call_id：{sorted(overlap)!r}")
        if not approved and not rejected:
            raise ValueError("selection 决策必须至少在 approved 或 rejected 中提供一个 call_id")
        return {"type": "selection", "approved": approved, "rejected": rejected}
    raise ValueError(f"不支持的审批 decision: {decision!r}（仅支持 approve/reject/selection）")


@tool("agent_discover")
def agent_discover(
    context: OpenAIConversationContext | None = None,
) -> str:
    """使用场景：发现与当前 Agent 同组的可协作 Agent，并附带每个 Agent 的 card 摘要；不用于发送消息本身。

    字段说明：
    - 无业务入参：分组范围固定取当前 Agent 配置 `DISCOVERY_GROUPS`。

    返回说明：
    - 成功：返回 JSON 文本，正文包含 `ok/requested_groups/agents`。
    - 失败：返回 JSON 文本，正文包含失败原因（`ok=false` 与错误信息）。

    调用范例：
    - `agent_discover({})`
    """

    s = get_settings()
    session_id = _session_id_from_context(context, "discover")
    groups = _stable_groups(s.discovery_groups)
    if not groups:
        return _build_error_envelope_text(
            intent="ask",
            session_id=session_id,
            target_agent_id=None,
            target_groups=["invalid-group"],
            message="discovery_groups 不能为空",
            code="invalid_groups",
            retryable=False,
        )
    try:
        discovered_agents = _discover_agents_by_groups(groups)
        _cache_agent_list(discovered_agents)
        agents = list(discovered_agents)
        enriched_agents: list[dict[str, Any]] = []
        for item in agents:
            enriched_agents.append(_attach_agent_card_summary(item))
        agents = enriched_agents
        env = build_agent_peer_envelope(
            caller_agent_id=s.agent_id,
            caller_session_id=session_id,
            caller_groups=s.discovery_groups,
            target_agent_id=None,
            target_groups=groups,
            intent="ask",
            payload_content={
                "ok": True,
                "requested_groups": groups,
                "agents": agents,
            },
            payload_content_type="application/json",
        )
        return _json_text(env.model_dump())
    except Exception as exc:
        return _build_error_envelope_text(
            intent="ask",
            session_id=session_id,
            target_agent_id=None,
            target_groups=groups,
            message=str(exc),
            code="discover_failed",
            retryable=True,
        )


@tool("agent_send_message")
async def agent_send_message(
    target_agent_id: str,
    message: str,
    context: OpenAIConversationContext | None = None,
) -> str:
    """使用场景：向指定 Agent 发起点对点委托请求；返回对端流式输出与可能的工具审批摘要。

    字段说明：
    - `target_agent_id`：目标 Agent ID（必填）。
    - `message`：发送给目标 Agent 的消息正文（必填）。

    返回说明：
    - 成功（对端正常 done）：返回 JSON 文本，`task.state="succeeded"`，`payload.content` 含 `target_session_id/stream_output`。
    - 等待审批（对端 `approval_required`）：返回 JSON 文本，`task.state="requires_input"`；`payload.content.approvals[]` 列出每个待审批批次（含 `target_session_id/approval_id/approval_args.tool_calls`）。后续应调用 **`agent_peer_approve_tools`** 逐批做出决策。
    - 失败：`task.state="failed"`，`error.code` 标注失败类别。

    调用范例：
    - `agent_send_message({"target_agent_id":"agent-b","message":"请总结日报"})`
    """

    s = get_settings()
    msg = message.strip()
    target_id = target_agent_id.strip()
    final_delivery_mode = (s.agent_peer_delivery_mode or "direct").strip().lower()
    caller_session_id = _session_id_from_context(context, f"peer-{s.agent_id}")
    # 对端会话独立命名，避免把调用方会话 id 复用到对端导致跨会话串扰。
    peer_session_id = _new_peer_session_id(
        caller_session_id=caller_session_id,
        target_agent_id=target_id or "unknown",
    )
    trace_id = f"trace-{uuid.uuid4().hex}"
    if not target_id:
        return _build_error_envelope_text(
            intent="delegate",
            session_id=caller_session_id,
            target_agent_id="",
            target_groups=None,
            message="target_agent_id 不能为空",
            code="invalid_target_agent",
            retryable=False,
            trace_id=trace_id,
        )
    if not msg:
        return _build_error_envelope_text(
            intent="delegate",
            session_id=caller_session_id,
            target_agent_id=target_id,
            target_groups=None,
            message="message 不能为空",
            code="invalid_message",
            retryable=False,
            trace_id=trace_id,
        )
    if final_delivery_mode not in {"direct", "relay"}:
        return _build_error_envelope_text(
            intent="delegate",
            session_id=caller_session_id,
            target_agent_id=target_id or "",
            target_groups=None,
            message="配置 AGENT_PEER_DELIVERY_MODE 仅支持 direct 或 relay",
            code="invalid_delivery_mode_config",
            retryable=False,
            trace_id=trace_id,
        )
    try:
        req_env = build_agent_peer_envelope(
            caller_agent_id=s.agent_id,
            caller_session_id=caller_session_id,
            caller_groups=s.discovery_groups,
            target_agent_id=target_id,
            target_groups=None,
            intent="delegate",
            payload_content=msg,
            payload_content_type="text/plain",
            trace_id=trace_id,
        )
        peer_client_id = f"peer-{uuid.uuid4().hex}"
        body = {
            "session_id": peer_session_id,
            "client_id": peer_client_id,
            "request_type": "message",
            "content": _json_text(req_env.model_dump()),
            "source": "agent-peer",
            "priority": "human",
        }
        target_base_url = ""
        async with httpx.AsyncClient(timeout=_DEFAULT_HTTP_TIMEOUT_SECONDS) as client:
            if final_delivery_mode == "direct":
                target = _resolve_target_agent(target_id)
                target_base_url = str(target.get("base_url") or "").strip().rstrip("/")
                resp = await client.post(f"{target_base_url}/v1/messages", json=body)
            else:
                registry_url = _require_registry_url()
                relay_payload = {
                    "target_agent_id": target_id,
                    "caller_groups": _stable_groups(s.discovery_groups),
                    "session_id": peer_session_id,
                    "client_id": peer_client_id,
                    "request_type": "message",
                    "content": _json_text(req_env.model_dump()),
                    "source": "agent-peer-relay",
                    "priority": "human",
                }
                resp = await client.post(f"{registry_url}/v1/relay", json=relay_payload)
            resp.raise_for_status()
            submit = resp.json()
            if final_delivery_mode == "relay":
                target_base_url = str(submit.get("target_base_url") or "").strip().rstrip("/")
        accepted = bool(submit.get("accepted", False))
        if not accepted:
            raise ValueError("目标 Agent 未确认入队（accepted=false）")
        summary = await _collect_peer_stream_summary(
            base_url=target_base_url,
            client_id=peer_client_id,
            session_id=peer_session_id,
            timeout_seconds=float(max(1, int(s.agent_peer_stream_timeout_seconds))),
        )
        ack_env = build_agent_peer_envelope(
            caller_agent_id=s.agent_id,
            caller_session_id=caller_session_id,
            caller_groups=s.discovery_groups,
            target_agent_id=target_id,
            target_groups=None,
            intent="delegate",
            payload_content={
                "ok": summary.final_state in {"succeeded", "requires_input"},
                "target_agent_id": target_id,
                "target_base_url": target_base_url,
                "target_session_id": peer_session_id,
                "target_client_id": peer_client_id,
                "submit": submit,
                "stream_output": summary.text,
                "stream_output_truncated": summary.truncated,
                "approvals": [item.model_dump() for item in summary.approvals],
                "errors": list(summary.errors),
                "final_state": summary.final_state,
            },
            payload_content_type="application/json",
            trace_id=trace_id,
            task=AgentPeerTask(
                task_id=f"peer-{trace_id}",
                state=_peer_state_to_task_state(summary.final_state),
                artifact_refs=[],
            ),
            error=(
                AgentPeerError(
                    code="peer_failed",
                    message="; ".join(summary.errors)[:500] or "对端流式处理失败",
                    retryable=True,
                )
                if summary.final_state == "failed"
                else None
            ),
        )
        return _json_text(ack_env.model_dump())
    except Exception as exc:
        # 与目标 Agent 通信失败时刷新目录缓存，便于下次改用最新地址重试。
        try:
            _refresh_agent_list_for_visible_groups(_stable_groups(s.discovery_groups))
        except Exception:
            pass
        return _build_error_envelope_text(
            intent="delegate",
            session_id=caller_session_id,
            target_agent_id=target_id,
            target_groups=None,
            message=str(exc),
            code="send_message_failed",
            retryable=True,
            trace_id=trace_id,
        )


@tool("agent_broadcast")
async def agent_broadcast(
    message: str,
    discovery_group_ids: list[str],
    context: OpenAIConversationContext | None = None,
) -> str:
    """使用场景：按多个 discovery_group 广播消息；并发汇总每个目标的流式输出与待审批摘要。

    字段说明：
    - `message`：广播消息正文（必填）。
    - `discovery_group_ids`：目标分组列表（必填，至少一个）。

    返回说明：
    - 成功：返回 JSON 文本，正文包含广播统计、每个目标的 `final_state/stream_output/approvals`。
    - 任一目标在等待审批：`task.state="requires_input"`；调用方应对每个 `approvals[]` 调用 **`agent_peer_approve_tools`** 处理。
    - 失败：返回 JSON 文本，正文包含失败原因（如分组为空或中继服务异常）。

    调用范例：
    - `agent_broadcast({"message":"请同步最新规范","discovery_group_ids":["team-a"]})`
    - `agent_broadcast({"message":"触发巡检","discovery_group_ids":["team-a","team-b"]})`
    """

    s = get_settings()
    caller_session_id = _session_id_from_context(context, "broadcast")
    trace_id = f"trace-{uuid.uuid4().hex}"
    msg = message.strip()
    groups = _stable_groups(discovery_group_ids)
    if not msg:
        return _build_error_envelope_text(
            intent="broadcast",
            session_id=caller_session_id,
            target_agent_id=None,
            target_groups=groups or ["invalid-group"],
            message="message 不能为空",
            code="invalid_message",
            retryable=False,
            trace_id=trace_id,
        )
    if not groups:
        return _build_error_envelope_text(
            intent="broadcast",
            session_id=caller_session_id,
            target_agent_id=None,
            target_groups=["invalid-group"],
            message="discovery_group_ids 不能为空",
            code="invalid_groups",
            retryable=False,
            trace_id=trace_id,
        )
    try:
        registry_url = _require_registry_url()
        async with httpx.AsyncClient(timeout=_DEFAULT_HTTP_TIMEOUT_SECONDS) as client:
            resp = await client.post(
                f"{registry_url}/v1/broadcast",
                json={
                    "message": msg,
                    "discovery_group_ids": groups,
                    "source": "agent-peer",
                },
            )
            resp.raise_for_status()
            result = resp.json()
        stream_timeout_seconds = float(max(1, int(s.agent_peer_broadcast_stream_timeout_seconds)))
        # 并发收集每个目标的 SSE：避免串行让快目标被慢目标饿死，整体 SLA 由 stream_timeout_seconds 兜底。
        targets: list[dict[str, Any]] = []
        for item in list(result.get("results", [])) if isinstance(result, dict) else []:
            if not isinstance(item, dict):
                continue
            agent_id = str(item.get("agent_id") or "").strip()
            base_url = str(item.get("base_url") or "").strip()
            peer_client_id = str(item.get("client_id") or "").strip()
            peer_session_id = str(item.get("session_id") or "").strip()
            if not agent_id or not base_url or not peer_client_id or not peer_session_id:
                continue
            targets.append(
                {
                    "agent_id": agent_id,
                    "base_url": base_url,
                    "client_id": peer_client_id,
                    "session_id": peer_session_id,
                }
            )

        async def _collect(t: dict[str, Any]) -> tuple[dict[str, Any], PeerStreamSummary]:
            summary = await _collect_peer_stream_summary(
                base_url=t["base_url"],
                client_id=t["client_id"],
                session_id=t["session_id"],
                timeout_seconds=stream_timeout_seconds,
            )
            return t, summary

        stream_outputs: list[dict[str, Any]] = []
        any_truncated = False
        per_target_states: list[str] = []
        if targets:
            results_per_target = await asyncio.gather(
                *(_collect(t) for t in targets),
                return_exceptions=False,
            )
            for t, summary in results_per_target:
                stream_outputs.append(
                    {
                        "agent_id": t["agent_id"],
                        "base_url": t["base_url"],
                        "client_id": t["client_id"],
                        "session_id": t["session_id"],
                        "output": summary.text,
                        "truncated": summary.truncated,
                        "final_state": summary.final_state,
                        "approvals": [item.model_dump() for item in summary.approvals],
                        "errors": list(summary.errors),
                    }
                )
                per_target_states.append(summary.final_state)
                if summary.truncated:
                    any_truncated = True
        # 聚合规则：任一目标待审批 → requires_input；全部成功 → succeeded；
        # 全部失败 → failed；混合或仍有截断 → running。
        if any(state == "requires_input" for state in per_target_states):
            aggregated: AgentPeerTaskState = "requires_input"
        elif per_target_states and all(state == "succeeded" for state in per_target_states):
            aggregated = "succeeded"
        elif per_target_states and all(state in {"failed", "truncated"} for state in per_target_states):
            aggregated = "failed" if all(state == "failed" for state in per_target_states) else "running"
        else:
            aggregated = "running"
        env = build_agent_peer_envelope(
            caller_agent_id=s.agent_id,
            caller_session_id=caller_session_id,
            caller_groups=s.discovery_groups,
            target_agent_id=None,
            target_groups=groups,
            intent="broadcast",
            payload_content={
                "ok": aggregated in {"succeeded", "requires_input", "running"},
                "broadcast_result": result,
                "stream_outputs": stream_outputs,
                "stream_timeout_seconds": stream_timeout_seconds,
                "stream_output_truncated": any_truncated,
                "aggregated_state": aggregated,
            },
            payload_content_type="application/json",
            trace_id=trace_id,
            task=AgentPeerTask(
                task_id=f"broadcast-{uuid.uuid4().hex[:10]}",
                state=aggregated,
                artifact_refs=[],
            ),
        )
        return _json_text(env.model_dump())
    except Exception as exc:
        return _build_error_envelope_text(
            intent="broadcast",
            session_id=caller_session_id,
            target_agent_id=None,
            target_groups=groups,
            message=str(exc),
            code="broadcast_failed",
            retryable=True,
            trace_id=trace_id,
        )


@tool("agent_peer_approve_tools")
async def agent_peer_approve_tools(
    target_agent_id: str,
    target_session_id: str,
    decision: str,
    approved_call_ids: list[str] | None = None,
    rejected_call_ids: list[str] | None = None,
    context: OpenAIConversationContext | None = None,
) -> str:
    """使用场景：对端 `agent_send_message`/`agent_broadcast` 返回 `approval_required` 时，提交本侧对对端 pending 工具的审批决策；不用于本机自身审批。

    字段说明：
    - `target_agent_id`：审批对端 Agent ID（必填）。
    - `target_session_id`：对端等待审批的会话 ID（必填，对应 `agent_send_message` 返回的 `payload.content.target_session_id` 或 `agent_broadcast` 中每个 `stream_outputs[i].session_id`）。
    - `decision`：审批决策，取值之一：`approve`（同意全部 pending）、`reject`（拒绝全部 pending）、`selection`（逐条决策）。
    - `approved_call_ids`：`decision=selection` 时必填非空之一；要批准的 `tool_call.id` 列表。
    - `rejected_call_ids`：`decision=selection` 时必填非空之一；要拒绝的 `tool_call.id` 列表。

    返回说明：
    - 成功：JSON 文本，`task.state` 反映对端审批后的执行终态（`succeeded` 表示工具执行完毕、`requires_input` 表示对端再次进入新一轮审批），`payload.content` 含 `stream_output/approvals/final_state`。
    - 失败：JSON 文本，`task.state="failed"`，`error.code` 标注失败类别（`invalid_decision`/`resume_failed` 等）。

    调用范例：
    - `agent_peer_approve_tools({"target_agent_id":"agent-b","target_session_id":"peer-...","decision":"approve"})`
    - `agent_peer_approve_tools({"target_agent_id":"agent-b","target_session_id":"peer-...","decision":"reject"})`
    - `agent_peer_approve_tools({"target_agent_id":"agent-b","target_session_id":"peer-...","decision":"selection","approved_call_ids":["call_1"],"rejected_call_ids":["call_2"]})`
    """

    s = get_settings()
    caller_session_id = _session_id_from_context(context, f"peer-{s.agent_id}")
    trace_id = f"trace-{uuid.uuid4().hex}"
    target_id = (target_agent_id or "").strip()
    peer_session_id = (target_session_id or "").strip()
    if not target_id:
        return _build_error_envelope_text(
            intent="delegate",
            session_id=caller_session_id,
            target_agent_id="",
            target_groups=None,
            message="target_agent_id 不能为空",
            code="invalid_target_agent",
            retryable=False,
            trace_id=trace_id,
        )
    if not peer_session_id:
        return _build_error_envelope_text(
            intent="delegate",
            session_id=caller_session_id,
            target_agent_id=target_id,
            target_groups=None,
            message="target_session_id 不能为空",
            code="invalid_target_session",
            retryable=False,
            trace_id=trace_id,
        )
    try:
        resume_value = _build_resume_value(
            decision=decision,
            approved_call_ids=approved_call_ids,
            rejected_call_ids=rejected_call_ids,
        )
    except ValueError as exc:
        return _build_error_envelope_text(
            intent="delegate",
            session_id=caller_session_id,
            target_agent_id=target_id,
            target_groups=None,
            message=str(exc),
            code="invalid_decision",
            retryable=False,
            trace_id=trace_id,
        )
    try:
        # 审批 resume 走 direct：Register Center 中继不透传 resume_value，仅消息分支可用。
        target = _resolve_target_agent(target_id)
        target_base_url = str(target.get("base_url") or "").strip().rstrip("/")
        if not target_base_url:
            raise ValueError("目标 Agent 未提供 base_url")
        peer_client_id = f"approve-{uuid.uuid4().hex}"
        body = {
            "session_id": peer_session_id,
            "client_id": peer_client_id,
            "request_type": "resume",
            "resume_value": resume_value,
            "source": "agent-peer-approve",
            "priority": "resume",
        }
        async with httpx.AsyncClient(timeout=_DEFAULT_HTTP_TIMEOUT_SECONDS) as client:
            resp = await client.post(f"{target_base_url}/v1/messages", json=body)
            resp.raise_for_status()
            submit = resp.json()
        accepted = bool(submit.get("accepted", False))
        if not accepted:
            raise ValueError("目标 Agent 未确认 resume 入队（accepted=false）")
        summary = await _collect_peer_stream_summary(
            base_url=target_base_url,
            client_id=peer_client_id,
            session_id=peer_session_id,
            timeout_seconds=float(max(1, int(s.agent_peer_stream_timeout_seconds))),
        )
        env = build_agent_peer_envelope(
            caller_agent_id=s.agent_id,
            caller_session_id=caller_session_id,
            caller_groups=s.discovery_groups,
            target_agent_id=target_id,
            target_groups=None,
            intent="delegate",
            payload_content={
                "ok": summary.final_state in {"succeeded", "requires_input"},
                "target_agent_id": target_id,
                "target_base_url": target_base_url,
                "target_session_id": peer_session_id,
                "target_client_id": peer_client_id,
                "decision": resume_value,
                "submit": submit,
                "stream_output": summary.text,
                "stream_output_truncated": summary.truncated,
                "approvals": [item.model_dump() for item in summary.approvals],
                "errors": list(summary.errors),
                "final_state": summary.final_state,
            },
            payload_content_type="application/json",
            trace_id=trace_id,
            task=AgentPeerTask(
                task_id=f"peer-approve-{trace_id}",
                state=_peer_state_to_task_state(summary.final_state),
                artifact_refs=[],
            ),
            error=(
                AgentPeerError(
                    code="peer_failed",
                    message="; ".join(summary.errors)[:500] or "对端 resume 后流式处理失败",
                    retryable=True,
                )
                if summary.final_state == "failed"
                else None
            ),
        )
        return _json_text(env.model_dump())
    except Exception as exc:
        return _build_error_envelope_text(
            intent="delegate",
            session_id=caller_session_id,
            target_agent_id=target_id,
            target_groups=None,
            message=str(exc),
            code="resume_failed",
            retryable=True,
            trace_id=trace_id,
        )
