"""OpenAI tool calling 适配：将现有 Python 工具封装为 OpenAI tools。"""

from __future__ import annotations

import inspect
import json
from typing import Any, Callable

from app.context.models import OpenAIConversationContext
from pydantic import BaseModel, ConfigDict, Field

from app.harness.tools.tool import get_tools


class OpenAIToolSpec(BaseModel):
    """OpenAI 工具规格与可执行函数绑定。"""

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
    for name, p in sig.parameters.items():
        # `context` 由 runtime 注入，不应暴露给模型参数 schema。
        if name == "context":
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
        properties[name] = {"type": t}
        if p.default is inspect._empty:
            required.append(name)
    return {
        "type": "object",
        "properties": properties,
        "required": required,
        "additionalProperties": True,
    }


def _tool_to_spec(tool_obj: Any) -> OpenAIToolSpec:
    """将单个工具对象转换为 OpenAI 可注册规格。"""
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

    Returns:
        - `tools_payload`：传给 OpenAI chat.completions 的 tools 列表；
        - `tool_map`：`tool_name -> OpenAIToolSpec` 执行映射。
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
    """解析 OpenAI tool arguments（JSON 字符串或对象）。"""
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
