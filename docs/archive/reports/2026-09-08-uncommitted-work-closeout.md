# 本地未提交工作收口审查

日期：2026-09-08。基线为 `b9cf7b3b`，分支 `codex/cleanup-legacy-logic`。用户授权核对全部未提交变更，闭环后提交并保持工作区干净。主负责人审查与验收，Luna 完成代码及测试修改。

## 变更归组

| 范围 | 内容与闭环依据 |
| --- | --- |
| Node 执行基础 | Trigger 目标归属、原子投递、持久化恢复、Session/Turn 用量及生命周期；Go 全量与并发回归 |
| 单 Node Auto | Goal、运行记录、预算、审批、自主类型与对话调整；API/存储测试及之前真实模型验收 |
| Manage 与反馈 | 默认鉴权、身份绑定、反馈幂等存储与同步、管理员处理；完整 Python 回归 |
| 两端前端 | 任务设置、历史路由、状态恢复、反馈与响应式布局；Vitest、lint、build及实际浏览器验收 |
| 契约与依赖 | OpenAPI 路由、测试依赖锁、npm 许可证元数据兼容；契约、许可检查与依赖审计 |
| 文档 | 用户指南与索引、未发布变更记录、验收边界及普通/Auto 产品方向评估 |

## 本轮补齐

- OpenAPI 缺少 autonomy GET/PUT，Goal 状态路由的内联 YAML 未被同步检查识别；补齐准确路由与请求契约。
- 移除前端迁移后遗留的未使用导入/变量，恢复 CI lint。
- 许可证检查支持对象形式 `license.type`，识别新增依赖的 MIT-0；未知/缺失仍拒绝，新增 4 项回归。
- 用户指南补充终态任务无法原地开启新周期的当前限制；产品评估明确把周期复用、持续观察语义、汇总与集中治理列为后续建议。
- 关闭旧 `POST /v1/goals` 新建入口，统一通过 Auto 设置创建并绑定；普通/缺省类型与切回普通后的任务不得绕过类型边界恢复或唤醒。旧历史仍可读、暂停与停止，旧集成测试迁移到新的真实创建契约。
- 真实模型验收测试使用自动清理临时目录，避免失败路径遗留复制的模型凭据。

## 验证

- Node 全量 `go test ./node/...` 与 `go vet ./node/...`；session/turn/queue 的 race 回归通过。
- 最终额外复跑 API 类型转换、旧接口拒绝、同 Agent 并发仲裁与不同 Agent 并行的 race 用例；全部通过。迁移期间暴露的旧 POST 测试和不可达代码均修复后再检查，没有把中间失败算作完成。
- shared/config、shared/logfiles、shared/update、client、desktop/tray 的 Go 测试与 vet 通过。
- Python `unittest discover -s tests -p "test_*.py" -v`：185 项通过；Manage/tests ruff 与 Manage pyright 通过。
- Node Vitest：59 文件、323 项通过；两端 ESLint 与构建通过。
- 两端 npm audit 无已知漏洞；许可证检查 235 个包版本通过，独立许可证测试 4 项通过。
- API / Workgroup 契约与 OpenAPI 同步检查通过；修改 Go 文件格式检查通过。
- 对新增/修改文件检查常见高置信凭据模式，未发现命中；没有将运行数据库、日志、二进制或临时模型凭据纳入提交。

以上是本地 Windows 检查，不等同远程 Linux CI、打包、发布或长期线上验证。构建保留既有体积及第三方 eval 警告。没有重新执行已有的付费真实模型场景，真实执行证据见 [前端验收](../../design/2026-09-08-frontend-live-acceptance.md)，视觉覆盖见 [视觉验收](../../design/2026-09-08-ui-visual-audit.md)。

## 交付边界

本次提交为未发布的单 Node 受控 MVP。Auto 新周期、长时无人值守可靠性、跨 Node Goal 调度、Manage 集中 Auto 治理尚未交付。正式进程未因提交而自动重启，当前运行状态不能作为新后端已部署的证明。正式 Manage 实际业务数据仍未做视觉检查，已有视觉验证使用隔离数据。
