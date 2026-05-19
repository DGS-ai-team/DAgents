# 安全治理与分阶段验收

本文档记录 Agent 设计优化落地后的安全边界、默认开关、审计字段与阶段性验收标准。目标是在 0.x 预览版保持兼容，同时让高风险能力具备可回滚路径。

## 高风险入口

- `bash_run` 与后台 `ShellJob`：启动前仍走工具审批策略；超时只改变等待方式，不改变安全边界。后台任务应优先通过 `bash_job_status`、`bash_job_tail`、`bash_job_cancel` 查询和控制。
- 文件写入工具：继续依赖工具审批与精确参数 schema；模型上下文不应包含未知参数。
- A2A 入站与 relay：入站 `AgentPeerEnvelope` 由 API 层识别并标记 `source=a2a:<caller_agent_id>`；未知或非法信封按普通文本处理，不提升权限。
- skills：`AGENT_SKILLS_ENABLED=false` 时不暴露 skills 工具、不注入 skills prompt；`clear_skills` 与 `unload_skills` 只影响会话态，不删除磁盘文件。
- 工具原始输出落盘：长输出或脱敏命中时写入 `.runtime/tool_outputs/`；模型与 SSE 展示使用脱敏/截断版本，避免敏感信息直接进入上下文。
- prompt 侧车与长期记忆：`.runtime/prompt_context/*.md` 与 `.runtime/memory/long_term.md` 只作为低优先级上下文，不应覆盖系统级安全规则。

## 审计与观测

- session 维度：`dagents_session_context_messages_count`、`dagents_session_queue_depth`、`dagents_session_queue_priority_depth`。
- 工具维度：`dagents_tool_executions_total`、`dagents_tool_approval_required_total`。
- 压缩维度：`dagents_summary_compression_total`，并记录触发层级与成功/失败状态。
- A2A 维度：`AgentPeerEnvelope.trace_id`、`message_id`、`caller.agent_id`、`target` 与 Register Center `expires_at_unix`。
- 长任务维度：`ShellJob.job_id`、`async_job_id`、`status`、`exit_code`、`started_at_unix_ms`、`finished_at_unix_ms`。

## 分阶段验收

### 第一阶段：测试与协议基线

- 新增编排器与 API 路由测试，缺少可选依赖时必须 skip 而不是失败。
- 工具 schema 能表达 `list[str]`，并默认禁止未知参数。
- `AgentPeerEnvelope` 可在 API 入站被识别，普通文本行为保持兼容。

### 第二阶段：工具结果与长任务

- `bash_run` 同步超时时返回 `status=RUNNING`、`job_id`、`async_job_id`，且不重启、不杀死原进程。
- `bash_job_status`、`bash_job_tail`、`bash_job_cancel` 可查询/读取/取消后台 job。
- 工具结果进入模型上下文前经过脱敏与截断；原文仅以 `raw_ref` 引用。

### 第三阶段：上下文与 prompt

- 阻塞压缩失败必须发可恢复错误事件并继续使用原上下文。
- 静默压缩应用前校验消息指纹，旧结果不得覆盖新上下文。
- `long_term.md` 长期记忆只在文件存在且非空时注入。

### 第四阶段：A2A 与生产化

- Register Center 记录带 TTL，查询时自动剔除过期 Agent。
- 异步工具具备 `async_tool_status` 与 `async_tool_cancel`。
- 后续如开启 relay/resume 自动审批，必须先增加鉴权、来源校验和审计日志，再改变默认行为。

## 回滚方式

- 若长任务后台化出现问题，可将命令 `timeout_seconds` 调大或临时禁用相关工具策略，只保留同步短命令。
- 若工具结果落盘影响部署，可清理 `.runtime/tool_outputs/`；模型上下文仍保留脱敏摘要。
- 若 A2A 入站解析引发兼容问题，可让调用方发送普通文本；非法信封不会被提升权限。
