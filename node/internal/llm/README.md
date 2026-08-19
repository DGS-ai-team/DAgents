# node/internal/llm

OpenAI 兼容 Chat Completions 客户端与厂商消息适配。

## 职责

| 文件 | 说明 |
|------|------|
| `types.go` | `Message`、`ChatRequest`、`Client` 接口 |
| `messages.go` | `MessagesWithSystem` |
| `messageutil.go` | `CloneMessage`、`EstimateMessageTokens`、`MessageToDeepSeekAPIPayload`、`MessageToJournalPayload` |
| `openai.go` | HTTP/SSE、`tool_calls` 增量合并 |
| `provider.go` | `MessageAdapter` 接口与工厂 |
| `provider_openai.go` / `provider_deepseek.go` / `provider_qwen.go` / `provider_vllm.go` | 厂商存储规范化与出站序列化 |
| `client_adapter.go` | `adapterClient` 包装 HTTP 客户端 |
| `factory.go` | 从 config 构造生产 `Client` |

## MessageAdapter 流水线

```
history Message
  → NormalizeAssistantForStorage   （写入 session history）
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

`GET /v1/llm/settings` 会返回 `thinking_control`、`thinking_label` 和
`thinking_secondary_label`，由前端按当前 provider/model 决定右下角显示的开关。
这样不会把某个厂商的参数名称误用到另一家接口；例如 Qwen 的 high/max 会映射为
`thinking_budget`，而 GLM、MiniMax、MiMo 不会发送 `reasoning_effort`。

## 相关文档

- 架构：[`docs/architecture/go-node-internals.md`](../../../docs/architecture/go-node-internals.md)
- History JSONL：[`../history/README.md`](../history/README.md)
