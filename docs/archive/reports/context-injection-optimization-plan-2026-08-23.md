# DAgents 上下文与 ContextInjection 优化方案

## 1. 目标与边界

本方案把模型请求中的信息分成稳定前缀、对话历史和按需注入的运行时上下文，解决以下问题：

- 运行环境、长期记忆、侧车 prompt 变化时，不再无条件重建完整 system prompt；
- 不引入每轮追加的动态尾部，避免消息连续性割裂；
- 原始 human message、assistant message、tool result 保持可回放、可审计；
- 同一个 Turn 内的多个 Step 继续使用同一套 context snapshot；
- context 变化时明确记录 digest、来源和生效边界；
- 压缩、hydrate、UI 展示与模型请求不再共享一份可变消息数组。

本次范围聚焦 Node 的模型请求链路，不改变 UI transcript 的展示语义，也不把动态 ContextInjection 持久化到普通对话 history。

## 2. 设计原则

### 2.1 三层数据模型

```text
Durable history / lifecycle events
    原始用户消息、assistant、tool call/result、turn/step 事实
    不覆盖、不因为模型上下文裁剪而丢失

Model surface
    当前请求可见的 history 副本
    可进行 tool-result 规范化、ContextInjection、compression replacement

UI projection
    由权威 history/lifecycle 事件派生
    不反向决定模型上下文
```

### 2.2 稳定前缀与动态上下文分离

稳定 system prompt 保留：

- 最高优先级规则；
- 任务执行契约；
- 工具结果协议；
- 工作区目录和路径规则；
- 外部工具目录；
- 当前 Turn 冻结的 skills 元数据、选择规则和工具使用约束。

ContextInjection 承载：

- 主机运行环境；
- Agent/session 身份；
- soul、用户称呼、长期记忆、custom prompt；
- 后续可扩展的 MCP/终端/工作区动态事实。

工具 schema 和 skills 清单的变化仍然允许导致 system/tools 前缀缓存失效，这是语义正确性的必要成本；Skill 正文在激活时作为独立 durable context message 进入 history，不再重写 system prompt 前缀；ContextInjection 只减少不必要的 system prompt 前缀变化。

### 2.3 ContextInjection 不是动态尾部

ContextInjection 的约束：

1. 同一个 snapshot 内只生成一次；
2. context 没有变化时不重复追加；
3. 不写入 `runtime.messages`；
4. 以当前 Turn 的根 user message 为锚点，插入其前方；
5. 不放在完整对话最后形成持续增长的 volatile tail；
6. context 变化只在下一个模型 Step 的 snapshot 边界生效；
7. 通过 `ContextInjectionDigest` 记录实际发送的内容版本。

这样模型看到的结构接近：

```text
stable system prompt
历史对话...
runtime context injection
当前 Turn 的 human / trigger / external input
当前 Turn 的 assistant/tool steps
```

实际插入点是当前 Turn 根 user 消息之前，因此不会破坏当前 Turn 后续 assistant/tool 消息的连续性。

## 3. 数据结构

### 3.1 ContextInjection

```go
type ContextInjection struct {
    Name     string `json:"name"`
    Source   string `json:"source"`
    Content  string `json:"content"`
    Position string `json:"position"`
}
```

当前只生成一条 `runtime_context` 注入；保留 source/name 是为了后续拆分环境、MCP、终端、workspace facts 时不需要重做 snapshot 协议。

### 3.2 ModelContextSnapshot

在现有 system/tools digest 之外增加：

- `ContextInjections`：当前模型 Step 实际使用的注入内容；
- `ContextInjectionDigest`：注入内容的稳定 digest。

snapshot clone、生命周期事件和诊断输出必须深拷贝并保留这些字段。

### 3.3 模型请求副本

构造请求时：

```text
history              原始 runtime history 的副本
    ↓
runLLMBeforeCall     hook 可以修改请求 history
    ↓
applyContextInjection
    ↓
ExpandMessagesForLLM
    ↓
PrepareToolResultMessagesForModel
    ↓
LLM request
```

ContextInjection 不进入 `history`，因此不会出现在 hydrate、普通 transcript、压缩源消息或再次持久化的消息中。

## 4. 生效边界

### 4.1 新 human Turn

新 Turn 清除旧 snapshot，重新构造：

- system prompt；
- tools；
- ContextInjection；
- snapshot digest。

### 4.2 同一 Turn 内

同一 snapshot 用于所有 Step。以下变化不立即影响正在执行的 Step：

- prompt sidecar 更新；
- 长期记忆更新；
- skills 控制面变化（Skill 正文通过独立 context message 在下一个模型 Step 生效）；
- MCP/终端配置变化。

如果变化明确要求当前 Turn 重新构造上下文，则通过现有 `RequestModelContextRefresh` 在下一个模型 Step 建立新 snapshot；当前正在流式生成的请求不修改。

### 4.3 压缩

压缩只处理持久化 history；侧车摘要请求可以临时追加摘要指令，但该临时消息不写回 session history，也不属于正常运行时动态尾部。

压缩完成后，下一模型 Step 重新读取当前 ContextInjection snapshot；必要时通过新的 context digest 诊断前后差异。

## 5. 缓存策略

- 稳定 system prompt、工具 schema、skills 清单保持确定性排序和冻结；
- Skill 正文只在激活时新增独立上下文消息，正文更新不改写旧 history；
- 动态环境不再拼进 system prompt，减少环境字段变更导致的前缀失效；
- ContextInjection 改变时，只从其插入位置开始影响消息缓存；
- tools/skills schema 或正文变化造成的 system/tools cache miss 接受为必要成本；
- 不向 prompt 写入 runtime revision、时间戳、诊断字段；这些字段只进入 lifecycle/SSE/metrics。

## 6. 失败与并发约束

- ContextInjection 构造失败时不阻塞请求，使用空注入并保留 system prompt；
- snapshot 建立后注入内容不可变；
- 异步压缩应用前继续使用指纹校验，防止覆盖新 history；
- hydrate 继续只返回持久化 history 和权威 lifecycle 状态；
- cancel、clear-context、Turn 完成时清理 snapshot；
- ContextInjection 不参与 UI 的消息数量和 transcript 序号。

## 7. 验证计划

### 单元测试

- system prompt 不再包含 Agent/session、运行环境和 prompt context 正文；
- ContextInjection 包含这些信息且稳定排序；
- context injection 插入当前根 user 前，不插入消息末尾；
- 多个 Step 请求注入内容和 digest 一致；
- prompt context 变化后新 Turn 得到新 digest；
- hook 修改的 history 仍保留，ContextInjection 只存在请求副本；
- 原始 history 不包含 ContextInjection；
- snapshot clone 深拷贝 injection 内容；
- child runtime 仍包含必要的运行环境和 purpose；
- Skill 正文不进入 system prompt，激活后以 `name=skill` 独立消息进入模型请求；
- unload/clear 后旧 Skill 正文仍可审计，但不会进入 outbound request；
- 压缩侧车临时摘要指令不写回主 history。

### 集成回归

- 连续 human → assistant/tool → tool continuation；
- skills load/unload 引起的下一 Step context refresh；
- remember / long-term reload；
- cancel、HITL resume、异步回调；
- Node 重启后 lifecycle snapshot 恢复；
- `/hydrate` transcript 不出现 ContextInjection；
- 真实 UI 连续对话中 system/tools/context digest 不跳变。

## 8. 分阶段落地

1. 增加 ContextInjection 数据结构、digest、snapshot clone 和请求副本注入器；
2. 将运行环境与 prompt context 从 system prompt 移到 ContextInjection；
3. 接入 orchestrator 的 snapshot 创建与复用；
4. 增加 child/runtime/lifecycle 诊断字段；
5. 补充专项测试并运行 Go、Web UI 和回归测试；
6. 根据测试结果修复兼容问题，再评估是否把 MCP、终端、workspace facts 接入同一机制。
