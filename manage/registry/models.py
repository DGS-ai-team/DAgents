"""Registry 请求/响应模型。"""

from __future__ import annotations

from typing import Any, Literal
from urllib.parse import urlparse

from pydantic import BaseModel, Field, field_validator, model_validator

from manage.platform.audit import AuditEvent
from manage.registry.status import AgentStatus

AuthMethod = Literal["shared_token", "mtls", "none"]
RiskLevel = Literal["low", "medium", "high"]
RegistryKind = Literal["node", "agent"]


def _normalize_string_list(value: Any, *, field_name: str, allow_empty: bool = True) -> list[str]:
    if value is None:
        return []
    if isinstance(value, str):
        raw_items = [value]
    elif isinstance(value, list):
        raw_items = value
    else:
        raise ValueError(f"{field_name} 必须是字符串或字符串列表")
    seen: set[str] = set()
    result: list[str] = []
    for item in raw_items:
        if not isinstance(item, str):
            raise ValueError(f"{field_name} 列表项必须是字符串")
        cleaned = item.strip()
        if not cleaned or cleaned in seen:
            continue
        seen.add(cleaned)
        result.append(cleaned)
    if not allow_empty and not result:
        raise ValueError(f"{field_name} 不能为空")
    return result


class AgentRegisterRequest(BaseModel):
    """Node 注册或心跳 upsert（discovery_group 由 Manage 分配，Node 不传）。

    `node_id` 标识承载连接的 Node，`agent_id` 标识 Node 上可被工作组
    选择的 Agent。Node-only 注册会创建一个同名 Node Agent。
    """

    agent_id: str = Field(default="", max_length=256)
    node_id: str = Field(default="", max_length=256)
    base_url: str
    host_ips: str = Field(default="", max_length=1024)
    capabilities_hint: list[str] = Field(default_factory=list)
    ttl_seconds: int = Field(default=60, ge=5, le=3600)
    name: str = Field(default="", max_length=128)
    description: str = Field(default="", max_length=2048)
    owner: str = Field(default="", max_length=256)
    team: str = Field(default="", max_length=128)
    capabilities: list[str] = Field(default_factory=list)
    tools: list[str] = Field(default_factory=list)
    skills: list[str] = Field(default_factory=list)
    auth_method: AuthMethod = Field(default="shared_token")
    risk_level: RiskLevel = Field(default="medium")
    allowed_scopes: list[str] = Field(default_factory=list)
    version: str = Field(default="", max_length=64)
    card: dict[str, Any] = Field(default_factory=dict)
    metadata: dict[str, Any] = Field(default_factory=dict)
    last_error_summary: str | None = Field(default=None, max_length=1024)
    recent_task_summary: str | None = Field(default=None, max_length=1024)

    @field_validator("agent_id", "node_id", mode="before")
    @classmethod
    def validate_ids(cls, value: Any) -> str:
        if value is None:
            return ""
        if not isinstance(value, str):
            raise ValueError("id 字段必须是字符串")
        return value.strip()

    @field_validator("base_url")
    @classmethod
    def validate_base_url(cls, value: str) -> str:
        cleaned = value.strip()
        parsed = urlparse(cleaned)
        if parsed.scheme not in {"http", "https"} or not parsed.netloc:
            raise ValueError("base_url 必须是 http/https 绝对 URL")
        return cleaned.rstrip("/")

    @field_validator("capabilities_hint", mode="before")
    @classmethod
    def validate_capabilities_hint(cls, value: Any) -> list[str]:
        return _normalize_string_list(value, field_name="capabilities_hint")

    @field_validator("capabilities", "tools", "skills", "allowed_scopes", mode="before")
    @classmethod
    def validate_string_lists(cls, value: Any) -> list[str]:
        return _normalize_string_list(value, field_name="capabilities")

    @field_validator("name", "owner", "team", "version", "host_ips", mode="before")
    @classmethod
    def validate_trimmed_text(cls, value: Any) -> str:
        if value is None:
            return ""
        if not isinstance(value, str):
            raise ValueError("字段必须是字符串")
        return value.strip()

    @field_validator("description", mode="before")
    @classmethod
    def validate_description(cls, value: Any) -> str:
        if value is None:
            return ""
        if not isinstance(value, str):
            raise ValueError("description 必须是字符串")
        return value.strip()

    @field_validator("last_error_summary", "recent_task_summary", mode="before")
    @classmethod
    def validate_optional_summary(cls, value: Any) -> str | None:
        if value is None:
            return None
        if not isinstance(value, str):
            raise ValueError("摘要字段必须是字符串")
        cleaned = value.strip()
        return cleaned or None

    @model_validator(mode="after")
    def apply_defaults(self) -> "AgentRegisterRequest":
        node = (self.node_id or "").strip()
        agent = (self.agent_id or "").strip()
        resolved = node or agent
        if not resolved:
            raise ValueError("node_id 或 agent_id 不能为空")
        self.node_id = node or resolved
        self.agent_id = agent or resolved
        if not self.name:
            self.name = self.agent_id
        meta = dict(self.metadata or {})
        meta.setdefault("node_id", self.node_id)
        self.metadata = meta
        return self


class AgentHeartbeatRequest(BaseModel):
    ttl_seconds: int = Field(default=60, ge=5, le=3600)
    version: str = ""
    tools: list[str] = Field(default_factory=list)
    skills: list[str] = Field(default_factory=list)
    last_error_summary: str | None = None
    recent_task_summary: str | None = None


class AgentDeregisterRequest(BaseModel):
    reason: str = Field(default="shutdown", max_length=256)


class AgentGroupsUpdateRequest(BaseModel):
    """Manage 端为 Node 分配 discovery_group。允许空列表表示清空分组。"""

    discovery_group: list[str] = Field(default_factory=list)

    @field_validator("discovery_group", mode="before")
    @classmethod
    def validate_discovery_group(cls, value: Any) -> list[str]:
        if value is None:
            return []
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
        return result


class AgentStoredRecord(BaseModel):
    agent_id: str
    node_id: str = ""
    # A Node is the transport/runtime host; an Agent is a selectable runtime
    # registered by that Node.  Keep this explicit in the public contract so
    # consumers do not have to infer type from identifiers.
    kind: RegistryKind = "agent"
    base_url: str
    host_ips: str = ""
    discovery_group: list[str]
    capabilities_hint: list[str] = Field(default_factory=list)
    name: str
    description: str = ""
    owner: str = ""
    team: str = ""
    capabilities: list[str] = Field(default_factory=list)
    tools: list[str] = Field(default_factory=list)
    skills: list[str] = Field(default_factory=list)
    auth_method: AuthMethod = "shared_token"
    risk_level: RiskLevel = "medium"
    allowed_scopes: list[str] = Field(default_factory=list)
    version: str = ""
    card: dict[str, Any] = Field(default_factory=dict)
    metadata: dict[str, Any] = Field(default_factory=dict)
    last_error_summary: str | None = None
    recent_task_summary: str | None = None
    registered_at_unix: int
    updated_at_unix: int
    last_seen_unix: int
    expires_at_unix: int


class AgentRecord(AgentStoredRecord):
    status: AgentStatus


class AgentDiscoverRecord(BaseModel):
    agent_id: str
    node_id: str = ""
    kind: RegistryKind = "agent"
    discovery_group: list[str]
    capabilities: list[str] = Field(default_factory=list)
    capabilities_hint: list[str] = Field(default_factory=list)
    name: str
    description: str = ""
    team: str = ""
    risk_level: RiskLevel = "medium"
    version: str = ""
    card: dict[str, Any] = Field(default_factory=dict)


class AgentListResponse(BaseModel):
    agents: list[AgentRecord]
    page: int = 1
    page_size: int = 50
    total: int = 0


class AgentDiscoverResponse(BaseModel):
    agents: list[AgentDiscoverRecord]


class AgentRegisterResponse(BaseModel):
    agent: AgentRecord
    heartbeat_interval_seconds: int = 30
    server_time_unix: int


class AuditListResponse(BaseModel):
    events: list[AuditEvent]


class HealthResponse(BaseModel):
    status: str
    agents: int
    blob: dict[str, object] = Field(default_factory=dict)
