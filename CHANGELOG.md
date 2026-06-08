# 变更记录

本文档遵循 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/) 的条目风格；版本号遵循 [语义化版本](https://semver.org/lang/zh-CN/)。

## [0.2.12] - 2026-06-08

**0.x 预览**：修复 **v0.2.11** Windows 发布包组装时 OfficeCLI skills 解压失败。

### 修复

- **`vendor_officecli.sh`**：解 skills  tarball 时一并提取根目录 **`SKILL.md`**（`skills/officecli/SKILL.md` 为 symlink）；复制 skills 时使用 **`cp -RL`** 展开 symlink，避免 Windows CI / zip 分发出现断链。

（Git **tag**：`v0.2.12`。）

## [0.2.11] - 2026-06-08

**0.x 预览**：在 **v0.2.10** 基础上增加 **手动压缩与 context 诊断**、**预编译包 Node 自启动**、**Windows 内置 OfficeCLI**，并改进 Go TUI 滚动体验。

### 新增

- **`POST /v1/sessions/{id}/compress`**：手动触发一次阻塞压缩（忽略 token 阈值）；turn 进行中返回 `409 turn_busy`。
- **Go / Python TUI `/compress`**：调用上述 API；已有压缩任务进行中时展示 `in_progress` 及 `trigger_level` 等字段，不重复执行。
- **`GET /v1/sessions/{id}/context`** 与 **`/context`**：响应/展示 **`system_prompt`**（与 turn 构建逻辑一致）。
- **预编译包 Node 自启动**：Linux **`dagents`**、Windows **`dagents.cmd`** 支持 **`--withnode`**（Client 前探活并后台启动 Node）与 **`node --background`**（日志 `.runtime/logs/node.log`）。
- **Windows 发布包内置 OfficeCLI**：[`scripts/ci/vendor_officecli.sh`](scripts/ci/vendor_officecli.sh) 打入 **`.runtime/scripts/officecli.exe`** 与 **`.runtime/skills/officecli*`**（上游 **[iOfficeAI/OfficeCLI](https://github.com/iOfficeAI/OfficeCLI)**，AGPL-3.0）；Linux tarball **不含** OfficeCLI。
- **`.runtime/scripts` 进 PATH**：Linux **`install.sh`** / systemd 服务、Windows 安装包，便于 Agent 与用户调用扩展 CLI。

### 变更

- **压缩协调器**：silent / blocking / 手动压缩共用 **`compressionTask`** 跟踪；手动压缩遇进行中任务返回 **`in_progress`**。
- **Go 全屏 TUI**：transcript 上滚后新输出不再强制跳底；支持鼠标滚轮；用户发消息时仍 **`syncViewportFollow`** 贴底。

### 修复

- **`contextView` 死锁**：持 session 锁时调用 `SystemPromptForSession` 改为先 unlock 再构建 system prompt。
- **Go TUI 窗口缩放**：resize 时保留 viewport 阅读位置（贴底时仍跟随）。

（Git **tag**：`v0.2.11`。）

## [0.2.10] - 2026-06-07

**0.x 预览**：在 **v0.2.9** 基础上完善 **skills 工具 schema 描述**，引导模型在匹配任务时主动调用 `load_skills`。

### 变更

- **`load_skills` / `unload_skills` / `clear_skills` 工具描述**：集中说明目录仅含元数据、任务匹配 description 时需先加载、整组替换与清空语义、与 `available_skills` 的名称对齐规则；用法不写进 system prompt，由工具 schema 承载。

（Git **tag**：`v0.2.10`。）

## [0.2.9] - 2026-06-07

**0.x 预览**：在 **v0.2.8** 基础上修复 **打断工具后重复 tool 消息** 导致的 LLM 400。

### 修复

- **打断 pending 工具去重**：`InterruptPending` 与 `RepairUnrespondedToolCalls` 共用 `insertMissingToolResponsesAfterAssistant`，按 `tool_call_id` 跳过已有 tool 响应，避免同一 call 写入两条「用户需要补充信息，打断了工具执行。」后 LLM 报 `Messages with role 'tool' must be a response to a preceding message with 'tool_calls'`。
- **idle cancel 清 pending**：`cancelTurn` 在 idle 下 repair 补写 tool 后清除 `pending`，防止后续用户消息再次 interrupt 重复补位。

（Git **tag**：`v0.2.9`。）

## [0.2.8] - 2026-06-08

**0.x 预览**：在 **v0.2.7** 基础上增强 **TUI 展示**、修复 **非法 tool 序列**、统一 **Skill 元数据格式**。

### 新增

- **assistant usage 右对齐**：Textual 与 Go 全屏 TUI 在末行放得下时同行右对齐，否则独占下一行仍右对齐；Go transcript 用 `\x1e` 分隔正文与 usage，展示层上色。
- **消息圆点分色**：用户 / assistant / reasoning / 工具 / 状态等类型使用不同颜色圆点。
- **非法 tool 序列修复**：`RepairUnrespondedToolCalls` 在 LLM 调用前、新 user 消息与 idle cancel 时补写 interrupted tool 结果，避免 `tool_calls` 后缺 tool 消息导致 400。
- **Skill 标准 frontmatter**：`name` + `description`；目录下全部 `SKILL.md` 参与元数据扫描（移除 per-skill `enabled`）。

### 变更

- **多工具审批 UI**：仅当前待审工具展示代码详情；`bash(...)` 标题压平参数换行。
- **工具等待耗时**：与 prefilling 一致按整数秒递增；审批结束后重置执行计时。
- **Esc 取消 HITL**：审批 Esc 提交全拒绝 resume；用户询问 Esc 提交 `cancelled` resume（不再只清本地队列）。
- **内置 `write-skill`**：SKILL.md 改为标准 `name` + `description` 示例。

### 修复

- 用户打断或取消后 history 尾部 assistant+tool_calls 无 tool 响应时，下轮 LLM 请求失败的问题。

（Git **tag**：`v0.2.8`。）

## [0.2.7] - 2026-06-08

**0.x 预览**：在 **v0.2.6** 基础上增加 **`bash_run` 输出压缩**、**tool/usage SSE 展示增强** 与 **双 TUI inline token 用量**。

### 新增

- **`bash_run` 输出压缩（P0）**：L1 ANSI/空行/重复行清洗 + rune 上限截断；配置项 `tools.bash_compress`（`enabled`、`max_output_chars`、`max_output_chars_stderr`）。
- **压缩统计 SSE**：`tool_result` 附加 `output_compress_saved_pct` / `output_compress_raw_runes` / `output_compress_out_runes`（仅 bash 且有节省时）；Go / Textual 工具行展示 `· -N%`。
- **usage 单轮 + turn 累计**：SSE `usage` 含顶层 turn 累计与 `round_*` 单轮字段、`llm_step`；input strip 展示 turn 累计；assistant 块末 inline 展示单轮用量（浅灰）。

### 变更

- **`bash_run` tool 正文精简**：`[BASH_RESULT] exit=N` + `--- STDOUT ---` / `--- STDERR ---`；压缩元数据不再写入 LLM context。
- **Client**：`grep`/`bash` 工具行适配新输出格式；inline usage 仅挂在 assistant 后（不在 thinking/reasoning 后）。

（Git **tag**：`v0.2.7`。）

## [0.2.6] - 2026-06-08

**0.x 预览**：在 **v0.2.5** 基础上拆分 **文件发现** 与 **内容检索** 工具，降低 `search_file` 名实不符带来的模型误用。

### 新增

- **`glob_files`**：在 `directory` 下按 glob（含 `**` 递归）列举匹配路径，分页返回，不读文件内容。
- **`grep_file`**：单文件行内容检索（正则/字面量、上下文、`index_offset` 翻页）；替代原 LLM 可见的 `search_file`。
- **`grep_files`**：目录树内先 `glob_pattern` 筛文件再逐行检索，跨文件命中分页（`hit_offset` / `max_hits` / `max_files`）。
- **共用实现**：`fs_glob.go`、`grep_shared.go`；glob 匹配依赖 `github.com/bmatcuk/doublestar/v4`。

### 变更

- **LLM 工具列表**：注册 `glob_files`、`grep_file`、`grep_files`；`search_file` 仍保留 handler 别名（兼容旧调用），不再出现在 `Definitions()`。
- **子 Agent 默认工具**：`read_file`、`glob_files`、`grep_file`、`bash_run`；父 Agent 可下放列表含 `grep_files`。
- **审批策略**：新只读工具默认 `never`（`policy` + `tool.approval.txt`）。
- **工具描述**：各工具 `description` 仅描述自身能力，不交叉引用其它工具名。

### 移除

- **`search_file.go`**：逻辑并入 `grep_file` / `grep_shared`。

（Git **tag**：`v0.2.6`。）

## [0.2.5] - 2026-06-08

**0.x 预览**：在 **v0.2.4** 基础上收敛 **LLM 厂商适配**、**turn/session 模块边界** 与 **流式中断 history** 行为。

### 新增

- **`llm/messageutil.go`**：`CloneMessage`、`EstimateMessageTokens`（含 `reasoning_content`）、DeepSeek/JSONL payload 辅助。
- **Turn 模块拆分**：`tool_router.go`（工具分流与执行）、`cancel_partial.go`（流式 cancel 部分 assistant 落库）、`history_write.go`（history 写入）。
- **`session/runtime_turn.go`**：`runTurnStep` / `finishTurnIdle` 统一四路 handler 脚手架。
- **包文档**：`llm/`、`store/`、`api/`、`queue/`、`stream/`、`hitl/` 的 `README.md`。

### 变更

- **`MessageAdapter`**：新增 `MarshalChatRequestMessages`；DeepSeek 出站序列化集中在 `provider_deepseek.go`；`openai.go` 不再按厂商名分支。
- **DeepSeek**：带 `tool_calls` 的 assistant 允许空 `reasoning_content`（出站强制键存在）；不再从最近 assistant 继承 reasoning。
- **Token 估算**：压缩触发与 `GET /context` 共用 `llm.EstimateMessageTokens`。
- **JSONL**：`messageToJournalPayload` 委托 `llm.MessageToJournalPayload`。

### 修复

- **流式 cancel**：`persistCancelledStream` 保留已流式输出的 assistant；未响应 `tool_calls` 补 interrupted tool 消息，保证下轮合法序列。
- **子 Agent 结算**：`finishTurnIdle` 在 `applyStepOutcome` 之后调用，修复 `tryCompleteChildIfIdle` 看不到最终 assistant 的时序问题。

### 移除

- 死代码：`hitl/waiter.go`、`session.handleMessage`、`provider` 未使用的 reasoning 继承 helper。

（Git **tag**：`v0.2.5`。）

## [0.2.4] - 2026-06-08

**0.x 预览**：在 **v0.2.3** 基础上增加 **LLM 厂商适配层**、**usage / reasoning token 统计**，并统一 **双 TUI transcript 排版**。

### 新增

- **LLM Provider 适配**（`node/internal/llm/`）：`llm.provider` 选择 `MessageAdapter`（`openai` / `deepseek`）；DeepSeek 自动注入 `thinking.enabled` 与 `reasoning_content` 出站校验；`RequestExtra` 合并进 Chat Completions 请求体。
- **Usage 归一化**：`usage.go` 兼容 OpenAI `cached_tokens` 与 DeepSeek `prompt_cache_hit_tokens`；解析 `completion_tokens_details.reasoning_tokens`、顶层 `reasoning_tokens` 及 `completion_token_details` 别名；SSE `usage` 含 `prompt_cache_hit_rate` 与 `reasoning_tokens`。
- **Turn 内 usage 累加**：工具循环多轮 LLM 调用在单条 user turn 内累加 token 统计后再发布 SSE。

### 变更

- **`reasoning_content` 收敛至 LLM 层**：history 不再做 assistant 规范化；由 provider adapter 在存储与出站前处理。
- **双 TUI transcript**：圆点列统一为 Rich `Table.grid` 布局；prefilling/thinking、assistant↔tool、tool 批次↔下一段、human message 与上下文的空行规则一致化。
- **配置示例**：`packaging/agent-client/config.example.yaml` 补充 `llm.provider` 说明。

### 修复

- **Go / Textual Client**：input strip 展示 cache hit 与 `· think N`（`reasoning_tokens`）；`completion_tokens_details` 嵌套字段兜底解析。
- **Transcript 空白行**：流式 assistant 空 partial 不落行；`tool_result` 后不再重复插入空行；Go REPL/full 跳过空 assistant delta。

（Git **tag**：`v0.2.4`。）

## [0.2.3] - 2026-06-08

**0.x 预览**：在 **v0.2.2** 基础上增加 **Windows 安装包**、**Linux 安装脚本**，并修复 **Windows GBK 环境下 shell 工具输出乱码**；强化子 Agent 提示词与双 TUI 展示。

### 新增

- **Windows 安装包（Release / manual-package）**：Inno Setup 生成 `dagents-local-assistant-windows-amd64-installer-*.exe`；包内 `dagents.cmd` 统一入口（`node` / `chat` / `tui` / `register-center`）。
- **Linux 分发**：tar.gz 根目录含 **`dagents`** 启动脚本与 **`install.sh`**（用户级 `~/.local/share/dagents` 或系统级 `/opt/dagents`，配置 `PATH` / `DAGENTS_HOME`）。
- **Shell 工具输出解码**：`config.yaml` → **`tools.bash_output_encoding`**（如 `gbk` / `utf-8`）；未配置时 Windows **cmd/powershell 默认 gbk**，**bash 默认 utf-8**，解码后以 UTF-8 交给 LLM。
- **子 Agent**：Orchestrator 注入 system prompt builder；`create_temporary_agent` 支持 **`skill_names`** 预加载；子 runtime **`BuildChildSystemPrompt`** 含已加载 skill 正文。

### 变更

- **`.env.example`**：仅保留 Register Center 与 Python CLI 仍读取的项；Node/LLM 主配置在 **`config.yaml`**。
- **双 TUI**：友好格式化 `wait_temporary_agents` 等工具结果；`wait_temporary_agents` 后抑制与工具结果重复的 `temporary_agent_completed` lifecycle 行。
- **打包文档**：`packaging/linux/`、`packaging/windows/` 与 CI assemble 说明更新。

### 修复

- **Release CI（Windows）**：Inno Setup 编译时 Git Bash 将 `/DMyAppVersion=…` 误转为 `D:\…` 导致 `ISCC` 失败；改用 `//D` 前缀。
- **Windows 中文环境**：`bash_run` 捕获 GBK 字节流时 Agent 此前收到乱码；现按配置/平台解码后再写入 `[BASH_RESULT]`。

（Git **tag**：`v0.2.3`。）

## [0.2.2] - 2026-06-07

**0.x 预览**：在 **v0.2.1** 代码基础上对齐 `/health` 与 Client 版本号为 **0.2.2**；**本版重点修复工具审批（HITL）相关缺陷**。

### 修复

- **工具审批（HITL）**：
  - **Go full TUI**：父 Agent 与子临时 Agent 并发出现 `approval_required` 时，审批队列曾按单一槽位去重，导致后到的审批覆盖先到、或用户批准后 `resume` 无法匹配正确 `tool_call_id`。现以 **`ApprovalQueueKey`** 分桶（父：`approval_id`；子：`child_session_id`），同键仅保留最新一条，不同 Agent 的审批可并行排队。
  - **resume 路由**：`SubmitResume` 载荷携带 `approval_id` / `child_session_id`，与 Node pending HITL 对齐，避免审批结果投递到错误会话或静默失败。
  - **Node**：启用临时 Agent 时，父 session 的 `resume` 曾被重复入队，导致队列积压、审批无法继续；现保证单次入队（见 `TestEnqueueResumeParentDoesNotDoubleEnqueue`）。
  - **Textual TUI（`dagents chat`）**：审批队列与用户追问（`ask_user_information`）展示与 Go Client 语义对齐，避免子 Agent 审批与父审批互相顶替。

### 变更

- **临时 Agent**：工具与 SSE 统一为 **temporary agent** 命名；子 runtime 禁止 `load_skills` / `unload_skills` / `clear_skills`；`ActiveAgent` 等内部命名整理。
- **文档**：Python Agent 运行时迁入 `docs/archive/python-agent-runtime/`；新增 [`docs/architecture/go-node-internals.md`](docs/architecture/go-node-internals.md) 与 `node/internal/session`、`turn` 包 README。

（Git **tag**：`v0.2.2`。）

## [0.2.1] - 2026-06-07

中间 tag，**未** bump 代码内 `Version` 常量；变更已包含在 **0.2.2**。主要内容：临时 Agent 重构与文档归档（见上）。

（Git **tag**：`v0.2.1`。）

## [0.2.0] - 2026-05-30

**0.x 预览**：**Go Agent Node + Client** 成为唯一 Agent 运行时；本地助手与终端交互以 Go 栈为主线。Python 保留 **Textual TUI Client**（`dagents chat`）与 **Register Center**，**Python FastAPI Agent API 已从仓库移除**（原 `app/harness/`、`run_agent_api.py`）。

### 新增

#### Go Agent Node（`node/`）

- **HTTP/SSE API**：会话创建/恢复、消息提交、`/resume`、流式事件、`/cancel`、context/skills/child-agents 等（契约见 `docs/architecture/agent-node-api.md`）。
- **Turn 编排**：OpenAI 兼容 LLM、工具循环、审批暂停、`user_information` 询问、上下文压缩（blocking/silent SSE）。
- **工具**：`bash_run`、`read_file` / `write_file` / `search_replace`、`load_skills` / `unload_skills`、trigger 系列、子 Agent 工具（`spawn_child_agent` 等）。
- **Triggers**：`interval`、`fire_at`、日历 **`schedule`**（含 `cmd` 门控）；SQLite/JSON 持久化。
- **Session**：SQLite 消息持久化；多 session 队列与活跃 turn 管理。
- **Policy / Skills**：本地 policy profile；按 session 加载 skill 元数据。
- **Usage 事件**：SSE `usage` 含 prompt/completion、cache hit/miss、`prompt_tokens_details`（供 Client input strip）。

#### Go Client（`client/`）

- **bubbletea 全屏 TUI**（`tui`，默认）：上输出 / 下输入；SSE 断线按 `Last-Event-ID` 重连。
- **行模式 REPL**（`tui --plain` / `TERM=dumb`）：老 SSH / RHEL6 兜底；turn 期间独占 stdin，完整走通 HITL。
- **HITL（full）**：逐工具审批（↑/↓、Space、Enter）；选项式 `ask_user_information`；子 Agent 审批标题区分。
- **展示友好化**：`[tool] ▶ 调用 bash(…)` / `✓ bash 完成`；审批展示参数/原因/风险；input strip `↑prompt ↓completion · hit N`。
- **斜杠命令**：`/context`、`/skill`、`/children`、`/cancel`、`/tools verbose|brief`、`/reasoning on|off` 等。
- **探测与一次性 chat**：`probe`、`chat "消息"`（非交互）。

#### 共用配置（`shared/config/`、`packaging/agent-client/`）

- **YAML 同包配置**：`agent_id`、`listen`、`local.endpoint`、`llm`、`data_dir` 等；Node 与双 Client 共用。
- **本地助手打包**：`dagents-local-assistant-*` tarball/zip（Go Node + Go Client + Textual CLI + `config.example.yaml` + 启动脚本）。

#### Python Textual TUI（`app/cli/`）

- **`dagents chat`**：连 **Go Node**（非 Python API）；RichLog 流式输出、非阻塞 HITL、子 Agent 过滤与状态条。
- **共用 YAML**：`--config` / `DAGENTS_CONFIG` 与 Go Client 对齐；`show session` / `delete session`。
- **Usage strip**：消费 SSE `usage`，显示上下行 token 与 cache hit（与 Go Client 语义一致）。

#### Register Center（`register_center/`）

- **持久化**：可选 JSON 文件存储；TTL  prune。
- **A2A 指标**：relay/broadcast Prometheus 计数（`register_center/metrics.py`）。
- **安全**：共享 token 校验；relay resume 转发。

#### 文档与工程

- **文档重组**：原 `docs/architecture-v2/` 迁入 `docs/architecture/`、`docs/design/`、`docs/future/`、`docs/archive/`。
- **N7 部分**：`go-node-compatibility.md`、Go 静态构建脚本、RHEL6 SysV init、Release CI Go tarball。
- **子 Agent 设计**：`docs/architecture/child-agent-tools.md`。

### 变更

- **运行时主线**：本地助手默认 `go run ./node/cmd/dagents-node` + `dagents chat` 或 `dagents-client tui`；不再提供 Python Agent API 进程。
- **`run_dev_stack.py`**：仅启动 Register Center（原 API + RC 联调入口简化）。
- **`dagents` CLI**：移除 `serve` / `api` 子命令（原后台 `dagents-api`）；保留 `chat`、`show`、`delete`、`register-center`。
- **CI**：PR 跑 Go `node/` / `client/` 单测 + Python CLI/RC 单测；移除 Python OpenAPI 导出步骤；`dagents-cli` 构建改用 Rocky Linux 8。
- **`requirements.txt`**：移除 `openai`、`APScheduler` 等仅 Agent API 栈依赖。
- **Prometheus（Register Center）**：A2A 指标从 `app/observability/` 迁至 `register_center/metrics.py`。

### 移除

- **Python FastAPI Agent 运行时**：`app/harness/`、`app/core/`、`app/context/`、`app/schemas/`、`app/observability/`、`run_agent_api.py`、`export_openapi_schema.py`。
- **Legacy 打包**：`packaging/linux/dagents-backend.*`、旧全栈 `startup_scripts` 中的 `dagents-api` 启动脚本。
- **辅助脚本**：`scripts/query_session_sqlite.py`、`scripts/migrate_runtime_layout.py`、`scripts/ci/export_openapi_for_frontend.py`。
- **Python Node 单测**：`test_agent_service*.py`、`test_api_app.py`、`test_main_agent_*.py` 等约 20+ 文件。
- **Windows 托盘启动器**（`scripts/windows/tray_launcher.py` 等）。

### 修复

- **HITL**：Python/Go Client 与 Node 间 `tool_call_id` 同步；新 `approval_required` 替换 stale 队列项。
- **Go full TUI**：textarea 默认 Focus；placeholder UTF-8 首字节乱码（`> 输入消息...`）。
- **Go plain REPL**：发消息后等待 `done` 再显示 `dagents>`，避免与 HITL 抢 stdin；`done` 正确收尾 assistant 行。
- **Trigger 调度与日志**：fire 路径与 observability 对齐（Go Node）。

### 已知限制（0.2.0）

- **N7 真机验收**：RHEL6 / Windows Server 2012 等待测清单见 `docs/architecture/rhel6-acceptance-checklist.md`。
- **plain REPL**：工具审批仅 y/N 全批/全拒；无 full TUI 的逐项勾选；回合等待期间不可用 `/cancel`。
- **A2A 工具**：随 Python Agent API 移除；Register Center relay/broadcast 仍可用，Agent 侧 A2A 需后续在 Go/Manage 落地。
- **历史文档**：`docs/api-reference.md`、`docs/architecture/python-runtime.md` 描述已删除的 Python API，仅作参考；现行契约见 `docs/architecture/agent-node-api.md`。
- **Web UI（DAgentsUI）**：独立前端仓库 **尚未适配 v0.2.0**，仍基于旧 Python Agent API / OpenAPI；浏览器 Client 暂不可用，请使用终端 TUI。
- **0.x 预览**：破坏性变更在 **1.0** 前仍可能出现。

（Git **tag**：`v0.2.0`。）

---

## [0.1.0] - 2026-05-12

首个对外标记版本（**0.x 预览**）：核心 Agent API、可选 SQLite 会话持久化、SSE 流式事件、Prometheus 指标、Register Center 与配套文档/单测基线。以下 **变更 / 文档 / 仓库维护** 与首版同批交付（尚未单独发补丁版号时，仍以 **`v0.1.0`** 与 Git tag 对齐）。

### 包含（概要）

- FastAPI Agent 服务：会话、消息提交、resume、SSE、取消 turn、会话释放等（详见 `app/harness/api/README.md`）。
- 本地联调脚本（`run_dev_stack.py` 等）；仓库根其它 `run_*.py` 入口仍在演进，交互能力不以变更记录为对外承诺。
- OpenAI 兼容运行时：工具调用、审批流、异步工具结果回灌、上下文压缩相关能力。
- `register_center/`：Agent 登记与发现（内存实现）。
- 配置：`app/config/settings.py`、`.env.example`；可观测：`/metrics`（可关）。
- 单元测试：`tests/` 下 `unittest discover`；可选联网冒烟见 `tests/integration/`。
- `docs/`：对外技术文档除 **`api-reference.md`**、**`prometheus-metrics.md`**、**`architecture-and-flows.md`**、**`agent-input-output.md`**、**`context-compression-and-state.md`** 外，另含 **`agent-turn-loop.md`**、**`a2a-and-register-center.md`**、**`built-in-tools.md`**、**`roadmap.md`** 与 **`cases/`** 案例目录索引等；索引见 **`docs/README.md`**；其余以代码与 **`app/**/README.md`** 为准。

### 变更

- **Prometheus（LLM token）**：`dagents_llm_*` token 指标由 **Gauge `set`（末次快照）** 改为 **Counter `inc`（进程内累计）**，指标名增加 **`_total`** 后缀（例如 **`dagents_llm_prompt_tokens_total`**）；监控面板的 PromQL 与告警需改用新指标名。语义约定：每次流式 **`usage`** 分片表示 **当前 completion 请求** 的消耗；若网关上报账号级终身累计会导致重复累加。
- **系统提示侧车**：运行时从 **`<运行根>/.runtime/prompt_context/`** 读取 **`soul.md` / `user.md` / `custom.md`**；若缺失则由 **`prompt.py`** 创建 **空 UTF-8 文件**（不覆盖已有文件），**不从其它路径拷贝文案**。**`packaging/runtime/prompt_context/`** 提供 **空文件占位**；**`packaging/runtime/`** 另含 **`scripts/`**、**`data/`** 等目录占位，随发布 zip 并入 **`bundle/.runtime/`**。
- **异步工具**：`AsyncToolResultStore.submit_coroutine` 要求非空 **`client_id`**；`OpenAIConversationContext` 新增进程内 **`sse_client_id`**（由带 **`client_id`** 的入站 `MessageEnvelope` 刷新），异步工具终态回灌的 **`MessageEnvelope.client_id`** 与该通道对齐，保证 SSE 可投递至原客户端。

### 文档

- **`docs/context-compression-and-state.md`**：补充 **SQLite 会话记忆**、侧车 Markdown、**`get_system_prompt`** 拼接顺序；索引见 **`docs/README.md`**。
- 新增 **`docs/agent-turn-loop.md`**：讲解 **队列外层 + `run_turn` 单轮内层**、**`_run_turn_and_maybe_execute_tools`**、**`tool_result` 入队** 与审批 / **`async_tool_result`** 分支。
- 新增 **`docs/a2a-and-register-center.md`**：**A2A（`agent_peer`）** 与 **Register Center**（登记、**`broadcast`/`relay`**、配置与审批 **`resume` 直连** 约束）。
- 新增 **`docs/built-in-tools.md`**：**`get_tools()`** 清单、**`@tool` / `tool()`** 装饰逻辑、**docstring → LLM**、**`parameters` 与 `parse_tool_arguments` → `invoke` 管道**、异步工具与 **`client_id`**、**`host_platform` 未注册** 等。
- 新增 **`docs/roadmap.md`**：**路线图**（已实现能力、待办、已知限制；含 **§3.4** CLI / 子 Agent / A2A 与 Register Center 增强 / 压缩 / 内置记忆书等规划方向）。
- 新增 **`docs/cases/`**：落地案例目录（见 **`docs/cases/README.md`**），用于收录各场景实践与效果供参考。
- **`docs/README.md`**：**`cases`** 入口链接格式修正；**`docs/roadmap.md`**：§3.4 **A2A 优化** 条目补全（广播 SSE、跨 NAT 等表述）。

### 仓库维护

- **`.gitignore`**：增加 **`dist/`**、**`build/`**、**`bundle/`**，避免将 PyInstaller 与本地打包中间产物误提交。
- **`tests/test_agent_service.py`**：懒加载 **`OpenAIImplicitReActRuntime`** 时 patch **`app.core.main_agent.runtime_openai.get_openai_client`**，避免新版 **OpenAI** SDK 在 **`LLM_API_KEY` 为空** 的 CI 环境于 **`AsyncOpenAI(...)`** 构造期抛错，导致 **`_session_consume_loop`** 崩掉、生命周期断言失败。

### 已知限制（0.1.0）

- **HTTP API 单测**未在仓库内全覆盖；CI 以 `requirements.txt` 安装后跑默认 `test_*.py`。
- **`test_agent_service.py`** 在缺少完整依赖（如未安装 `openai`）时相关用例会 **skip**，与 CI 全量安装行为不同。
- 破坏性 API 变更在迈向 **1.0** 前仍可能出现；见 [README.md](README.md) 中「版本与兼容性」。

> **注**：0.1.0 中的 Python FastAPI Agent 运行时已在 **0.2.0** 移除；上文保留作该版本历史记录。

（Git **tag**：`v0.1.0`；请在对应托管平台上创建 **Releases** 并与该 tag 对齐。）
