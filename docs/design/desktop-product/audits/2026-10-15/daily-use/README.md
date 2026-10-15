# Desktop 日常使用回归（2026-10-15）

本轮以个人 Node 与独立 Desktop 的产品边界为准。隔离 Node 位于 `127.0.0.1:18974`，使用当前源码新构建；原生 Tauri 使用此前同一源码的开发构建。真实 LLM 为已有的 MiMo profiles，未使用 Mock。凭据只在 gitignored 测试目录中复用，没有写入报告或提交。

## 原生窗口实测

- 正常 Agent 从 Desktop 发送指令，真实模型调用 `read_file`，界面展示 `input.txt` 路径并经点击批准；随后 `write_file` 写入 `result.txt`，再次批准后完成。磁盘结果为 `17 * 19 = 323`，回复为 `DAILY_REAL_OK_323`。
- 在输入栏把 `mimo-v2.5-pro` 切到 `mimo-v2.5`，按钮名称正确更新。Node Agent snapshot 的模型变为 `mimo-v2.5`，Node 默认模型仍为 `mimo-v2.5-pro`。下一回合回复 `MODEL_SWITCH_OK323`，保留同一会话上下文。
- 原生文件选择器上传 `upload-note.txt`，工作区文件内容为 `UPLOAD_ACCEPTANCE_20261015`；发送后消息中显示引用文件卡片。同名 `input.txt` 被拒绝，没有覆盖原文件。
- 长回复请求发送后，点击“停止本轮”。Node 状态成为 `cancelled / cancelled_by_user`、队列为空、无待执行工具。
- 停止后通过 Node API 发送下一条人类消息，真实模型回复 `RESUME_AFTER_CANCEL_OK`，恢复为 completed / idle。Auto 同一会话中通过 API 接收用户插话并回复 `AUTO_HUMAN_OK`；这两条是 API 验证，不冒充原生窗口重开后的操作。
- 从菜单“退出 Desktop（Node 将继续运行）”退出后，Desktop 进程确实结束。Node 仍响应 API，Auto 在 `07:28:19Z` 发起、`07:28:23Z` 完成新的 trigger 回合，晚于 Desktop 退出时间 `07:27:36Z`。职责仅要求回复 `AUTO_BACKGROUND_OK_323`，无工具操作。生命周期摘要见 [auto-lifecycle.json](auto-lifecycle.json)。测试结束关闭了该 Auto 的默认唤醒（wake=0，revision=2）。

截图：[文件读写完成与模型切换](01-real-file-tools-model-switch.jpg)、[切换后上下文回答](02-switched-model-context.jpg)。截图中的测试 Agent、路径及 marker 均为合成验收数据。

## 本轮修复

上传重名文件时，Node 的 `workspace_file_exists` 被错误地映射成“文件已被其他操作修改”。现已区分重名上传与版本冲突：重名提示“同名文件已存在，请换名后重新上传”，版本冲突保留重新载入提示。成功上传仅清除当前 Agent 仍匹配的旧上传错误，不吞掉其他错误。相关回归已通过。

## 自动化验证

- Node 全模块 `go test -count=1 ./...`、shared/config、shared/logfiles、shared/update、Client 测试及各模块 `go vet` 通过；包括 Runtime 隔离、重复 Open、Close/Open 历史、busy close 拒绝、Auto 与普通 Agent 的工具区分以及进程重启持久化。
- Node 前端初验 407/407，合入最新 `dev` 后为 410/410（75 个测试文件）；共享图标 6/6；Desktop UI 修复并合并后 51/51；Desktop Vite 构建通过。
- Rust `cargo test` 30/30 与 `cargo check` 通过。额外启用隔离 Node 的 `live_local_node_terminal_open_input_detach_resume_close_smoke`：1/1 通过，实际覆盖 PTY 输入、detach、resume、输出回放、close 与 session 移除。默认 Rust 测试中的其他 opt-in live smoke 并未自动执行。
- API contracts、OpenAPI 路由同步、Manage Console build/lint、Ruff、Pyright 通过。按锁定依赖创建的独立 Python 3.13 环境完整 unittest 197/197。默认 Anaconda 环境缺 FastAPI 的首次导入失败不作为产品失败；隔离依赖环境的重跑结果为最终结果。
- PR 首轮 CI 发现的 Node 未使用变量与 Manage Docker 共享图标复制遗漏已修正。依赖审计修复后，三套前端 `npm audit --audit-level=high` 均为 0 vulnerabilities，许可证策略覆盖 219 个 package/version pairs；Node 410/410 和 Desktop 51/51 再次通过。Docker 镜像构建由远端 CI 复验，本机没有 Docker CLI。
- 第二轮远端 CI 已通过 Manage Docker 构建、Linux Rust、API contracts、Desktop UI、安装器与 bundle 契约、Linux Go 全模块测试及 vet；之后检出的 Client 两条静态检查文案已改为小写并通过同版本 staticcheck。Python `multidict` 升至最小修复版 6.9.1，主依赖及 Browser 既有例外清单下的审计均通过，197 项测试重跑通过。多附件先失败后成功会保留本批失败提示，针对性 workspace 测试 9/9 通过。最终远端状态以 PR 当前 head 为准。

隔离 Auto 已禁用唤醒；Desktop 退出后，最后通过指定 fixture JSON 的 owned-node CLI 停止空闲测试 Node，`18974` 不再监听。首次清理命令未指定该 JSON，因默认 endpoint 的 readiness 无法确认而安全拒绝；没有停止其他 Node。未删除测试数据或凭据文件。

## 验证限制

本次未执行旧发行版迁移、真实 Linux/SSH 隧道、多日连续运行以及所有外部服务验收。Dreaming 与 Workgroup 的既有真实回归见 [10 月 12 日记录](../../2026-10-12/auto-workgroup/README.md)，不冒充本日重测。配置归属、监督生命周期与安装流程的前轮证据见 [10 月 14 日收束](../../2026-10-14/product-closeout/README.md) 和 [安装升级记录](../../2026-10-14/install-upgrade/README.md)。Windows cgo 未能编译 race 测试，race 结果以 Linux CI 为准。

自动审批拒绝了重开更新后 Desktop 的命令，只返回 `blocked by policy`，未提供进一步原因。因此上传提示修复通过单元回归与构建，但未在更新后的原生窗口再次操作验证；本轮也未再次手工重启 Node，恢复能力由自动化进程重启测试及前轮实测佐证。前轮最终版本安装循环复跑与打包 Browser 服务启动同样被自动审批拒绝，仍属于发布验收缺口；本次开发合并不等同于批准发版。
