"""项目内工具装饰器：替代外部框架的 `@tool`。"""

from __future__ import annotations

import functools
import inspect
from typing import Any, Callable

from app.context.models import OpenAIConversationContext
from app.harness.tools.async_store import get_async_tool_result_store


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
    3. 从 `context.session_id` 解析会话标识并提交到 `AsyncToolResultStore`；
    4. 返回包含 `job_id` 的受理文案。

    关键边界：
    - 无运行中事件循环或缺少 `session_id` 时，提交会抛异常；
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
        if isinstance(ctx, OpenAIConversationContext):
            session_id = ctx.session_id
        store = get_async_tool_result_store()
        job = store.submit_coroutine(
            session_id=session_id,
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

    与外部交互：
    - 无。

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
    return decorator
