# 内置工具一览

本文说明 **当前注册进 OpenAI 运行时** 的内置工具：来源为 **`app/harness/tools/tool.py`** 中的 **`get_tools()`**，经 **`build_openai_toolkit()`** 生成 **tools JSON** 与 **`tool_map`**（**`app/core/main_agent/runtime_openai.py`** 请求模型时使用）。**`function.description`** 取自工具函数的 **docstring**；**`function.parameters`** 优先来自 **`args_schema.model_json_schema()`**，否则由 **`_signature_to_json_schema`** 从签名推导（见下文 **「附」**）。执行时由编排层 **`_invoke_tool`** 调用 **`OpenAIToolSpec.invoke`**。

**审批**：是否进入 **`approval_required`** 由 **`decide_tool_approval`**（**`tool.py`**）结合 **`AGENT_TOOL_APPROVAL_MODE`**（**`always` / `never` / `rule`**）及 **`.runtime/policy/`** 下策略文件决定；返回值包含 **是否审批、原因、风险等级、策略来源**，并由审批卡片透出；兼容布尔入口 **`should_require_tool_approval`**。

---

## 附：`@tool` / `tool()` 装饰逻辑、`docstring`、传给 LLM 的声明与参数执行管道

以下描述 **`app/harness/tools/tool.py`** 与 **`app/core/main_agent/runtime_openai.py`**、**`app/core/main_agent/agent.py`** 之间的数据流。暴露给模型的 **`@tool`** 函数，其 docstring 建议采用 **「使用场景 → 字段说明 → 返回说明 → 调用范例」** 四段结构（面向调用契约，不写实现细节）。

### 附.1 `tool(...)` 装饰器本身的逻辑

实现入口为 **`tool(name: str | None = None)`**（**`tool.py`**）。装饰阶段 **不** 与 OpenAI 通信，只准备 **注册名**、**`description`** 元数据，并决定 **同步 / 异步** 两条装配路径。

**1）调用形态分流（最外层）**

| 写法 | 解析方式 |
|------|----------|
| **`@tool("my_tool")`** | 第一个参数为 **字符串**，返回 **`decorator`**，待 Python 再传入被装饰函数。 |
| **`@tool()`** | 第一个参数非可调用，**`name` 为 `None`**，同样返回 **`decorator`**；最终工具名回退为 **`func.__name__`**。 |
| **`@tool`**（无括号） | 第一个参数 **即为被装饰函数**（**`callable(name)`** 为真）；代码将 **`func = name`**、**`name = None`**，并 **立即** **`return decorator(func)`**，等价于用 **函数名** 作为工具名。 |

**2）`decorator(func)` 内部（统一入口）**

1. **`final_name`**：`(name or "").strip() or func.__name__.strip()`；若仍为空则 **`ValueError("tool name 不能为空，且函数名不能为空")`**。  
2. **`description`**：**`inspect.getdoc(func) or ""`**（无 docstring 则为空串；**`_tool_to_spec`** 会用 **`f"调用工具 {name}"`** 作兜底描述）。  
3. **同步 / 异步**：**`inspect.iscoroutinefunction(func)`** 为真 → **`_decorate_async_tool`**；否则 → **`_decorate_sync_tool`**。

**3）`_decorate_sync_tool`（同步）**

- **不**用包装器替换原函数：仅 **`func.name = final_name`**、**`func.description = description`**，然后 **原样返回 `func`**。  
- **目的**：保持 **真实 Python 签名** 不变，便于 **`_signature_to_json_schema(inspect.signature(...))`** 与运行时 **`invoke_fn(**kwargs)`** 一致；装饰器注释写明 **「不包裹原函数」**。

**4）`_decorate_async_tool`（异步）**

- 使用 **`@functools.wraps(func)`** 定义 **`_wrapped_async_tool(*args, **kwargs)`**，对外暴露的 **可调用对象** 才是注册进 **`get_tools()`** 的引用。  
- 包装函数体内：**若被装饰函数签名不包含 `context`**，则从 **`kwargs` 中移除 `context`** 再调用原协程函数，避免 **`TypeError`**。  
- 调用原 **`async def`** 得到 **协程对象**（**不在此处 `await`**），从 **`kwargs["context"]`** 取出 **`OpenAIConversationContext`**，读取 **`session_id` / `sse_connection_id`**，向 **`AsyncToolResultStore.submit_coroutine`** 提交后台任务；**`connection_id` 为空则 `ValueError`**。  
- 立即返回 **固定格式的受理字符串**（含 **`job_id`**）。  
- 同样把 **`final_name` / `description`** 挂在包装函数上供 **`_tool_to_spec`** 读取。  
- **`functools.wraps`** 有利于保留 **docstring** 与签名元数据，供 **`inspect.getdoc` / `inspect.signature`** 在注册阶段与 **`_tool_to_spec`** 衔接（具体以 **`_tool_to_spec`** 选用的 **`invoke_fn`** 为准）。

### 附.2 工具函数的 docstring 做什么

1. **`@tool("name")` 或 `@tool` / `@tool()`** 在 **`decorator(func)`** 内执行 **`inspect.getdoc(func)`**，得到整段 docstring 字符串（见附.1）。  
2. 该字符串被写入 **对外工具对象** 上的 **`description`** 属性（同步：**原函数**；异步：**包装函数**，通常仍能通过 **`wraps` 暴露原 docstring**）。  
3. **`_tool_to_spec`** 读取 **`tool_obj.description`**，作为 OpenAI **`tools[]` → `function.description`** 的原文发给模型。  

因此：**docstring 是模型在「工具列表」里看到的唯一长说明**；宜按上述四段组织，避免把实现细节写进 docstring（模型用不上，且易与真实代码漂移）。

**注意**：docstring **不参与** JSON Schema 字段生成；**参数名 / 类型 / 必填** 来自 **Python 函数签名**（见附.4）。

### 附.3 如何传给 LLM（`tools` 载荷）

1. **`build_openai_toolkit()`** 对 **`get_tools()`** 中每个工具调用 **`_tool_to_spec`**，得到 **`OpenAIToolSpec`**（**`name` / `description` / `parameters` / `invoke`**）。  
2. 组装为 OpenAI Chat Completions 所接受的 **`tools`** 数组元素：  

```text
{ "type": "function", "function": { "name": <工具名>, "description": <docstring>, "parameters": <JSON Schema> } }
```

3. **`OpenAIImplicitReActRuntime`** 在 **`_request_model_stream`** 里把该 **`tools_payload`** 与 **`messages`**、**`system`** 一并传给 **`chat.completions.create(..., stream=True)`**（见 **`runtime_openai.py`**）。  

运行时 **不在每轮请求里改动** **`tools` 列表**（构造 **`OpenAIImplicitReActRuntime`** 时从 **`build_openai_toolkit()`** 取一份固定 payload）；模型每轮看到的工具表一致，**已加载 skills** 等会话差异体现在 **`get_system_prompt(context)`** 的系统提示里，而非动态增删 **`tools`** 条目。

### 附.4 `parameters`（JSON Schema）如何生成

| 优先级 | 行为 |
|--------|------|
| **1** | 若工具对象带 **`args_schema`** 且实现 **`model_json_schema()`**（Pydantic v2），则 **`parameters`** 取该 **JSON Schema**（可表达复杂嵌套类型）。 |
| **2** | 否则调用 **`_signature_to_json_schema(invoke_fn)`**：按 **`inspect.signature`** 遍历参数，**跳过名为 `context` 的参数**（该参数由运行时注入，**禁止出现在模型可见 schema**，避免模型伪造会话）。 |
| **类型映射** | 注解为 **`int` / `float` / `bool`** 时分别映射为 **`integer` / `number` / `boolean`**；**其余注解（含 `list[str]` 等）当前一律映射为 `string`**。 |
| **`required`** | 无默认值（**`inspect.Parameter.empty`**）的参数名加入 **`required`**。 |
| **`additionalProperties`** | Pydantic `args_schema` 由模型配置决定；高风险 shell/fs 工具使用 **`extra="forbid"`** 拒绝未知字段。签名推导路径默认 **`false`**。 |

### 附.5 模型返回后，参数如何变成 `invoke(...)` 调用

1. **`run_turn`** 在流式 **`final`** 中读取 **`tool_calls`**；对每条调用 **`parse_tool_arguments(fn.get("arguments"))`**（**`tool.py`**）。  
2. **`parse_tool_arguments`**：**`None` → `{}`**；已是 **`dict` → 原样**；字符串则 **`json.loads`**，**仅当解析结果为 `dict` 时返回**，否则 **`{}`**（非法 JSON、数组/标量根、空串均不抛异常）。  
3. 结果存入 **`PendingToolCall.arguments`**（**`dict`**）。  
4. 编排器 **`_invoke_tool`** 执行 **`spec.invoke(tool_call.arguments, ctx)`**（**`agent.py`**）。  
5. **`OpenAIToolSpec._invoke`**（**`_tool_to_spec` 内闭包**）：先用工具对象的 **`args_schema.model_validate(...)`** 做运行时参数校验（若存在），再进入实际调用。  
   - 若工具对象自带 **`invoke` 方法**：**`tool_obj.invoke(validated_args)`**（**不**自动注入 **`context`**，由该类自行处理）；  
   - 否则将 **`validated_args` 按键展开为关键字参数**：**`invoke_fn(**final_kwargs)`**；若签名包含 **`context`**，则 **`final_kwargs["context"] = ctx`**。  

**边界与建议**：

- **多余键**：带 Pydantic `args_schema` 且配置 **`extra="forbid"`** 的工具会在执行函数前返回参数校验错误；无 `args_schema` 的工具按签名过滤/展开。  
- **类型校验**：**`parse_tool_arguments` 只解析 JSON 根对象**；字段级校验由 **`_validate_tool_arguments`** 根据工具声明的 Pydantic schema 完成。  
- **返回值**：**`_invoke_tool`** 将 **`dict`/`list`** 结果 **`json.dumps`**，其余 **`str(...)`**；异常捕获为 **`ERROR: ...`** 字符串并同样走 **`tool_result`** SSE，再经 **`package_tool_result`** 生成模型上下文、展示内容和 `raw_ref` 元数据。

---

## 1. 已注册工具（固定顺序）

下列工具与 **`get_tools()`** 返回顺序一致（便于单测与日志对齐）。

| 工具名 | 执行形态 | 定义位置 | 作用概要 |
|--------|----------|----------|----------|
| **`ask_user_information`** | 编排器特殊处理 | **`app/harness/tools/user_information.py`** | 向用户询问自由文本或选项式信息；发 SSE `user_information_required`，TUI 收集回答后经 `resume(type=user_information)` 回灌为 tool 结果。 |
| **`load_skills`** | 同步 | **`app/harness/tools/skills.py`** | 按名称加载会话 **`loaded_skills`**，并返回可用技能元数据（受 **`agent_skills_max_in_prompt`** 等配置影响）。 |
| **`read_file`** | 同步 | **`app/harness/tools/fs.py`** | 流式按 **`line_offset`/`line_limit`** 分页；头含 **`next_line_offset`**；默认无行号，可用 **`include_line_numbers`** 输出 `行号<TAB>正文`。 |
| **`search_file`** | 同步 | **`fs.py`** | 流式检索；支持 regex/literal、大小写敏感开关、上下文行数与 **`next_index_offset`** 翻页；命中块建议 **`read_file`** 参数；相邻命中合并上下文。 |
| **`search_replace`** | 同步 | **`fs.py`** | 在 **`FS_ROOT`** 内按 **`old_string`/`new_string`** 精确子串替换（可选 **`replace_all`**）；保留原始文件文本并返回匹配行与 unified diff。 |
| **`write_file`** | 同步 | **`fs.py`** | 在 **`FS_ROOT`** 内写入整文件；支持父目录自动创建与 **`if_exists=overwrite/error/skip_if_same`**。 |
| **`bash_run`** | 同步 | **`app/harness/tools/bash.py`** | 统一 shell 执行（**`bash` / `cmd` / `powershell`**），含命令切段与安全策略（如非 root 下对 **`su`/`sudo`** 的拦截）。 |
| **`trigger_list`** | 同步 | **`app/harness/tools/triggers.py`** | 列出触发器资源，支持包含或过滤禁用项；只读低风险。 |
| **`trigger_get`** | 同步 | **`triggers.py`** | 查看单个触发器配置和状态；只读低风险。 |
| **`trigger_create`** | 同步 | **`triggers.py`** | 创建触发器资源，用于沉淀定时、事件、指标等自主唤起规则；默认需要审批。 |
| **`trigger_update`** | 同步 | **`triggers.py`** | 更新触发器条件、模板、风险等级或启用状态；默认需要审批。 |
| **`trigger_delete`** | 同步 | **`triggers.py`** | 删除触发器资源；默认需要审批。 |
| **`trigger_fire`** | 异步 | **`triggers.py`** | 手动触发触发器并投递到 **`AgentService`** 队列；不会绕过工具审批；默认需要审批。 |
| **`agent_discover`** | 同步 | **`app/harness/tools/agent_peer.py`** | 查询 **Register Center** 可见分组下的 Agent 列表，并尝试拉取 **`.well-known/agent-card.json`** 摘要。 |
| **`agent_send_message`** | **异步** | **`agent_peer.py`** | 向指定 **`target_agent_id`** 投递 **`AgentPeerEnvelope`**（**`direct`/`relay`**），并汇总对端 SSE；依赖 **`REGISTRY_URL`**、**`DISCOVERY_GROUPS`** 等。 |
| **`agent_broadcast`** | **异步** | **`agent_peer.py`** | 调用 **`POST /v1/broadcast`** 后并发拉取各目标 SSE。 |
| **`agent_peer_approve_tools`** | **异步** | **`agent_peer.py`** | 对对端 **`approval_required`** 提交 **`resume`**（按 **`AGENT_PEER_DELIVERY_MODE`** 直连或经 Register Center relay）。 |

### 1.1 异步工具与 `connection_id`

**`async def` + `@tool`** 的函数会走 **`_decorate_async_tool`**：**立即返回** 含 **`job_id`** 的受理文案，真实逻辑在 **`AsyncToolResultStore`** 后台协程执行；完成后以 **`async_tool_result`** 入队并走 SSE。

**硬前提**：会话 **`OpenAIConversationContext`** 上须已有非空 **`sse_connection_id`**（由带 **`connection_id`** 的入站 **`MessageEnvelope`** 刷新）。否则提交后台任务会 **`ValueError`**。详见 [agent-input-output.md](./agent-input-output.md) 与 **CHANGELOG** 中异步工具相关说明。

---

## 2. 配置与环境依赖（摘要）

| 领域 | 关键项 |
|------|--------|
| **文件工具** | 环境变量 **`FS_ROOT`**：所有路径须落在该根目录下，否则拒绝访问。 |
| **Shell** | 宿主 OS、策略文件（**`bash.py`** / **`tool.py`** 审批分支）；Windows/Linux 行为差异见 **`bash.py`**。 |
| **Skills** | **`AGENT_SKILLS_*`**（开关与注入上限等）；技能根目录固定 **`<运行根>/.runtime/skills`**；**`get_system_prompt`** 是否注入技能段由 **`agent_skills_enabled`** 等控制，与 **`load_skills`** 写入 **`ctx.loaded_skills`** 配合。 |
| **A2A** | **`REGISTRY_URL`**、**`DISCOVERY_GROUPS`**、**`AGENT_ID`**、**`AGENT_PUBLIC_BASE_URL`**（自登记）、**`AGENT_PEER_DELIVERY_MODE`**、各类超时；详见 [a2a-and-register-center.md](./a2a-and-register-center.md)。 |
| **Triggers** | **`TRIGGERS_ENABLED`**、**`TRIGGER_SCHEDULER_POLL_SECONDS`**；触发器资源固定存储在 **`<运行根>/.runtime/triggers/triggers.json`**。 |

---

## 3. 仓库内存在但未纳入 `get_tools()` 的实现

| 名称 | 位置 | 说明 |
|------|------|------|
| **`host_platform`** | **`app/harness/tools/host_platform.py`** | 已用 **`@tool("host_platform")`** 声明，**未**出现在 **`get_tools()`** 列表中，故 **当前模型不可见**。可用于后续与 **`bash_run`** 联动或 CLI。 |

新增内置工具时，除实现函数外，须在 **`get_tools()`** 中 **显式加入** 才会进入 **`build_openai_toolkit()`**（注释写明「按稳定性逐步放开」）。

---

## 4. 相关文档与源码索引

| 文档 / 路径 | 内容 |
|-------------|------|
| **`app/harness/tools/README.md`** | 各工具文件职责表 |
| **`app/harness/tools/REFERENCE.md`** | 符号级索引 |
| [a2a-and-register-center.md](./a2a-and-register-center.md) | **`agent_*`** 与 Register Center |
| [architecture-and-flows.md](./architecture-and-flows.md) | 工具在主编排中的位置 |
| [agent-turn-loop.md](./agent-turn-loop.md) | **`tool_result`** 回灌与 **`_invoke_tool`** |

---

**说明**：工具集合以 **`tool.py` → `get_tools()`** 为准；**docstring / Schema / 参数管道** 见上文 **「附」**；若与 OpenAPI/前端展示不一致，以运行时代码为准。
