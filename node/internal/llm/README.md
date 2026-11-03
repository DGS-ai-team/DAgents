# node/internal/llm

OpenAI 兼容 Chat Completions 客户端与厂商消息适配。

## 职责

| 文件 | 说明 |
|------|------|
| `types.go` | `Message`、`ChatRequest`、`Client` 接口 |
| `messages.go` | `MessagesWithSystem` |
| `messageutil.go` | `CloneMessage`、`EstimateMessageTokens`、`MessageToDeepSeekAPIPayload`、`MessageToJournalPayload` |
| `tool_result_visibility.go` | tool result status/error 的 history 元数据与模型请求侧 metadata 适配 |
| `openai.go` | HTTP/SSE、`tool_calls` 增量合并 |
| `usage.go` | OpenAI/DeepSeek usage 归一化、cache 可用性和 reasoning token 统计 |
| `provider.go` | `MessageAdapter` 接口与工厂 |
| `provider_openai.go` / `provider_deepseek.go` / `provider_qwen.go` / `provider_vllm.go` | 厂商存储规范化与出站序列化 |
| `client_adapter.go` | `adapterClient` 包装 HTTP 客户端 |
| `factory.go` | 从 config 构造生产 `Client` |

## MessageAdapter 流水线

```
history Message
  → NormalizeAssistantForStorage   （写入 session history）
  → PrepareToolResultMessagesForModel （仅请求副本增加 [TOOL_RESULT_METADATA]）
  → PrepareOutboundMessages        （出站前裁剪 []Message）
  → MarshalChatRequestMessages     （HTTP messages 字段；openai 返回 ok=false）
  → RequestExtra                   （合并进 POST body 顶层：user_id=agent_id + thinking 等）
```

| `provider` | 默认 `base_url` | thinking 控制 | 状态栏控制 |
|------------|-----------------|---------------|------------|
| `openai` | （需配置） | OpenAI-compatible（按网关实现） | 思考 + 推理强度 |
| `deepseek` | `https://api.deepseek.com` | `thinking` + `reasoning_effort` | 思考 + 推理强度 |
| `qwen` | DashScope compatible-mode | `enable_thinking` + `thinking_budget` | 思考 + 思考预算 |
| `glm` | `https://open.bigmodel.cn/api/paas/v4` | `thinking` + preserved `reasoning_content` | 思考开关 |
| `minimax` | `https://api.minimaxi.com/v1` | `thinking` + `reasoning_split` | M3 思考开关；旧模型固定开启 |
| `mimo` | `https://api.xiaomimimo.com/v1` | `thinking` | 深度思考开关 |
| `vllm` | `http://127.0.0.1:8000/v1` | 否（取决于部署模型） | 不展示 |

DeepSeek / Qwen 的 `reasoning_content` 出站规则集中在对应 adapter；`openai.go` 不含厂商分支。

工具结果的原始正文继续用于 hydrate、UI 和历史审计；模型请求副本在每条 `tool` 消息正文前增加紧凑的
`[TOOL_RESULT_METADATA]`，让模型可读取运行时的 `status/error`，而 `MessageToAPIPayload` 不会把内部
`tool_result_metadata` 字段作为非标准 provider 字段发送。

模型能力由 `/v1/llm/capabilities` 按 provider 与模型名解析；未知模型不展示思考控制，
也不会收到推理参数。参数保存在各自的 LLM profile，`PATCH /v1/llm/settings` 只切换
active profile，避免 Node 级开关覆盖不同 Agent 的模型配置。

## Verified reasoning controls

| Provider / model family | UI control and request fields | Source |
|---|---|---|
| DeepSeek Flash / Pro, including V4 | Toggle + `reasoning_effort` (`low`, `high`, `max`); `thinking.type` | [Thinking Mode](https://api-docs.deepseek.com/guides/thinking_mode/) |
| OpenAI o1/o3/o3-mini/o4-mini, GPT-5, GPT-5.1 | `reasoning_effort` only; GPT-5.1 allows `none`, GPT-5 classic allows `minimal`. No DeepSeek `thinking` object. Other OpenAI models remain unsupported here. | [Reasoning guide](https://developers.openai.com/api/docs/guides/reasoning), [GPT-5.1](https://developers.openai.com/api/docs/models/gpt-5.1) |
| Qwen Plus / Flash and listed Qwen3 hybrid models | Toggle + optional integer `thinking_budget`; thinking-only variants keep thinking on. An empty budget leaves the service default intact. | [Deep Thinking](https://help.aliyun.com/zh/model-studio/deep-thinking) |
| GLM 4.5/4.6/4.7 | Toggle + preserved `reasoning_content` mode | [Thinking](https://docs.bigmodel.cn/cn/guide/capabilities/thinking) |
| GLM 5.2 / 5.3 families | Effort (`high`/`max`, or `low`/`high`/`max` for 5.3); defaults to `max`. 5.3 and Flash cannot be disabled. | [Thinking](https://docs.bigmodel.cn/cn/guide/capabilities/thinking) |
| MiniMax M3 | Adaptive / disabled thinking | [Text OpenAI API](https://platform.minimax.io/docs/api-reference/text-openai-api) |
| MiniMax M3.1-Flash | Effort (`low`, `medium`, `high`, `xhigh`, `max`) via `reasoning_effort`, default `max`; thinking stays enabled. | [Text OpenAI API](https://platform.minimax.io/docs/api-reference/text-openai-api) |
| MiniMax M2.x | Thinking remains enabled; no toggle. | [Text OpenAI API](https://platform.minimax.io/docs/api-reference/text-openai-api) |
| MiMo v2.5 and v2-Flash | Thinking toggle | [Reasoning content](https://platform.xiaomimimo.com/docs/en-US/usage-guide/passing-back-reasoning_content) |

OpenAI model requests remain on the existing Chat Completions transport. This table does not imply
support for model families whose reasoning controls require another transport.

## 相关文档

- 架构：[`docs/architecture/go-node-internals.md`](../../../docs/architecture/go-node-internals.md)
- History JSONL：[`../history/README.md`](../history/README.md)
