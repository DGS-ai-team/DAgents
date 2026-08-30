# Contributing to DAgents

感谢参与 DAgents。项目是一个多模块、本地优先的 Agent 控制台；提交变更前请先确认变更属于哪个运行边界，并保持 Node、Manage、Console 和桌面 Shell 的契约一致。

## 开发流程

1. 从 `dev` 创建分支，分支名使用 `codex/<topic>`、`feature/<topic>` 或 `fix/<topic>`。
2. 涉及跨组件行为时，先更新 `docs/architecture/` 或 `docs/design/` 中的契约，再实现代码。
3. 保持提交小而聚焦；推荐使用 `feat:`, `fix:`, `refactor:`, `test:`, `docs:` 和 `chore:` 前缀。
4. 提交前运行统一验证：

   ```powershell
   powershell -ExecutionPolicy Bypass -File scripts/verify.ps1
   ```

   Linux/macOS 使用 `bash scripts/verify.sh`。

## 模块边界

- `node/`：本地 Agent Node、Turn/Step 生命周期、工具执行和对外 HTTP/SSE。
- `manage/`：可选的工作组控制面、注册表、审计、发布元数据和 Console API。
- `shared/`：只能放跨 Go 模块确实共享的稳定类型或算法，不放业务编排。
- `client/`、`desktop/`：客户端和桌面适配层，不把 UI 状态反向写入 Node 事实源。
- `node/webui/frontend/`、`manage/console/frontend/`：展示与事件投影；不要在组件中复制后端业务规则。

新增跨层字段必须同步代码模型、OpenAPI/JSON Schema、兼容说明和回归测试。工作组 WS 使用 Node→Manage 出站连接；不要新增 Manage→Node 反向 HTTP 依赖。

## 代码与数据约定

- 版本唯一来源是根目录 `VERSION`；发布元数据由 `scripts/release/` 校验。
- Go 使用 `gofmt`，Rust 使用 `cargo fmt`，Python 使用 Ruff；行尾和缩进遵循 `.editorconfig`。
- 所有跨层异步操作必须携带稳定身份（至少 `agent_id`、`session_id`、`turn_id` 或 `workgroup_id`/`member_id`），不得只靠时间窗或展示文本推断状态。
- 取消、重试、重复投递、断线恢复和迟到回调必须定义幂等语义；可能已发生副作用但结果未知时使用 `indeterminate`。
- 不提交 `node_modules/`、构建出的 Go embed 静态目录、运行时数据库、密钥或真实用户数据。

## Pull Request 要求

PR 描述必须说明：行为变化、兼容性/迁移影响、风险与回滚方式、验证命令，以及未覆盖的真实环境（例如真实 LLM、WSS、远程终端）。

涉及 API、WS、工具、数据库或 Workgroup 的 PR 至少包含对应契约检查和回归测试。不要通过放宽检查、忽略失败或压制构建警告来掩盖行为变化；如需暂时例外，请在 PR 中写出负责人和移除日期。

安全漏洞不要在公开 Issue 或 PR 中披露，按 [SECURITY.md](SECURITY.md) 的私密渠道报告。

### 协作与提交边界

- 一个 PR 只解决一个可审查的行为或架构主题；跨 Node、Manage、UI 和
  Desktop 的变更必须在描述中说明依赖关系，必要时拆分提交。
- 不要直接修改另一个子系统的事实源来修补展示问题。先确认事件、快照、
  投影和兼容字段的归属，再补适配层测试。
- 新增异步状态必须说明 owner、终态、取消、重试、重复投递、重启恢复和
  资源上限；所有失败路径都应有稳定错误码或可断言的错误类型。
- 测试等待优先使用 channel、条件轮询和 deadline；避免用固定
  `sleep` 掩盖竞态。
- 贡献者应遵守 [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md)。
