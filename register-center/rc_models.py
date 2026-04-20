"""Register Center 的数据模型定义。"""

from __future__ import annotations

from typing import Any
from urllib.parse import urlparse

from pydantic import BaseModel, Field, field_validator


class AgentUpsertRequest(BaseModel):
    """登记或更新 Agent 的请求体。

    逻辑：
    1. 校验 `agent_id` 非空且长度不超过约束上限；
    2. 校验 `base_url` 为 http/https 的绝对地址，并做尾部斜杠规范化；
    3. 校验 `discovery_group`（字符串或字符串列表）并统一规范为分组列表；
    4. 规范化可选 `capabilities_hint`，用于目录初筛。

    关键分支/边界：
    - `agent_id` 为空白字符时直接报错；
    - `agent_id` 超过长度限制时直接报错；
    - `base_url` 非 http/https 或缺失 netloc 时直接报错；
    - `discovery_group` 为空、包含空白项时直接报错；
    - 重复分组会自动去重，保留首次出现顺序；
    - `capabilities_hint` 为空或重复项会被清理。

    与外部交互：
    - 无外部 IO，仅执行入参校验与规范化。

    异常说明：
    - 校验失败时抛出 `ValueError`，由 FastAPI/Pydantic 转为 422。

    副作用说明：
    - 无对象外部副作用。
    """

    agent_id: str = Field(description="逻辑 Agent ID，MVP 内需全局唯一。", max_length=256)
    base_url: str = Field(description="Agent 对外 HTTP 根地址，必须是绝对 URL。")
    discovery_group: list[str] = Field(
        min_length=1,
        description="发现分组 ID 列表（必填）；允许单字符串输入并自动规范化为列表。",
    )
    capabilities_hint: list[str] = Field(
        default_factory=list,
        description="能力标签提示（可选），用于 discovery 阶段的轻量过滤。",
    )

    @field_validator("agent_id")
    @classmethod
    def validate_agent_id(cls, value: str) -> str:
        """校验并规范化 agent_id。

        逻辑：
        1. 去除首尾空白；
        2. 校验非空；
        3. 校验长度不超过 256。

        关键分支/边界：
        - 输入全为空白时拒绝；
        - 输入长度超限时拒绝。

        与外部交互：
        - 无。

        异常说明：
        - 违反约束时抛出 `ValueError`。

        副作用说明：
        - 无。
        """

        cleaned = value.strip()
        if not cleaned:
            raise ValueError("agent_id 不能为空")
        if len(cleaned) > 256:
            raise ValueError("agent_id 长度不能超过 256")
        return cleaned

    @field_validator("base_url")
    @classmethod
    def validate_base_url(cls, value: str) -> str:
        """校验并规范化 base_url。

        逻辑：
        1. 去除首尾空白；
        2. 解析 URL 并校验 scheme/netloc；
        3. 统一移除尾部 `/`，避免同地址重复存储。

        关键分支/边界：
        - 仅允许 http/https；
        - 缺失主机部分（netloc）时报错；
        - 根路径 URL（如 `https://a.com/`）会收敛为 `https://a.com`。

        与外部交互：
        - 无。

        异常说明：
        - URL 非法时抛出 `ValueError`。

        副作用说明：
        - 无。
        """

        cleaned = value.strip()
        parsed = urlparse(cleaned)
        if parsed.scheme not in {"http", "https"}:
            raise ValueError("base_url 必须使用 http 或 https 协议")
        if not parsed.netloc:
            raise ValueError("base_url 必须是绝对 URL")
        return cleaned.rstrip("/")

    @field_validator("discovery_group", mode="before")
    @classmethod
    def validate_discovery_group(cls, value: Any) -> list[str]:
        """校验并规范化 discovery_group 列表。

        逻辑：
        1. 支持输入字符串或字符串列表，并统一转为列表；
        2. 逐项去除首尾空白并校验非空；
        3. 去重并保留首次出现顺序，返回规范化列表。

        关键分支/边界：
        - 空字符串、空列表或包含空白项直接拒绝；
        - 非字符串类型输入直接拒绝。

        与外部交互：
        - 无。

        异常说明：
        - 非法输入抛出 `ValueError`。

        副作用说明：
        - 无。
        """

        if isinstance(value, str):
            raw_items = [value]
        elif isinstance(value, list):
            raw_items = value
        else:
            raise ValueError("discovery_group 必须是字符串或字符串列表")

        seen: set[str] = set()
        result: list[str] = []
        for item in raw_items:
            if not isinstance(item, str):
                raise ValueError("discovery_group 列表项必须是字符串")
            cleaned = item.strip()
            if not cleaned:
                raise ValueError("discovery_group 中存在空值")
            if cleaned in seen:
                continue
            seen.add(cleaned)
            result.append(cleaned)
        if not result:
            raise ValueError("discovery_group 不能为空")
        return result

    @field_validator("capabilities_hint", mode="before")
    @classmethod
    def validate_capabilities_hint(cls, value: Any) -> list[str]:
        """校验并规范化能力标签列表。

        逻辑：
        1. 支持输入字符串或字符串列表；
        2. 逐项去空白并去重；
        3. 返回规范化标签数组。

        关键分支/边界：
        - `None` 视为无标签；
        - 非字符串或列表输入直接报错。

        与外部交互：
        - 无。

        异常说明：
        - 非法输入抛出 `ValueError`。

        副作用说明：
        - 无。
        """

        if value is None:
            return []
        if isinstance(value, str):
            raw_items = [value]
        elif isinstance(value, list):
            raw_items = value
        else:
            raise ValueError("capabilities_hint 必须是字符串或字符串列表")
        seen: set[str] = set()
        result: list[str] = []
        for item in raw_items:
            if not isinstance(item, str):
                raise ValueError("capabilities_hint 列表项必须是字符串")
            cleaned = item.strip()
            if not cleaned or cleaned in seen:
                continue
            seen.add(cleaned)
            result.append(cleaned)
        return result


class AgentRecord(BaseModel):
    """Register Center 对外返回的 Agent 记录。"""

    agent_id: str
    base_url: str
    discovery_group: list[str]
    capabilities_hint: list[str] = Field(default_factory=list)
    registered_at_unix: int


class AgentListResponse(BaseModel):
    """Agent 列表查询返回结构。"""

    agents: list[AgentRecord]


class HealthResponse(BaseModel):
    """健康检查返回结构。"""

    status: str
    agents: int


class BroadcastRequest(BaseModel):
    """广播请求体。

    逻辑：
    1. 接收待广播的文本消息；
    2. 接收目标发现分组 ID 列表；
    3. 可选接收 `source` 标识，转发给下游 Agent API。

    关键分支/边界：
    - `message` 为空白时拒绝；
    - `discovery_group_ids` 至少包含 1 项；
    - 列表项会做去空白处理，空白项会被拒绝。

    与外部交互：
    - 无外部 IO，仅做入参校验与规范化。

    异常说明：
    - 校验失败时抛出 `ValueError`，由 FastAPI/Pydantic 转换为 422。

    副作用说明：
    - 无。
    """

    message: str = Field(description="广播文本消息。")
    discovery_group_ids: list[str] = Field(
        min_length=1,
        description="目标发现分组 ID 列表；命中任一分组即会接收广播。",
    )
    source: str = Field(default="register-center-broadcast", description="转发消息来源标识。")

    @field_validator("message")
    @classmethod
    def validate_message(cls, value: str) -> str:
        """校验并规范化广播消息。

        逻辑：
        1. 去除首尾空白；
        2. 校验非空；
        3. 返回规范化结果。

        关键分支/边界：
        - 空字符串或全空白输入直接拒绝。

        与外部交互：
        - 无。

        异常说明：
        - 非法输入抛出 `ValueError`。

        副作用说明：
        - 无。
        """

        cleaned = value.strip()
        if not cleaned:
            raise ValueError("message 不能为空")
        return cleaned

    @field_validator("discovery_group_ids")
    @classmethod
    def validate_group_ids(cls, value: list[str]) -> list[str]:
        """校验并规范化分组 ID 列表。

        逻辑：
        1. 逐项去除首尾空白；
        2. 拒绝空白分组 ID；
        3. 去重并保留首次出现顺序。

        关键分支/边界：
        - 列表中出现空字符串直接拒绝；
        - 重复分组仅保留一次，避免重复广播。

        与外部交互：
        - 无。

        异常说明：
        - 非法输入抛出 `ValueError`。

        副作用说明：
        - 无。
        """

        seen: set[str] = set()
        result: list[str] = []
        for item in value:
            cleaned = item.strip()
            if not cleaned:
                raise ValueError("discovery_group_ids 中存在空值")
            if cleaned in seen:
                continue
            seen.add(cleaned)
            result.append(cleaned)
        return result


class BroadcastResultItem(BaseModel):
    """单个目标 Agent 的广播结果。"""

    agent_id: str
    base_url: str
    discovery_group: list[str]
    ok: bool
    status_code: int | None = None
    request_id: str | None = None
    detail: str | None = None


class BroadcastResponse(BaseModel):
    """广播接口返回结构。"""

    message: str
    discovery_group_ids: list[str]
    total_targets: int
    success_count: int
    failed_count: int
    results: list[BroadcastResultItem]


class RelayRequest(BaseModel):
    """单目标中继请求体。

    逻辑：
    1. 指定目标 `target_agent_id`；
    2. 由调用方带上可见分组 `caller_groups` 供中继侧做访问约束；
    3. 透传消息字段（`session_id/request_type/content/source/priority`）给目标 Agent。

    关键分支/边界：
    - `target_agent_id` 为空白时拒绝；
    - `caller_groups` 可为空列表（表示不做分组限制）；
    - `request_type`/`content` 为空白时拒绝。
    """

    target_agent_id: str = Field(description="目标 Agent ID。")
    caller_groups: list[str] = Field(default_factory=list, description="调用方可见分组列表。")
    session_id: str = Field(description="透传到目标 Agent 的会话 ID。")
    request_type: str = Field(description="透传到目标 Agent 的请求类型。")
    content: str = Field(description="透传到目标 Agent 的消息内容。")
    source: str = Field(default="agent-peer-relay", description="透传消息来源标识。")
    priority: str = Field(default="human", description="透传消息优先级。")

    @field_validator("target_agent_id")
    @classmethod
    def validate_target_agent_id(cls, value: str) -> str:
        cleaned = value.strip()
        if not cleaned:
            raise ValueError("target_agent_id 不能为空")
        return cleaned

    @field_validator("session_id", "request_type", "content", "source", "priority")
    @classmethod
    def validate_non_empty_text_fields(cls, value: str) -> str:
        cleaned = value.strip()
        if not cleaned:
            raise ValueError("中继字段不能为空")
        return cleaned

    @field_validator("caller_groups")
    @classmethod
    def validate_caller_groups(cls, value: list[str]) -> list[str]:
        seen: set[str] = set()
        result: list[str] = []
        for item in value:
            cleaned = item.strip()
            if not cleaned or cleaned in seen:
                continue
            seen.add(cleaned)
            result.append(cleaned)
        return result


class RelayResponse(BaseModel):
    """单目标中继响应。"""

    accepted: bool
    target_agent_id: str
    target_base_url: str
    request_id: str | None = None
