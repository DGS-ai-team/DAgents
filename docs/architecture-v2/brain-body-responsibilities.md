# Brain 与 Body 职责清单

本文定义 Agent 运行过程中各类信息和能力应归属 Brain、Body 还是 Control Plane。目标是避免把“思考状态”“执行环境”“用户偏好”“宿主机资源”混在一起。

## 1. 核心原则

```text
Brain 负责理解、决策、记忆和生成。
Body 负责执行、资源、环境和本地硬约束。
Control Plane 负责身份、路由、策略、审批和状态协调。
```

进一步拆分：

- **Brain Runtime** 是 Backend 中共享的推理引擎。
- **Brain Profile** 是某个 Agent 的思考配置。
- **Body** 是该 Agent 绑定的执行环境。
- **Session Context** 是某次用户或 A2A 对话的运行上下文。
- **Body Context** 是 Body 暴露给 Brain 的环境上下文。

## 2. 总览表

| 项目 | 归属 | 说明 |
|------|------|------|
| LLM API key / model provider | Brain / Backend config | 不属于 Body，Body 不调用 LLM |
| 模型选择、temperature、tool choice 策略 | Brain Profile | 影响推理行为 |
| Agent persona / `soul.md` | Brain Profile | 定义 Agent 的长期角色与行为风格 |
| 用户偏好 / `user.md` | Brain Profile 或 User Profile | 跨 Body 生效的偏好归 Brain；只在某宿主机有效的偏好归 Body Context |
| 临时专项指令 / `custom.md` | 视来源而定 | Backend 侧 custom 属 Brain Profile；宿主机侧 custom 属 Body Context |
| system prompt 拼接 | Brain | Brain 负责将 Brain Profile、Session Context、Body Manifest 组装为最终 prompt |
| conversation history | Session Context / Brain | 对话历史由 Brain 使用并由 Backend 持久化 |
| pending tool calls | Session Context / Brain | 属于推理流程状态，不属于 Body |
| context compression | Brain | 压缩策略和摘要生成在 Backend 执行 |
| skills 元数据 | Brain 或 Body Manifest | 纯提示词 skills 属 Brain；依赖宿主机工具/文件/权限的 skills 属 Body |
| skills 执行依赖 | Body | 例如 kubectl、mysql、PowerShell、脚本路径 |
| tools schema | Brain / Control Plane | 模型可见的工具定义和参数 schema 在 Backend 管理 |
| tool implementation | Body 或 Backend local executor | 实际执行在 Agent 绑定的 Body 上 |
| shell / file system | Body | Body 提供执行环境和文件边界 |
| `fs_root` | Body | Body 本地硬约束 |
| allowed commands / deny rules | Body hard constraints + Policy | Body 必须能本地拒绝危险操作；Backend 负责全局策略 |
| host snapshot | Body Context | OS、arch、hostname、用户、可用工具链等 |
| environment variables | Body | Body 控制执行时注入哪些环境变量 |
| resource handles | Body | kubeconfig、DB socket、本地证书、工作目录等 |
| resource summary | Body Manifest → Brain | Body 上报资源摘要供 Brain 理解，不暴露敏感原文 |
| approval policy | Control Plane | Backend 决定 auto / require_approval / deny |
| audit log | Control Plane | 策略判定、审批、执行结果统一审计 |
| A2A routing | Brain + Control Plane | Brain 决定是否调用 peer；Control Plane 负责会话和消息投递 |
| Register Center metadata | Control Plane / RC | RC 保存 Agent discovery 元数据，不执行工具 |
| SSE client state | Control Plane | client_id、连接、推送路由不属于 Brain 或 Body |

## 3. Prompt 类文件归属

### 3.1 Brain-owned prompt

以下内容归 Brain Profile 或 User Profile：

- Agent persona，例如 `soul.md`。
- 跨所有 Body 生效的用户偏好，例如 `user.md`。
- 模型行为规则、回复风格、工具选择原则。
- 与宿主机无关的长期业务知识。

这些内容由 Backend 读取、缓存，并参与最终 system prompt 组装。

### 3.2 Body-owned prompt/context

以下内容归 Body Context：

- 只对某宿主机有效的 `custom.md`。
- 宿主机本地目录说明、脚本说明、操作注意事项。
- 本地工具链说明，例如 “本机 kubectl 默认指向 prod 集群”。
- Body 暴露的 resource manifest。

Body-owned 内容不应直接覆盖 Brain 的最高优先级规则。它应作为“环境上下文”进入 prompt，由 Brain 解释并受策略约束。

### 3.3 Session-owned prompt/context

以下内容归 Session Context：

- 当前对话历史。
- 用户本轮目标。
- A2A peer session 信息。
- pending tool calls 与 resume 信息。
- 本 session 内的临时补充约束。

Session Context 生命周期跟随 session，不应写入 Body Profile。

## 4. Skills 归属

Skills 应按“是否依赖执行环境”区分：

| Skill 类型 | 归属 | 示例 |
|------------|------|------|
| 纯推理 / 纯提示词 skill | Brain Profile | 写作风格、代码审查流程、总结模板 |
| 依赖特定工具链 skill | Body Manifest | kubectl 排障、mysql 查询、本地脚本调用 |
| 依赖特定文件或密钥 skill | Body Manifest + Policy | 读取某业务配置、调用内部 CLI |
| A2A 协作 skill | Brain + Control Plane | agent discover、agent send message |

Brain 负责决定是否使用 skill；Body 负责提供 skill 所需的可执行资源；Control Plane 负责策略审批和审计。

## 5. Context 归属

| Context 类型 | 归属 | 生命周期 |
|--------------|------|----------|
| Session conversation context | Brain / Session | 随 session 创建和过期 |
| Compression summary | Brain / Session | 随 session 持久化 |
| Body environment context | Body | 随 Body 注册、心跳或环境扫描更新 |
| User preference context | Brain Profile / User Profile | 跨 session，可长期保留 |
| A2A peer context | Brain / Session | 短 TTL，调用结束清理 |
| Audit context | Control Plane | 按审计保留策略保存 |

重要区分：

```text
Session Context 记录“这次对话发生了什么”。
Body Context 描述“这个执行环境能做什么、有什么限制”。
Brain Profile 定义“这个 Agent 如何思考和表达”。
```

## 6. Resources 归属

Resource 是 Body 能访问或代表的真实世界能力。

| Resource | Body 负责 | Brain 可见 |
|----------|-----------|------------|
| 文件系统 | 路径、权限、读写、沙箱 | 文件摘要、目录说明、允许访问范围 |
| kubeconfig | 实际凭据和调用 | 集群名称、namespace、允许操作摘要 |
| 数据库 | socket、账号、网络可达性 | 数据库用途、允许查询范围 |
| 本地脚本 | 路径、参数、执行权限 | 脚本说明、输入输出契约 |
| 密钥/证书 | 安全保存和最小暴露 | 默认不可见，只能看到能力摘要 |

Brain 不应直接持有敏感 resource 原文。Brain 只应看到经过 Body Manifest 脱敏后的摘要，并通过工具调用请求 Body 执行。

## 7. Environment 归属

Environment 是 Body 的一部分，但可以有摘要进入 Brain：

- Body 持有：真实 OS、用户、PATH、工作目录、环境变量、网络可达性。
- Brain 可见：脱敏后的 host_info、工具清单、resource manifest、限制说明。
- Control Plane 持有：Body online/offline、proxy_connection_id、并发、队列、心跳。

环境变量默认不进入 prompt。只有明确 allowlist 的变量可以作为 Body Context 摘要暴露给 Brain。

## 8. 配置归属

| 配置 | 归属 |
|------|------|
| LLM endpoint、model、API key | Backend / Brain Runtime |
| prompt_profile、context_policy | Brain Profile |
| discovery_group、schedulable | Agent Instance / Control Plane |
| body.kind、body_id | Body Binding |
| fs_root、allowed_commands、env allowlist | Body |
| approval rules、deny rules、risk profile | Control Plane Policy |
| Redis/shared state、RC URL | Control Plane |
| TUI 主题、用户入口偏好 | Client Plane |

## 9. Prompt 组装建议顺序

最终 system prompt 由 Brain 组装。建议顺序：

1. Backend 静态最高优先级规则。
2. Brain Profile：persona / `soul.md`。
3. User Profile：跨 Body 用户偏好 / `user.md`。
4. Agent capability summary。
5. Body Manifest：body.kind、host_info、resources、tools、environment 摘要和限制。
6. 已加载 skills 的说明。
7. Session Context：当前 session_id、用户目标、A2A peer 信息。
8. Body-owned `custom.md` 或本 session 临时约束。

Body Manifest 和 Body-owned `custom.md` 不能覆盖 Backend 静态最高优先级规则、安全策略或审批要求。

## 10. 决策规则

当不确定某项归属时，按以下规则判断：

1. **是否需要 LLM 理解或长期影响表达？** 是 → Brain Profile。
2. **是否只在某次对话中有效？** 是 → Session Context。
3. **是否依赖某台宿主机、文件、工具、网络或权限？** 是 → Body。
4. **是否涉及允许/拒绝/审批/审计？** 是 → Control Plane Policy。
5. **是否是用户界面偏好？** 是 → Client Plane，除非会影响 Agent 推理。

## 11. Phase 1 最小落地清单

Phase 1 不需要一次实现完整配置系统，但文档和数据模型应按以下最小集对齐：

- `AgentRecord` 包含 `brain_profile` 与 `body`。
- `body.kind` 支持 `backend_local` 和 `proxy_hosted`。
- `body_id` 出现在 ProxyConnection、Session、Execution 记录中。
- Prompt 组装能接收 Body Manifest 摘要。
- 策略输入包含 `agent_id`、`body_id`、`body.kind`、tool 和 params。
- 审计记录包含 `agent_id`、`body_id`、execution 与 policy decision。
