# 配置与数据收束验收

日期：2026-10-17。Luna 负责引用保护、迁移测试和依赖修复；主线程负责代码复核、真实 Windows Tauri 操作、文档和验证范围确认。

## 原生配置往返

使用用户此前手动启动的隔离 Node `127.0.0.1:18977` 与真实 `dagents-shell-idle.exe` 窗口。没有重启使用者的 Node，没有复制或探测真实模型凭据。测试 HTTP 服务只监听本机 18978/18979，使用合成字符串校验收到的凭据；记录仅保存匹配与否，不记录请求头明文。

实测二进制 SHA-256：

- Node：`B7CB07B8CE229FE4F3C148F49AFA58AD3FC7563FBF3F6AFF4AF45623BB5D6DA2`。
- Desktop：`992D17E227B3AFE2DDB7B669E8521B3100C29C9C41C74E845BBE4A4DB1865320`。

这套二进制包含此前密钥与 MCP 往返修复、新上下文功能；不包含本轮后来增加的模型 ID 引用保护和 Bash 等锁取消修复。后续修复单独通过最新源码回归，不冒充原生已重启复验。

| 操作 | 观察与对照 |
| --- | --- |
| 已有 LLM 配置，Key 留空，点击测试并获取模型 | 本地 `/v1/models` 收到与初始保存一致的合成 Bearer；界面显示获取 1 个模型，配置 ID 保持 `key-preservation-acceptance`。 |
| 确认弹窗并保存整页 | Node GET 仍为同一配置 ID，`has_api_key=true`；不返回密钥。 |
| 重新打开配置，再次用空 Key 探测 | 再次成功，测试服务确认 Node 解密并发送原凭据，排除仅靠 UI 缓存“已配置”标记的假通过。 |
| MCP JSON 配置合成请求头并保存 | 服务完成 initialize 与 tools/list，实际收到预期测试请求头；API/编辑器回显遮罩标记。 |
| 切换工具，再点高级 JSON 保存并刷新 | 两个测试工具均保持启用，JSON 中 `enabled_tools` 与工具开关一致；刷新请求继续携带原合成请求头，没有把遮罩标记当作实际凭据。 |
| 无效 MCP JSON 保存 | 界面保留 `{` 草稿并显示解析错误；Node 原配置、两个启用工具和凭据保持不变。手动重新加载可恢复已存配置。 |

LLM 测试与 MCP 服务均为真实 HTTP 传输的合成对端，不能代表外部 Provider/MCP 供应商或真实网络故障已经验收。LLM 明确清空/替换、歧义改名和失败重开的分支由已有自动测试覆盖，本轮没有逐一进行原生操作。

原生截图已作为文档资产保存到 `docs/images/desktop/`，仅含合成会话和遮罩后的配置。测试完成恢复 Node 原有单一 Mock 配置、移除合成 MCP 服务并停止两个测试 HTTP 服务；Node 与 Desktop 保持运行，返回原测试对话。旧聊天与单条上下文分隔线仍正常显示。

## 模型配置引用保护

`PATCH /v1/settings` 对仍被活动普通/Auto Agent 引用的配置改名或删除，先返回中文 `409 llm_profile_in_use`，不改变 Node 设置、密钥或 Agent 快照。没有 Agent 恢复 API，已软归档 Agent 不永久占用引用。相同 ID 的普通编辑、未引用配置的改名继续允许。

新建/修改 Agent 的模型绑定还会检查目标 ID 存在，和配置变更使用同一读写锁；Prompt Context/MCP 的快照写回以及旧 LLM settings 写入也纳入锁，避免校验后被并发写入覆盖。子 Agent 继承父配置；Manage Supervisor 使用独立配置命名空间。回归覆盖普通与 Auto、归档后解除、无副作用拒绝、正常编辑/未引用改名和无效绑定拒绝。

## 数据恢复与依赖

v0.11.0 的代表性 SQLite/JSON 文件已用脱敏旧 schema 构建，并通过当前 Store 打开、配对解密、Agent/Auto/workspace 与历史恢复、未决 Trigger 安全暂停验证。详细操作与限制见 [迁移指南](../../../../user/migrate-from-v0.11.md)；这不是旧版安装器迁移已通过。

三套生产前端 `npm audit --audit-level=moderate` 为 0 漏洞；Python 主依赖审计通过，Browser 审计沿用仓库已有明确例外且未新增忽略项。原型与 Rust 告警的目标范围见 [依赖记录](../../../../development/dependency-readiness-2026-10-17.md)。

本轮共享前端 85 文件、470 项和共享图标 6 项通过；Desktop UI 67 项、Vite build、两套生产前端 lint、API contracts 通过。增加跨架构更新回归后，Python 3.13 全量 unittest 202 项通过，Ruff/Pyright 与 219 个 npm 包版本的许可检查通过。系统默认 `python` 指向缺少 FastAPI 的 Anaconda 环境；完整测试明确使用 `py -3.13`，环境导入失败不计为产品回归通过。Go 工具链、终端协议与最终回归单独见 [运行时回归](runtime-regression.md)，候选输入及打包见 [候选记录](release-candidate.md)。
