"""Agent 工具统一入口：装饰器、注册与 OpenAI tool 适配。"""

from __future__ import annotations

import functools
import inspect
import json
import os
from pathlib import Path
from typing import Any, Callable

from app.config.env import resolve_runtime_root
from app.config.runtime_layout import shell_policy_dir, tool_policy_file_path
from app.config.settings import get_settings
from app.context.models import OpenAIConversationContext
from app.harness.tools.async_store import get_async_tool_result_store
from pydantic import BaseModel, ConfigDict, Field

ApprovalMode = str
_VALID_APPROVAL_MODES: set[str] = {"always", "never", "rule"}


def _resolve_repo_relative_path(rel_or_abs: str) -> Path:
    """将策略相关路径解析为绝对路径。

    逻辑：
    1. `expanduser` 展开用户主目录；
    2. 已为绝对路径则 `resolve`；
    3. 否则锚定 **`resolve_runtime_root()`**（源码仓库根或 frozen 可执行目录），与 **`runtime_layout`** 会话/sqlite 等路径锚定规则一致。

    关键边界：
    - 相对路径不以进程 cwd 为锚点，避免在不同启动目录下误读策略文件。
    """

    p = Path(rel_or_abs).expanduser()
    if p.is_absolute():
        return p.resolve()
    return (resolve_runtime_root() / p).resolve()


def _normalize_approval_mode(raw: str | None, *, default: str = "rule") -> str:
    """规范化审批模式字符串。

    逻辑：
    1. 将输入转为小写并去除首尾空白；
    2. 若命中 `always/never/rule` 之一则返回；
    3. 其余值回退到 `default`。
    """
    normalized = str(raw or "").strip().lower()
    if normalized in _VALID_APPROVAL_MODES:
        return normalized
    return default


def _policy_entry_map_from_file(path: Path) -> dict[str, str]:
    """读取 `key=mode` 策略文件并返回映射。

    逻辑：
    1. 文件不存在时返回空映射；
    2. 按行解析 `key=mode`，忽略空行与 `#` 注释；
    3. key 标准化为小写，mode 规范为 `always/never/rule`。
    """
    if not path.is_file():
        return {}
    content = path.read_text(encoding="utf-8", errors="replace")
    mapping: dict[str, str] = {}
    for line in content.splitlines():
        stripped = line.strip()
        if not stripped or stripped.startswith("#"):
            continue
        if "=" not in stripped:
            continue
        key_raw, mode_raw = stripped.split("=", 1)
        key = key_raw.strip().lower()
        if not key:
            continue
        mapping[key] = _normalize_approval_mode(mode_raw, default="rule")
    return mapping


def _ensure_approval_policy_files() -> None:
    """确保工具级与 shell 级审批策略文件存在。

    逻辑：
    1. 创建工具策略文件父目录与文件；
    2. 创建 shell 策略目录；
    3. 为 `bash/cmd/powershell` 各自创建 `<shell>.approval.txt`。
    """
    tool_policy = tool_policy_file_path()
    tool_policy.parent.mkdir(parents=True, exist_ok=True)
    if not tool_policy.exists():
        tool_policy.write_text("", encoding="utf-8")
    shell_dir = shell_policy_dir()
    shell_dir.mkdir(parents=True, exist_ok=True)
    for shell_name in ("bash", "cmd", "powershell"):
        shell_file = shell_dir / f"{shell_name}.approval.txt"
        if not shell_file.exists():
            shell_file.write_text("", encoding="utf-8")


def _tool_approval_mode(tool_name: str) -> str:
    """读取指定工具的审批模式（`tool_name=mode`）。"""
    _ensure_approval_policy_files()
    mapping = _policy_entry_map_from_file(tool_policy_file_path())
    return mapping.get(tool_name.strip().lower(), "rule")


def _shell_command_approval_mode(shell_type: str, root_command: str) -> str:
    """读取指定 shell 命令首词的审批模式（`root=mode`）。"""
    _ensure_approval_policy_files()
    policy_file = shell_policy_dir() / f"{shell_type}.approval.txt"
    mapping = _policy_entry_map_from_file(policy_file)
    return mapping.get(root_command.strip().lower(), "rule")


def _should_require_approval_for_bash_tool(tool_args: dict[str, Any]) -> bool:
    """按 shell 策略文件判断 `bash_run` 是否需要审批。

    逻辑：
    1. 从参数读取 `command/shell_type`，并解析最终 shell 类型；
    2. 按 shell 语法拆分命令，提取每个片段的 root command；
    3. 读取 `<shell>.approval.txt` 的 `root=mode` 映射；
    4. 只要任一 root 判定为 `always/rule` 则要求审批；全部 `never` 才放行。

    关键边界：
    - 参数缺失、shell 解析失败、命令片段不可识别等场景均保守返回 `True`；
    - `rule` 在当前阶段仍回退为“需要审批”。
    """
    try:
        from app.harness.tools import bash as bash_tool

        raw_command = str(tool_args.get("command") or "").strip()
        if not raw_command:
            return True
        raw_shell_type = tool_args.get("shell_type")
        if isinstance(raw_shell_type, str):
            cleaned_shell_type = raw_shell_type.strip().lower()
            explicit_shell_type = cleaned_shell_type if cleaned_shell_type in {"bash", "cmd", "powershell"} else None
        else:
            explicit_shell_type = None
        final_shell = str(bash_tool._resolve_shell_type(explicit_shell_type)).strip().lower()
        if final_shell not in {"bash", "cmd", "powershell"}:
            return True
        nodes = bash_tool._parse_command_ast(raw_command, final_shell)
        if not nodes:
            return True
        mode_list: list[str] = []
        for node in nodes:
            root = str(getattr(node, "root", "") or "").strip().lower()
            if not root:
                return True
            mode_list.append(_shell_command_approval_mode(final_shell, root))
        if any(mode in {"always", "rule"} for mode in mode_list):
            return True
        return False
    except Exception:
        return True


def _decorate_sync_tool(
    *,
    func: Callable[..., Any],
    final_name: str,
    description: str,
) -> Callable[..., Any]:
    """构建同步工具装饰结果。

    逻辑：
    1. 不包裹原函数，保持原始签名与调用行为；
    2. 仅挂载 `name/description` 元数据供注册层读取；
    3. 返回原函数对象。

    关键边界：
    - 不做任何参数注入或执行改写；
    - 调用异常由原函数自身语义决定。

    副作用说明：
    - 会给函数对象写入 `name` 与 `description` 属性。
    """
    func.name = final_name  # type: ignore[attr-defined]
    func.description = description  # type: ignore[attr-defined]
    return func


def _decorate_async_tool(
    *,
    func: Callable[..., Any],
    final_name: str,
    description: str,
) -> Callable[..., Any]:
    """构建异步工具装饰结果（后台提交 + 立即 ACK）。

    逻辑：
    1. 检测函数签名是否声明 `context` 参数；
    2. 包装后调用原始 async 函数拿协程对象（不在此 await）；
    3. 从 `context.session_id` / **`context.sse_client_id`** 解析会话与 SSE 通道并提交到 **`AsyncToolResultStore`**；
    4. 返回包含 `job_id` 的受理文案。

    关键边界：
    - 无运行中事件循环、缺少 **`session_id`** 或 **`sse_client_id`** 时，提交会抛异常；
    - 若原函数不接收 `context`，会在调用前移除该参数以保持兼容。

    与外部交互：
    - 调用 `app.harness.tools.async_store.get_async_tool_result_store` 提交后台任务。

    异常说明：
    - 不吞异常；提交失败由上层按工具失败链路处理。

    副作用说明：
    - 返回的是包装函数，但会保留原函数元信息并挂载 `name/description`。
    """
    accepts_context = "context" in inspect.signature(func).parameters

    @functools.wraps(func)
    def _wrapped_async_tool(*args: Any, **kwargs: Any) -> str:
        ctx = kwargs.get("context")
        call_kwargs = dict(kwargs)
        if not accepts_context:
            call_kwargs.pop("context", None)
        coro = func(*args, **call_kwargs)
        session_id = ""
        client_id = ""
        if isinstance(ctx, OpenAIConversationContext):
            session_id = ctx.session_id
            client_id = (ctx.sse_client_id or "").strip()
        if not client_id:
            raise ValueError(
                "异步工具缺少 client_id：请确保本会话已处理过带 client_id 的入站消息（已写入 "
                "OpenAIConversationContext.sse_client_id），否则异步结果无法投递到 SSE。"
            )
        store = get_async_tool_result_store()
        job = store.submit_coroutine(
            session_id=session_id,
            client_id=client_id,
            tool_name=final_name,
            coroutine_obj=coro,
        )
        return (
            f"工具 {final_name} 已执行并转为后台任务，job_id={job.job_id}。"
            "任务完成后将自动推送结果。"
        )

    _wrapped_async_tool.name = final_name  # type: ignore[attr-defined]
    _wrapped_async_tool.description = description  # type: ignore[attr-defined]
    return _wrapped_async_tool


def tool(
    name: str | None = None,
) -> Callable[[Callable[..., Any]], Callable[..., Any]] | Callable[..., Any]:
    """声明一个可注册的项目工具函数。

    逻辑：
    1. 兼容 `@tool("name")`、`@tool()` 与 `@tool` 三种写法；
    2. 当 `name` 未传或为空白时，自动使用函数名作为工具名；
    3. 将工具名写入 `func.name`，将 docstring 写入 `func.description`。

    关键分支/边界：
    - `name` 为空字符串时会回退函数名，而不是报错；
    - 函数名为空（极端异常）才会抛 `ValueError`；
    - 不包裹原函数，避免破坏签名，便于后续自动生成 JSON Schema。

    异常说明：
    - 解析出空工具名时抛出 `ValueError`。

    副作用说明：
    - 会给被装饰函数挂载 `name` 与 `description` 属性。

    Args:
        name: 工具注册名（可选）；为空时使用函数名。

    Returns:
        装饰器或装饰后的函数对象。
    """

    def decorator(func: Callable[..., Any]) -> Callable[..., Any]:
        final_name = (name or "").strip() or func.__name__.strip()
        if not final_name:
            raise ValueError("tool name 不能为空，且函数名不能为空")
        description = inspect.getdoc(func) or ""
        if inspect.iscoroutinefunction(func):
            # 异步工具分支：后台提交任务并立即返回 ACK。
            return _decorate_async_tool(
                func=func,
                final_name=final_name,
                description=description,
            )
        else:
            # 同步工具分支：保持原函数执行行为不变，仅注入元数据。
            return _decorate_sync_tool(
                func=func,
                final_name=final_name,
                description=description,
            )

    if callable(name):
        # 支持无参直接写法：@tool
        func = name
        name = None
        return decorator(func)
    else:
        return decorator


def should_require_tool_approval(
    *,
    tool_name: str,
    tool_args: dict[str, Any],
    context: OpenAIConversationContext | None = None,
) -> bool:
    """判断某次工具调用是否需要进入审批流程（占位实现）。

    逻辑：
    1. 接收工具名、工具参数与可选会话上下文，作为审批策略判断输入；
    2. 读取全局配置 `AGENT_TOOL_APPROVAL_MODE`，支持 `always/never/rule` 三种模式；
    3. `always` 强制审批，`never` 直接放行，`rule` 进入规则分支；
    4. 规则分支先读工具级策略文件（默认 **`.runtime/policy/tool.approval.txt`**，行格式 `tool_name=mode`），并对 `bash_run` 额外读取 shell 级目录（默认 **`.runtime/policy/shell/`** 下各 `<shell>.approval.txt`）的 `root=mode` 策略。

    关键边界：
    - 本方法应保持纯函数语义，不直接修改 `context` 或触发外部副作用；
    - 当 `AGENT_TOOL_APPROVAL_MODE` 取值非法时，回退为 `rule`；
    - 当策略未命中任何规则时，维持“默认需要审批”的保守行为；
    - `bash_run` 的 `rule` 由 shell 策略文件细分，当前 `rule` 仍回退为需要审批。

    Args:
        tool_name: 工具名（如 `bash_run`、`agent_send_message`）。
        tool_args: 本次工具调用参数（已解析为字典）。
        context: 当前会话上下文（可选，供后续策略扩展使用）。

    Returns:
        bool: `True` 表示进入审批；`False` 表示可自动执行。
    """
    mode_raw = (get_settings().agent_tool_approval_mode or "rule").strip().lower()
    mode = mode_raw if mode_raw in {"always", "never", "rule"} else "rule"
    if mode == "always":
        return True
    elif mode == "never":
        return False
    else:
        del context
        tool_mode = _tool_approval_mode(tool_name)
        if tool_mode == "always":
            return True
        elif tool_mode == "never":
            return False
        else:
            if tool_name.strip().lower() == "bash_run":
                return _should_require_approval_for_bash_tool(tool_args)
            return True


def get_tools() -> list[Any]:
    """返回供 OpenAI runtime 注册的工具列表。

    逻辑：
    1. 采用函数内导入，避免工具定义模块反向导入 `tool` 时出现循环依赖；
    2. 收集当前启用工具并按固定顺序返回，便于稳定测试与观测。
    """
    from app.harness.tools.agent_peer import (
        agent_broadcast,
        agent_discover,
        agent_peer_approve_tools,
        agent_send_message,
    )
    from app.harness.tools.bash import bash_run
    from app.harness.tools.fs import edit_file, read_file, search_file, write_file
    from app.harness.tools.skills import load_skills

    # 先最小集启用，后续可按稳定性逐步放开更多工具。
    return [
        load_skills,
        read_file,
        search_file,
        edit_file,
        write_file,
        bash_run,
        agent_discover,
        agent_send_message,
        agent_broadcast,
        agent_peer_approve_tools,
    ]


class OpenAIToolSpec(BaseModel):
    """OpenAI 工具规格与可执行函数绑定。

    逻辑：
    1. `name/description/parameters` 对齐 OpenAI tools `function` 声明结构；
    2. `invoke` 保存运行时可直接调用的执行入口（含可选 `context` 注入）；
    3. `build_openai_toolkit` 基于该模型同时产出“模型可见 schema”与“本地可执行映射”。

    关键边界：
    - `invoke` 为可调用对象，需允许 runtime 在工具调用阶段按 `args` 执行；
    - `parameters` 默认空对象，避免未配置 schema 时出现空值分支。
    """

    model_config = ConfigDict(arbitrary_types_allowed=True, frozen=True)

    name: str
    description: str
    parameters: dict[str, Any] = Field(default_factory=dict)
    invoke: Callable[[dict[str, Any], OpenAIConversationContext | None], Any]


def _signature_to_json_schema(func: Callable[..., Any]) -> dict[str, Any]:
    """将 Python 签名转换为最小 JSON Schema。

    逻辑：
    1. 遍历函数参数，生成 `properties`；
    2. 推断基础类型（str/int/float/bool/其他）；
    3. 无默认值参数放入 `required`。
    """
    sig = inspect.signature(func)
    properties: dict[str, Any] = {}
    required: list[str] = []
    for param_name, p in sig.parameters.items():
        # `context` 由 runtime 注入，不应暴露给模型参数 schema。
        if param_name == "context":
            continue
        if p.kind in (inspect.Parameter.VAR_POSITIONAL, inspect.Parameter.VAR_KEYWORD):
            continue
        anno = p.annotation
        if anno in (int,):
            t = "integer"
        elif anno in (float,):
            t = "number"
        elif anno in (bool,):
            t = "boolean"
        else:
            t = "string"
        properties[param_name] = {"type": t}
        if p.default is inspect._empty:
            required.append(param_name)
    return {
        "type": "object",
        "properties": properties,
        "required": required,
        "additionalProperties": True,
    }


def _tool_to_spec(tool_obj: Any) -> OpenAIToolSpec:
    """将单个工具对象转换为 OpenAI 可注册规格。

    逻辑：
    1. 读取工具元数据：优先 `tool_obj.name/description`，回退到函数名与默认描述；
    2. 解析参数 schema：优先 `args_schema.model_json_schema()`，失败回退签名推导；
    3. 构造 `_invoke(args, context)`：统一运行时调用入口，处理 context 注入与函数调用方式差异；
    4. 返回 `OpenAIToolSpec`，供后续批量注册与执行映射使用。

    关键分支/边界：
    - 工具对象存在 `invoke` 方法时，执行走对象入口；否则按函数关键字参数调用；
    - 工具函数声明 `context` 参数时才注入 runtime context，避免污染不接收该参数的工具；
    - schema 生成失败时回退到签名推导，保证工具仍可被注册。

    与外部交互：
    - 读取工具对象上的 `name/description/args_schema/invoke` 约定字段；
    - 依赖 `inspect.signature` 与 pydantic schema 能力完成参数模型构建。
    """
    name = getattr(tool_obj, "name", None) or getattr(tool_obj, "__name__", "tool")
    description = (getattr(tool_obj, "description", "") or "").strip()
    invoke_fn = getattr(tool_obj, "invoke", None)
    if not callable(invoke_fn):
        # 回退：普通函数
        invoke_fn = tool_obj

    parameters: dict[str, Any]
    args_schema = getattr(tool_obj, "args_schema", None)
    if args_schema is not None and hasattr(args_schema, "model_json_schema"):
        try:
            parameters = args_schema.model_json_schema()  # pydantic v2
        except Exception:
            parameters = _signature_to_json_schema(invoke_fn)
    else:
        parameters = _signature_to_json_schema(invoke_fn)

    fn_sig = inspect.signature(invoke_fn)
    accepts_context = "context" in fn_sig.parameters

    def _invoke(args: dict[str, Any], context: OpenAIConversationContext | None = None) -> Any:
        # 带 `invoke` 的工具对象走统一入口；否则按 Python 关键字展开（可注入 `context`）。
        if getattr(tool_obj, "invoke", None):
            return tool_obj.invoke(args)
        else:
            final_kwargs = dict(args)
            if accepts_context:
                final_kwargs["context"] = context
            return invoke_fn(**final_kwargs)

    return OpenAIToolSpec(
        name=name,
        description=description or f"调用工具 {name}",
        parameters=parameters,
        invoke=_invoke,
    )


def build_openai_toolkit() -> tuple[list[dict[str, Any]], dict[str, OpenAIToolSpec]]:
    """构建 OpenAI tools payload 与执行映射。

    逻辑：
    1. 调用 `get_tools()` 获取当前启用工具列表；
    2. 逐个工具转换为 `OpenAIToolSpec`；
    3. 组装 OpenAI 侧 `tools_payload`（`type=function` + `function{name,description,parameters}`）；
    4. 同步生成本地执行映射 `tool_map[name] = OpenAIToolSpec`。

    关键边界：
    - `tools_payload` 与 `tool_map` 必须同源，保证模型侧声明与执行侧映射一致；
    - 若工具列表为空，返回空 payload 与空映射，调用方需自行决定是否允许无工具模式。

    Returns:
        tuple[list[dict[str, Any]], dict[str, OpenAIToolSpec]]:
            第一个元素为传给 OpenAI 的 tools 声明；
            第二个元素为本地执行映射（按工具名索引）。
    """
    specs = [_tool_to_spec(t) for t in get_tools()]
    tools_payload = [
        {
            "type": "function",
            "function": {
                "name": s.name,
                "description": s.description,
                "parameters": s.parameters,
            },
        }
        for s in specs
    ]
    return tools_payload, {s.name: s for s in specs}


def parse_tool_arguments(raw: str | dict[str, Any] | None) -> dict[str, Any]:
    """解析 OpenAI tool arguments（JSON 字符串或对象）。

    逻辑：
    1. `None` 直接返回空字典；
    2. 已是 `dict` 时原样返回；
    3. 其余输入按字符串处理并尝试 `json.loads`；
    4. 仅当解析结果为 `dict` 时返回，否则回退空字典。

    关键边界：
    - 非法 JSON、空字符串、非对象 JSON（如列表/数字）均不抛异常，统一返回 `{}`；
    - 本函数只做轻量容错解析，不做字段级语义校验（校验由工具函数自身完成）。
    """
    if raw is None:
        return {}
    if isinstance(raw, dict):
        return raw
    text = str(raw).strip()
    if not text:
        return {}
    try:
        data = json.loads(text)
    except Exception:
        return {}
    return data if isinstance(data, dict) else {}