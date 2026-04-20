"""常用工具占位模块。

用于后续添加通用/测试工具函数。
"""

from __future__ import annotations

from langgraph.types import interrupt
from app.context.models import OpenAIConversationContext
from app.harness.tools.tooling import tool

@tool("calc_add")
def calc_add(a: float, b: float, context: OpenAIConversationContext | None = None) -> str:
    """使用场景：需要对两个数求和且必须先经过人工确认时使用。

    字段说明：
    - `a`：第一个数字（必填，`float`）。
    - `b`：第二个数字（必填，`float`）。

    返回说明：
    - 确认通过：返回执行参数与计算结果。
    - 拒绝或中断：返回拒绝提示文本。

    调用范例：
    - `calc_add({"a":1,"b":2})`
    - `calc_add({"a":3.5,"b":4.2})`
    """
    approval_result = interrupt(
        {
            "interrupt_type": "execute_tool",
            "message": f"工具调用【calc_add】，参数：a={a}, b={b}",
            "description": "计算两个数字之和",
            "args": {"a": a, "b": b},
        }        
    )
    if approval_result.get("type") == "approve":
        a = approval_result.get("a", a)
        b = approval_result.get("b", b)
        result = a + b
        return f"用户确认后执行工具调用，参数：a={a}, b={b}\n执行结果：{result}"
    else:
        return "用户拒绝执行工具调用"


