"""集中配置：默认值在此定义，由环境变量（含 `.env`）覆盖。"""

from __future__ import annotations

import os
import uuid
from pathlib import Path
from typing import Self

from pydantic import BaseModel, ConfigDict

def _env_str(key: str, default: str = "") -> str:
    return os.environ.get(key, default).strip()


def _env_bool(key: str, default: bool = False) -> bool:
    v = _env_str(key)
    if not v:
        return default
    return v.lower() in ("1", "true", "yes", "on")


def _env_int(key: str, default: int) -> int:
    raw = _env_str(key)
    if not raw:
        return default
    return int(raw)


def _env_csv(key: str) -> list[str]:
    """解析逗号分隔环境变量为字符串列表。

    逻辑：
    1. 读取原始环境变量并按逗号切分；
    2. 去除每项首尾空白；
    3. 过滤空项并按首次出现去重。

    关键分支/边界：
    - 未配置或仅空白时返回空列表；
    - 重复值只保留第一项，保持输入顺序稳定。

    与外部交互：
    - 仅读取进程环境变量。

    异常说明：
    - 无显式异常，异常输入按空列表处理。

    副作用说明：
    - 无。
    """

    raw = _env_str(key)
    if not raw:
        return []
    seen: set[str] = set()
    result: list[str] = []
    for item in raw.split(","):
        cleaned = item.strip()
        if not cleaned or cleaned in seen:
            continue
        seen.add(cleaned)
        result.append(cleaned)
    return result


def _agent_session_store_path() -> str:
    """解析 `AGENT_SESSION_STORE_PATH`：未出现在环境中时用默认路径；显式空串表示关闭 sqlite。"""
    if "AGENT_SESSION_STORE_PATH" not in os.environ:
        return ".runtime/memory/session.sqlite3"
    return os.environ.get("AGENT_SESSION_STORE_PATH", "").strip()


def _agent_id_file_path() -> str:
    """解析 `AGENT_ID_FILE_PATH`。

    逻辑：
    1. 若环境变量中未出现该键，使用默认路径；
    2. 若出现该键，读取其值并去除首尾空白；
    3. 对空值回退默认路径，避免出现无效路径。

    关键分支/边界：
    - 显式空串会回退到默认值，而不是关闭功能；
    - 路径可以是相对路径或绝对路径，后续由文件写入逻辑统一处理。

    与外部交互：
    - 仅读取进程环境变量。

    异常说明：
    - 无显式异常抛出。

    副作用说明：
    - 无。
    """

    default_path = ".runtime/agent/agent_id"
    if "AGENT_ID_FILE_PATH" not in os.environ:
        return default_path
    candidate = os.environ.get("AGENT_ID_FILE_PATH", "").strip()
    return candidate or default_path


def _resolve_agent_id(agent_id_file_path: str) -> str:
    """解析并确保 agent_id 持久化文件存在。

    逻辑：
    1. 优先读取环境变量 `AGENT_ID`，若有值则作为当前实例 ID；
    2. 当未配置 `AGENT_ID` 时，尝试从 `agent_id_file_path` 读取已有 ID；
    3. 若环境变量和文件都不可用，则生成新 UUID 并写入文件；
    4. 将最终 ID 回写到 `os.environ["AGENT_ID"]`，保证进程内后续读取一致。

    关键分支/边界：
    - 文件不存在、为空或仅空白时，会自动生成并写入新 ID；
    - 环境变量存在时会覆盖文件内容，确保“配置优先”；
    - 写文件前会自动创建父目录（如 `.runtime/agent/`）。

    与外部交互：
    - 读取/写入本地文件系统中的 agent_id 文件；
    - 读写当前进程环境变量。

    异常说明：
    - 文件系统权限或 IO 异常不吞掉，向上抛出，避免静默启动异常状态。

    副作用说明：
    - 可能创建目录、创建/覆盖 ID 文件；
    - 会修改当前进程环境变量 `AGENT_ID`。
    """

    configured_id = _env_str("AGENT_ID")
    file_path = Path(agent_id_file_path)
    if configured_id:
        # 明确配置 AGENT_ID 时以配置为准，并同步写回文件防止重启漂移。
        file_path.parent.mkdir(parents=True, exist_ok=True)
        file_path.write_text(configured_id, encoding="utf-8")
        os.environ["AGENT_ID"] = configured_id
        return configured_id

    if file_path.is_file():
        file_id = file_path.read_text(encoding="utf-8").strip()
        if file_id:
            os.environ["AGENT_ID"] = file_id
            return file_id

    # 环境与文件都没有可用值时，生成稳定 UUID 并持久化。
    generated_id = str(uuid.uuid4())
    file_path.parent.mkdir(parents=True, exist_ok=True)
    file_path.write_text(generated_id, encoding="utf-8")
    os.environ["AGENT_ID"] = generated_id
    return generated_id


class Settings(BaseModel):
    """应用配置（Pydantic 校验）。

    环境变量键名与 `.env.example` 一致；未设置时使用 `Settings.load()` 内默认值。
    """

    model_config = ConfigDict(frozen=True)

    # --- LLM（OpenAI 兼容）---
    llm_provider: str = "openai"
    llm_api_key: str = ""
    llm_api_base: str = ""
    llm_model: str = ""
    llm_timeout: int = 120
    llm_enable_thinking: bool = False
    llm_thinking_budget: int = 256
    llm_max_tool_loops: int = 16
    # summary 静默压缩触发阈值（估算 token 数）；<=0 表示关闭静默压缩
    summary_compression_silent_trigger_tokens: int = 4000
    # summary 显式阻塞压缩触发阈值（估算 token 数）；<=0 表示关闭阻塞压缩
    summary_compression_blocking_trigger_tokens: int = 8000
    # 流式 completions 是否请求末尾 chunk 携带 usage（部分兼容网关不支持 `stream_options`）
    llm_stream_include_usage: bool = True
    # 是否启用 skills 动态注入 system prompt
    agent_skills_enabled: bool = True
    # skills 根目录（默认仓库根目录下 `skills/`）
    agent_skills_dir: str = "skills"
    # 单轮最多注入多少个匹配 skill
    agent_skills_max_in_prompt: int = 3

    # --- 可观测性 ---
    metrics_enabled: bool = True

    # --- 队列（MVP）---
    max_queue_size: int = 0
    agent_max_active_session_queues: int = 3
    # 活跃 session 达上限时：仅当某 session 最后活动时间早于该秒数，才允许按 LRU 闲置顺序淘汰以接纳新 session；<=0 关闭该机制
    agent_session_idle_evict_seconds: int = 300

    # --- CLI（方案二：HTTP 客户端）---
    agent_cli_mode: str = "api"
    agent_api_base: str = "http://127.0.0.1:8000"
    # API CORS 允许来源（逗号分隔）；用于浏览器开发调试
    api_cors_allow_origins: list[str] = ["http://localhost:5173", "http://127.0.0.1:5173"]
    agent_id: str = ""
    agent_id_file_path: str = ".runtime/agent/agent_id"
    registry_url: str = ""
    discovery_groups: list[str] = []
    agent_public_base_url: str = ""
    agent_peer_cache_ttl_seconds: int = 60
    agent_peer_delivery_mode: str = "direct"
    agent_peer_stream_timeout_seconds: int = 60
    agent_peer_broadcast_stream_timeout_seconds: int = 20
    # 全局工具审批模式：always（总是审批）/ never（永不审批）/ rule（按规则函数判断）
    agent_tool_approval_mode: str = "rule"
    # bash_run/cmd/powershell 输出解码编码（Windows 中文环境可用 gbk/cp936）
    bash_output_encoding: str = "utf-8"

    # --- 会话消息落盘 ---
    agent_session_store_path: str = ".runtime/memory/session.sqlite3"

    # --- 兼容旧变量 ---
    openai_api_key: str = ""

    @classmethod
    def load(cls) -> Self:
        """从当前进程环境读取（请先 `load_env()` 以加载 `.env`）。"""
        agent_id_file_path = _agent_id_file_path()
        agent_id = _resolve_agent_id(agent_id_file_path)
        return cls(
            llm_provider=_env_str("LLM_PROVIDER") or "openai",
            llm_api_key=_env_str("LLM_API_KEY"),
            llm_api_base=_env_str("LLM_API_BASE"),
            llm_model=_env_str("LLM_MODEL"),
            llm_timeout=_env_int("LLM_TIMEOUT", 120),
            llm_enable_thinking=_env_bool("LLM_ENABLE_THINKING", False),
            llm_thinking_budget=_env_int("LLM_THINKING_BUDGET", 256),
            llm_max_tool_loops=_env_int("LLM_MAX_TOOL_LOOPS", 16),
            summary_compression_silent_trigger_tokens=_env_int(
                "SUMMARY_COMPRESSION_SILENT_TRIGGER_TOKENS",
                4000,
            ),
            summary_compression_blocking_trigger_tokens=_env_int(
                "SUMMARY_COMPRESSION_BLOCKING_TRIGGER_TOKENS",
                8000,
            ),
            llm_stream_include_usage=_env_bool("LLM_STREAM_INCLUDE_USAGE", True),
            agent_skills_enabled=_env_bool("AGENT_SKILLS_ENABLED", True),
            agent_skills_dir=_env_str("AGENT_SKILLS_DIR", "skills") or "skills",
            agent_skills_max_in_prompt=_env_int("AGENT_SKILLS_MAX_IN_PROMPT", 3),
            metrics_enabled=_env_bool("METRICS_ENABLED", True),
            max_queue_size=_env_int("MAX_QUEUE_SIZE", 0),
            agent_max_active_session_queues=_env_int("AGENT_MAX_ACTIVE_SESSION_QUEUES", 3),
            agent_session_idle_evict_seconds=_env_int("AGENT_SESSION_IDLE_EVICT_SECONDS", 300),
            agent_cli_mode=_env_str("AGENT_CLI_MODE") or "api",
            agent_api_base=_env_str("AGENT_API_BASE") or "http://127.0.0.1:8000",
            api_cors_allow_origins=(
                _env_csv("API_CORS_ALLOW_ORIGINS")
                or ["http://localhost:5173", "http://127.0.0.1:5173"]
            ),
            agent_id=agent_id,
            agent_id_file_path=agent_id_file_path,
            registry_url=_env_str("REGISTRY_URL"),
            discovery_groups=_env_csv("DISCOVERY_GROUPS"),
            agent_public_base_url=_env_str("AGENT_PUBLIC_BASE_URL"),
            agent_peer_cache_ttl_seconds=_env_int("AGENT_PEER_CACHE_TTL_SECONDS", 60),
            agent_peer_delivery_mode=_env_str("AGENT_PEER_DELIVERY_MODE", "direct") or "direct",
            agent_peer_stream_timeout_seconds=_env_int("AGENT_PEER_STREAM_TIMEOUT_SECONDS", 60),
            agent_peer_broadcast_stream_timeout_seconds=_env_int(
                "AGENT_PEER_BROADCAST_STREAM_TIMEOUT_SECONDS",
                20,
            ),
            agent_tool_approval_mode=_env_str("AGENT_TOOL_APPROVAL_MODE", "rule") or "rule",
            bash_output_encoding=_env_str("BASH_OUTPUT_ENCODING", "utf-8") or "utf-8",
            agent_session_store_path=_agent_session_store_path(),
            openai_api_key=_env_str("OPENAI_API_KEY"),
        )


_settings: Settings | None = None


def get_settings(*, reload: bool = False) -> Settings:
    """单例；`reload=True` 时重新读取环境变量。"""
    global _settings
    if _settings is None or reload:
        _settings = Settings.load()
    return _settings
