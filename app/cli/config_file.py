from __future__ import annotations

import os
from dataclasses import dataclass
from pathlib import Path

import yaml

ENV_CONFIG_PATH = "DAGENTS_CONFIG"
DEFAULT_CONFIG_CANDIDATES = (
    "packaging/agent-client/config.yaml",
    "packaging/agent-client/config.example.yaml",
    "config.yaml",
)
DEFAULT_LISTEN_HOST = "127.0.0.1"
DEFAULT_LISTEN_PORT = 18765
DEFAULT_API_BASE = f"http://{DEFAULT_LISTEN_HOST}:{DEFAULT_LISTEN_PORT}"


@dataclass(frozen=True, slots=True)
class AgentClientConfig:
    """Node 与 Client 共用的 YAML 配置（Python CLI 侧子集）。"""

    path: str
    agent_id: str
    api_base: str


def resolve_config_path(explicit: str | None = None) -> str | None:
    """解析配置文件路径；未找到时返回 None。

    逻辑：
    1. 显式 `--config` 非空则必须存在；
    2. 否则读 `DAGENTS_CONFIG`；
    3. 否则按 DEFAULT_CONFIG_CANDIDATES 探测。
    """
    if explicit and str(explicit).strip():
        path = str(explicit).strip()
        if not os.path.isfile(path):
            raise FileNotFoundError(f"config not found: {path}")
        return path
    env_path = os.getenv(ENV_CONFIG_PATH, "").strip()
    if env_path:
        if not os.path.isfile(env_path):
            raise FileNotFoundError(f"{ENV_CONFIG_PATH}={env_path} not found")
        return env_path
    for rel in DEFAULT_CONFIG_CANDIDATES:
        if os.path.isfile(rel):
            return rel
    return None


def _runtime_dir_from_config(data: dict) -> Path:
    """由 fs_root 得到运行时根目录（与 Go `Config.RuntimeDir` 一致）。"""
    fs_root = str(data.get("fs_root") or "").strip() or "./.runtime"
    return Path(fs_root.rstrip("/"))


def _agent_id_file_path(data: dict) -> Path:
    return _runtime_dir_from_config(data) / "agent" / "agent_id"


def resolve_agent_id(data: dict) -> str:
    """解析 agent_id：文件为权威来源，缺失时由 YAML 种子或 UUID 生成并落盘。

    逻辑：
    1. `AGENT_ID` 环境变量非空时写回文件并返回；
    2. 否则读取 `.runtime/agent/agent_id`；
    3. 文件不可用则用 YAML `agent_id`，仍空则生成 UUID 并写入文件。
    """
    import uuid

    configured = os.getenv("AGENT_ID", "").strip()
    file_path = _agent_id_file_path(data)
    if configured:
        file_path.parent.mkdir(parents=True, exist_ok=True)
        file_path.write_text(configured, encoding="utf-8")
        return configured

    if file_path.is_file():
        file_id = file_path.read_text(encoding="utf-8").strip()
        if file_id:
            return file_id

    seed = str(data.get("agent_id") or "").strip()
    if not seed:
        seed = str(uuid.uuid4())
    file_path.parent.mkdir(parents=True, exist_ok=True)
    file_path.write_text(seed, encoding="utf-8")
    return seed


def load_agent_client_config(path: str) -> AgentClientConfig:
    """从 YAML 加载 Client 所需字段并应用与 Go 一致的缺省值。

    逻辑：
    1. 读文件并 os.path.expandvars；
    2. 解析 listen/local；
    3. local.endpoint 缺省时由 listen 推导。
    """
    raw_text = Path(path).read_text(encoding="utf-8")
    expanded = os.path.expandvars(raw_text)
    data = yaml.safe_load(expanded)
    if not isinstance(data, dict):
        raise ValueError(f"invalid config yaml: {path}")

    agent_id = resolve_agent_id(data)
    listen = data.get("listen") if isinstance(data.get("listen"), dict) else {}
    local = data.get("local") if isinstance(data.get("local"), dict) else {}

    host = str(listen.get("host") or "").strip() or DEFAULT_LISTEN_HOST
    port_raw = listen.get("port")
    port = int(port_raw) if port_raw else DEFAULT_LISTEN_PORT
    if port <= 0:
        port = DEFAULT_LISTEN_PORT

    endpoint = str(local.get("endpoint") or "").strip().rstrip("/")
    if not endpoint:
        endpoint = f"http://{host}:{port}"

    return AgentClientConfig(path=path, agent_id=agent_id, api_base=endpoint)


def resolve_client_settings(
    *,
    config_path: str | None,
    api_override: str | None,
    env_api_fallback: str,
) -> tuple[str, str | None]:
    """合并 YAML 配置与 CLI 覆盖项，返回 (api_base, config_path)。

    逻辑：
    1. 尝试 resolve/load YAML；
    2. `--api` 非空时覆盖 YAML；
    3. 无 YAML 时 api 使用 env_api_fallback。
    """
    resolved = resolve_config_path(config_path)
    cfg: AgentClientConfig | None = None
    if resolved:
        cfg = load_agent_client_config(resolved)

    api_base = str(api_override or "").strip().rstrip("/")
    if not api_base:
        if cfg is not None:
            api_base = cfg.api_base
        else:
            api_base = env_api_fallback.rstrip("/")

    return api_base, resolved
