"""Agent 间交互工具：发现（含卡片信息）、点对点发送、广播与任务查询。"""

from __future__ import annotations

import asyncio
import json
import time
import uuid
from typing import Any
from urllib.parse import urljoin, urlparse

import httpx

from app.context.models import OpenAIConversationContext
from app.config.settings import get_settings
from app.schemas.agent_peer import AgentPeerError, AgentPeerTask, build_agent_peer_envelope
from app.harness.tools.tool import tool

_DEFAULT_HTTP_TIMEOUT_SECONDS = 15.0
_AGENT_LIST_CACHE: list[dict[str, Any]] = []
_AGENT_LIST_CACHE_UPDATED_AT_UNIX_MS = 0


def _session_id_from_context(context: OpenAIConversationContext | None, fallback_prefix: str) -> str:
    """从工具上下文提取会话 ID，缺失时回退到前缀+随机值。

    逻辑：
    1. 有 context 且 `session_id` 非空时直接复用；
    2. 否则生成 `${fallback_prefix}-${uuid}`，保证工具独立可运行。
    """
    if context is not None and (context.session_id or "").strip():
        return context.session_id.strip()
    return f"{fallback_prefix}-{uuid.uuid4().hex[:8]}"


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

    与外部交互：
    - 无网络/文件交互，仅依赖本地时钟。

    异常说明：
    - 不抛异常，始终返回布尔值。

    副作用说明：
    - 无。
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

    关键分支/边界：
    - 分组为空时由 `_discover_agents_by_groups` 返回空列表并覆盖缓存；
    - 注册中心异常向上抛，由上层按场景决定是否降级使用旧缓存。

    与外部交互：
    - 通过 `_discover_agents_by_groups` 发起 HTTP 请求访问注册中心。

    异常说明：
    - 不吞异常，调用方负责捕获与重试策略。

    副作用说明：
    - 会修改进程内 `_AGENT_LIST_CACHE` 与缓存更新时间。
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
    """将字典转换为稳定 JSON 文本。

    逻辑：
    1. 使用 `json.dumps` 进行 UTF-8 友好序列化；
    2. 保持 `ensure_ascii=False` 便于直接阅读中文字段；
    3. 返回紧凑格式，减少工具返回体积。

    关键分支/边界：
    - 不可序列化对象会向上抛异常，由调用方统一转换为 `ERROR`。

    与外部交互：
    - 无。

    异常说明：
    - 不吞异常，交给上层方法处理。

    副作用说明：
    - 无。
    """

    return json.dumps(payload, ensure_ascii=False)


def _stable_groups(raw_groups: list[str] | None) -> list[str]:
    """规范化分组列表。

    逻辑：
    1. 处理空值输入并返回空列表；
    2. 去除每项首尾空白；
    3. 过滤空项并按首次出现去重。

    关键分支/边界：
    - 输入为 `None` 时返回空列表；
    - 重复分组只保留一份，避免重复调用。

    与外部交互：
    - 无。

    异常说明：
    - 无显式异常。

    副作用说明：
    - 无。
    """

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

    关键分支/边界：
    - 目标缺失时允许传空分组，但不能同时缺失（由调用方保证）。

    与外部交互：
    - 读取本地 `Settings` 获取 caller 信息。

    异常说明：
    - 不吞异常，调用方负责兜底。

    副作用说明：
    - 无。
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
    """获取必需的 Register Center 地址。

    逻辑：
    1. 从配置读取 `registry_url`；
    2. 判空；
    3. 返回去尾斜杠后的地址。

    关键分支/边界：
    - 未配置时抛 `ValueError`，由工具函数转为结构化失败结果。

    与外部交互：
    - 读取本地配置。

    异常说明：
    - 未配置时抛 `ValueError`。

    副作用说明：
    - 无。
    """

    registry_url = _resolve_registry_url()
    if not registry_url:
        raise ValueError("未配置 REGISTRY_URL，无法进行 Agent 发现与转发")
    return registry_url


def _discover_agents_by_groups(groups: list[str]) -> list[dict[str, Any]]:
    """按分组查询 register-center 并聚合去重。

    逻辑：
    1. 校验并读取 `REGISTRY_URL`；
    2. 对每个分组调用 `/v1/agents?discovery_group=...`；
    3. 按 `agent_id` 去重并返回聚合结果。

    关键分支/边界：
    - 分组为空直接返回空列表；
    - 单个分组请求失败会抛异常并终止本次调用，避免返回不完整目录快照。

    与外部交互：
    - 对 Register Center 发起 HTTP GET 请求。

    异常说明：
    - HTTP/JSON 异常向上抛，由各工具函数统一转失败信封。

    副作用说明：
    - 无。
    """

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


def _extract_sse_text_from_event(event_name: str, payload: dict[str, Any]) -> str:
    """从单条 SSE 事件中提取对 Agent 可读的正文片段。

    逻辑：
    1. 读取事件 `data` 字段（兼容 dict/空值）；
    2. 按事件类型提取核心文本（assistant/reasoning/tool_result/error）；
    3. 其他事件返回空串，避免把协议元数据混入正文。

    关键分支/边界：
    - 未知事件或缺失字段返回空串；
    - `error` 事件前置 `[ERROR]` 标签，便于上层快速定位异常。

    与外部交互：
    - 无。

    异常说明：
    - 不抛异常，异常输入统一降级为空串。

    副作用说明：
    - 无。
    """
    data = payload.get("data", {})
    if not isinstance(data, dict):
        data = {}
    if event_name in {"assistant", "reasoning", "tool_result"}:
        return str(data.get("content", "") or "").strip()
    if event_name == "error":
        msg = str(data.get("message", "") or "").strip()
        return f"[ERROR] {msg}" if msg else ""
    return ""


async def _collect_peer_stream_output(
    *,
    base_url: str,
    client_id: str,
    session_id: str,
    timeout_seconds: float,
) -> tuple[str, bool]:
    """拉取远端 SSE 并汇总当前已产出的正文。

    逻辑：
    1. 连接 `{base_url}/v1/streams?client_id=...` 订阅事件；
    2. 解析 `event/data` 行并抽取可读正文；
    3. 仅处理目标 `session_id` 的事件；
    4. 遇到 `done` 正常结束；超时则截断并返回已采集内容。

    关键分支/边界：
    - `client_id`/`session_id` 任一为空直接返回空输出；
    - 连接失败时返回错误提示文本；
    - 超时返回 `truncated=True`，正文保留截至当前的已收集片段。

    与外部交互：
    - 通过 HTTP SSE 读取目标 Agent 的流式事件。

    异常说明：
    - 吞掉网络/解析异常并转为可读错误文本，避免中断主调用链。

    副作用说明：
    - 无。
    """
    final_base_url = base_url.strip().rstrip("/")
    final_client_id = client_id.strip()
    final_session_id = session_id.strip()
    if not final_base_url or not final_client_id or not final_session_id:
        return "", False
    collected_lines: list[str] = []
    event_name = ""
    data_lines: list[str] = []
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
                        text_piece = _extract_sse_text_from_event(stream_event_name, payload)
                        if text_piece:
                            collected_lines.append(text_piece)
                        if stream_event_name == "done":
                            break
                        event_name = ""
                        data_lines = []
        return "\n".join(collected_lines), False
    except TimeoutError:
        # 广播/跨 Agent 场景允许截断返回，保证工具在 SLA 内可收口。
        return "\n".join(collected_lines), True
    except Exception as exc:
        err = str(exc).strip() or "读取远端流失败"
        return f"[ERROR] {err}", False


def _attach_agent_card_summary(agent: dict[str, Any]) -> dict[str, Any]:
    """为单个发现结果补充 Agent Card 摘要字段。

    逻辑：
    1. 从 `base_url` 生成 `/.well-known/agent-card.json` 地址；
    2. 发起 HTTP 请求拉取 card JSON；
    3. 将固定结构 `agent_card` 写回返回项（含访问 URL、端口、card URL、card 内容、错误字段）。

    关键分支/边界：
    - `base_url` 为空时不请求网络，直接写入 `agent_card.error`；
    - 远端 card 不可用时不影响整体 discover，保留 agent 基础信息。

    与外部交互：
    - 对目标 Agent 发起 HTTP GET 请求读取 card。

    异常说明：
    - 吞掉 card 拉取异常并落入 `agent_card.error`，避免单个节点影响整批发现。

    副作用说明：
    - 无（返回新字典，不修改传入参数）。
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
        # 地址缺失时不发起网络请求，避免无效重试与噪音日志。
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
        # 发现流程要尽量返回可用目录，因此把失败信息内联到 agent 项。
        card_info["card_payload"] = None
        card_info["error"] = str(exc)
        enriched["agent_card"] = card_info
        return enriched


def _resolve_target_agent(target_agent_id: str) -> dict[str, Any]:
    """在调用方可见分组范围内解析目标 Agent。

    逻辑：
    1. 读取当前实例的 `discovery_groups`；
    2. 通过 `_discover_agents_by_groups` 获取可见候选；
    3. 按 `target_agent_id` 精确匹配并返回记录。

    关键分支/边界：
    - 未配置本地 `DISCOVERY_GROUPS` 时无法解析目标，直接报错；
    - 未命中目标时报错，防止跨组盲投。

    与外部交互：
    - 间接调用 Register Center 查询接口。

    异常说明：
    - 解析失败抛 `ValueError`，由工具调用方转换成失败信封。

    副作用说明：
    - 无。
    """

    s = get_settings()
    visible_groups = _stable_groups(s.discovery_groups)
    if not visible_groups:
        raise ValueError("未配置 DISCOVERY_GROUPS，无法解析目标 Agent")
    # TTL 过期时优先回源刷新，避免长期进程里使用过期目录。
    if _is_agent_list_cache_stale():
        try:
            _refresh_agent_list_for_visible_groups(visible_groups)
        except Exception:
            # 刷新失败时保留旧缓存兜底，避免短时目录抖动导致全量不可用。
            pass
    # 先读进程内缓存：`agent_discover` 成功后可复用，避免每次点对点都回源注册中心。
    cached = _resolve_target_agent_from_cache(target_agent_id, visible_groups)
    if cached is not None:
        return cached
    # 缓存未命中再回源发现，并刷新缓存以服务后续调用。
    agents = _refresh_agent_list_for_visible_groups(visible_groups)
    for item in agents:
        if str(item.get("agent_id") or "").strip() == target_agent_id.strip():
            return item
    raise ValueError(f"在当前可见分组内未找到目标 Agent: {target_agent_id!r}")


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
        # 发现成功后刷新缓存，后续 `agent_send_message` 可直接命中。
        _cache_agent_list(discovered_agents)
        agents = list(discovered_agents)
        # 在 discover 阶段补齐 card 摘要，避免额外工具往返。
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
    """使用场景：向指定 Agent 发起点对点委托请求（异步后台执行）；不用于按分组群发。

    字段说明：
    - `target_agent_id`：目标 Agent ID（必填）。
    - `message`：发送给目标 Agent 的消息正文（必填）。

    返回说明：
    - 成功：返回 JSON 文本，正文包含 `ok=true`、目标地址、提交回执与远端 SSE 已输出内容。
    - 失败：返回 JSON 文本，正文包含失败原因（如目标不可达、配置错误）。

    调用范例：
    - `agent_send_message({"target_agent_id":"agent-b","message":"请总结日报"})`
    """

    s = get_settings()
    msg = message.strip()
    target_id = target_agent_id.strip()
    # 投递链路由配置控制：direct 直连，relay 走 register-center 中继。
    final_delivery_mode = (s.agent_peer_delivery_mode or "direct").strip().lower()
    # 会话 ID 统一由 context 推导，避免调用方手工透传导致串会话。
    session_id = _session_id_from_context(context, f"peer-{s.agent_id}")
    trace_id = f"trace-{uuid.uuid4().hex}"
    if not target_id:
        return _build_error_envelope_text(
            intent="delegate",
            session_id=session_id,
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
            session_id=session_id,
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
            session_id=session_id,
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
            caller_session_id=session_id,
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
            "session_id": session_id,
            "client_id": peer_client_id,
            "request_type": "message",
            "content": _json_text(req_env.model_dump()),
            "source": "agent-peer",
            "priority": "human",
        }
        target_base_url = ""
        with httpx.AsyncClient(timeout=_DEFAULT_HTTP_TIMEOUT_SECONDS) as client:
            if final_delivery_mode == "direct":
                # direct 模式：本地先解析目标地址，再直接调用目标 Agent。
                target = _resolve_target_agent(target_id)
                target_base_url = str(target.get("base_url") or "").strip().rstrip("/")
                resp = await client.post(f"{target_base_url}/v1/messages", json=body)
            else:
                # relay 模式：交给 register-center 转发，避免调用方直接感知目标拓扑。
                registry_url = _require_registry_url()
                relay_payload = {
                    "target_agent_id": target_id,
                    "caller_groups": _stable_groups(s.discovery_groups),
                    "session_id": session_id,
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
        stream_text, stream_truncated = await _collect_peer_stream_output(
            base_url=target_base_url,
            client_id=peer_client_id,
            session_id=session_id,
            timeout_seconds=float(max(1, int(s.agent_peer_stream_timeout_seconds))),
        )
        ack_env = build_agent_peer_envelope(
            caller_agent_id=s.agent_id,
            caller_session_id=session_id,
            caller_groups=s.discovery_groups,
            target_agent_id=target_id,
            target_groups=None,
            intent="delegate",
            payload_content={
                "ok": True,
                "target_base_url": target_base_url,
                "submit": submit,
                "stream_output": stream_text,
                "stream_output_truncated": stream_truncated,
            },
            payload_content_type="application/json",
            trace_id=trace_id,
            task=AgentPeerTask(
                task_id=f"peer-{trace_id}",
                state="queued",
                artifact_refs=[],
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
            session_id=session_id,
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
    """使用场景：按多个 discovery_group 广播消息（异步后台执行）；不保证所有目标都成功处理。

    字段说明：
    - `message`：广播消息正文（必填）。
    - `discovery_group_ids`：目标分组列表（必填，至少一个）。

    返回说明：
    - 成功：返回 JSON 文本，正文包含广播统计与各目标当前已输出内容；超时会标记截断。
    - 失败：返回 JSON 文本，正文包含失败原因（如分组为空或中继服务异常）。

    调用范例：
    - `agent_broadcast({"message":"请同步最新规范","discovery_group_ids":["team-a"]})`
    - `agent_broadcast({"message":"触发巡检","discovery_group_ids":["team-a","team-b"]})`
    """

    s = get_settings()
    session_id = _session_id_from_context(context, "broadcast")
    trace_id = f"trace-{uuid.uuid4().hex}"
    msg = message.strip()
    groups = _stable_groups(discovery_group_ids)
    if not msg:
        return _build_error_envelope_text(
            intent="broadcast",
            session_id=session_id,
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
            session_id=session_id,
            target_agent_id=None,
            target_groups=["invalid-group"],
            message="discovery_group_ids 不能为空",
            code="invalid_groups",
            retryable=False,
            trace_id=trace_id,
        )
    try:
        registry_url = _require_registry_url()
        with httpx.AsyncClient(timeout=_DEFAULT_HTTP_TIMEOUT_SECONDS) as client:
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
        stream_deadline = time.monotonic() + stream_timeout_seconds
        stream_outputs: list[dict[str, Any]] = []
        stream_truncated = False
        for item in list(result.get("results", [])) if isinstance(result, dict) else []:
            if not isinstance(item, dict):
                continue
            agent_id = str(item.get("agent_id") or "").strip()
            base_url = str(item.get("base_url") or "").strip()
            peer_client_id = str(item.get("client_id") or "").strip()
            peer_session_id = str(item.get("session_id") or "").strip()
            if not agent_id or not base_url or not peer_client_id or not peer_session_id:
                continue
            remaining = stream_deadline - time.monotonic()
            if remaining <= 0:
                stream_truncated = True
                break
            output_text, truncated = await _collect_peer_stream_output(
                base_url=base_url,
                client_id=peer_client_id,
                session_id=peer_session_id,
                timeout_seconds=remaining,
            )
            stream_outputs.append(
                {
                    "agent_id": agent_id,
                    "client_id": peer_client_id,
                    "session_id": peer_session_id,
                    "output": output_text,
                    "truncated": truncated,
                }
            )
            if truncated:
                stream_truncated = True
                break
        env = build_agent_peer_envelope(
            caller_agent_id=s.agent_id,
            caller_session_id=session_id,
            caller_groups=s.discovery_groups,
            target_agent_id=None,
            target_groups=groups,
            intent="broadcast",
            payload_content={
                "ok": True,
                "broadcast_result": result,
                "stream_outputs": stream_outputs,
                "stream_timeout_seconds": stream_timeout_seconds,
                "stream_output_truncated": stream_truncated,
            },
            payload_content_type="application/json",
            trace_id=trace_id,
            task=AgentPeerTask(
                task_id=f"broadcast-{uuid.uuid4().hex[:10]}",
                state="running" if int(result.get("total_targets") or 0) > 0 else "succeeded",
                artifact_refs=[],
            ),
        )
        return _json_text(env.model_dump())
    except Exception as exc:
        return _build_error_envelope_text(
            intent="broadcast",
            session_id=session_id,
            target_agent_id=None,
            target_groups=groups,
            message=str(exc),
            code="broadcast_failed",
            retryable=True,
            trace_id=trace_id,
        )


