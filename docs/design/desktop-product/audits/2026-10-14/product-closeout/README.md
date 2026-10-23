# 产品线收束与 Desktop 配置分离验收

日期：2026-10-14。方案依据：[产品方向收束](../../../product-direction-simplification-2026-10-14.md)。本轮由 Luna 编码，主线程负责方向、审查、文档与原生窗口验收；未提交或推送，保留工作区既有变更。

## 产品与删除范围

- 一个 Node 对应一个使用者档案，可由同一使用者的多台 Desktop 访问；不新增账号、membership、用户 ACL 或 Desktop 专属 runtime。
- 本机 Desktop 默认管理本机 Node；`tunnel` 只访问用户预先建立的 SSH loopback 转发端点，不创建隧道，不承担 Node 设备认证，不支持 LAN/WAN 直连。
- 移除整个退役 `desktop/tray` Go 模块、`build_dagents_shell.sh`、未调用的 `install_node_service_sysv.sh`，以及对应 go.work、门禁、CI 和品牌生成引用。Windows 仅发布 Tauri Desktop。
- 更新 Windows 安装器门槛与活跃文档：基础 Windows 10、Server 2019+；完整产品推荐 Windows 11。旧 Windows 不继续维护兼容轨道；x86 发布暂保留，Rocky 8 构建环境仍用于当前 Linux 二进制基线。
- Node API 不提供 `/ui` 页面；前端源码暂仍位于 `node/webui/frontend`，由 Desktop 构建复用。此轮没有再次搬迁全部共享组件。
- 保留真实存量数据迁移、审批/取消栅栏、持久化生命周期恢复；未将这些必需路径当作死代码批量删除。

## Runtime 收束

`node/internal/api/agent_session_runtime.go` 统一主 Agent 与 Workgroup 的快照、工具、policy、LLM、prompt 与 digest 构造。Workgroup 继续使用独立 durable session 并禁用个人记忆；正常对话和 Auto 保持主上下文；会话不按 Desktop 连接复制。

重复 Open 同一 binding 复用原 registry，不允许不同 binding 覆盖同一 session；Manager 已释放 runtime 时清理旧映射并恢复持久化历史。idle Close 先持久化并释放 runtime，再删除 binding；之后 Open 可恢复旧历史。存在活动 turn、队列或 HITL 时 Close 拒绝，不丢失当前绑定。未转移所有权的构造结果会关闭，防止资源泄漏。并发重复 Open、未知 live runtime 拒绝、空闲释放后重建、Close/Open 历史保留与构造失败关闭均有回归测试。

## 配置归属

| 范围 | 保存位置 | 本轮行为 |
| --- | --- | --- |
| Desktop 连接模式/endpoint、通知、更新检查与频率 | 当前设备的 Desktop JSON；安装默认按用户保存 | 独立本机设置页，Node 未首配或离线也能访问；连接变更重启 Desktop 生效。 |
| UI 草稿与显示偏好 | 前端本机 localStorage | 不上传 Node，不构造用户账户或服务端私人空间。 |
| Node listen | Node bootstrap `config.yaml` | 不保存 Desktop 设置。 |
| Node 名称、可选称呼、运行参数、LLM、Manage 与工具服务 | Node settings/LLM 数据库 | 设置导航明确“当前 Node”；多个 Desktop 共享这些业务配置。 |
| Agent/Auto/Trigger、工作区与上下文 | Node Agent 与 runtime 数据 | Agent/session 隔离维持原有含义。 |

首配去掉外观步骤，剩称呼/Node 名称与模型两步。`user.preferred_name` 可空，`agent.name` 必填；称呼只是 prompt 提示，不是身份凭证。提交合并既有模型列表，随后重读 Node 状态确认完成。`onboarding.node_profile_completed` 不等同模型可连接。

隧道模式禁止本机 Node 自动启动、恢复、托盘启停与安装器更新动作；Node 原生目录选择器经 Desktop transport 屏蔽，远端工作区按 Node 路径填写或目录 API 浏览。附件仍通过工作区 API 上传并生成 Node 文件引用，不把 Desktop 本机绝对路径当成远端附件。

## 原生窗口实测

使用隔离 fixture `.runtime-test-product-closeout`，本机 Node 18968、离线隧道端点 18969；未改动使用者现有 Node/Manage、真实密钥或 Agent。测试采用无凭据模型配置，不代表真模型联网成功。

| 检查 | 结果与证据 |
| --- | --- |
| 未首配 Node 可打开 Desktop 设置 | 原生窗口通过；不要求先修改 Node 档案。 |
| Desktop 更新频率保存 | UI 从 20 改为 17 分钟，JSON 写入 `1020` 秒；Node `/v1/settings` 无 Desktop 字段。 |
| 新版首配两步、空称呼 | 原生窗口 `1 / 2` → `2 / 2` → 工作台；Node 重读 `node_profile_completed=true`，称呼为空。见 [模型步骤](native/local-first-run-model.jpg)。 |
| 既有模型保留 | 首配后 Node 返回 `retained` 与 `deepseek-chat` 两个 profile，当前 profile 为后者；没有测试 Key 或外部 probe。 |
| Desktop 退出不结束 Node | 退出 GUI 后隔离 Node 18968 仍响应设置读取。 |
| 隧道端点离线 | 窗口显示“等待隧道中的 Node”；18969 无监听，未创建 managed-node owner 文件。见 [离线画面](native/tunnel-offline.jpg)。 |
| 隧道离线仍能改本机设置 | Desktop 设置页正常显示连接模式/endpoint、通知与频率。见 [离线设置](native/tunnel-desktop-settings.jpg)。 |

视觉发现首配进度原硬编码为三步，已修为两步并复验。离线主提示已改为检查转发与远程 Node，技术错误降为小字诊断。隧道菜单原误标“本机”的修正曾误接入首配判断，原生复验发现后纠正调用并增加组件接线断言；最终内嵌 debug exe（UTC 04:04:13）的 [原生菜单复验](native/tunnel-mode-menu.jpg) 已显示“SSH 隧道”。

## 自动检查

- Go 剩余模块测试与 vet 通过；共享 runtime 构造修改后的 API/session 测试通过。
- Rust 30 项测试通过；最终 Desktop UI 49 项、共享 Node Vue 407 项、共享图标 6 项通过。
- Node 与 Desktop Vite 构建、Tauri 内嵌前端 debug build 成功；native 验收使用 `tauri.localhost` 内嵌资源，不依赖 Vite dev server。
- OpenAPI 路由、API/Workgroup contract、品牌资产检查通过。API 接入文档覆盖 150 条 method/path，并逐条验证 handler 与源文件行号。
- Windows `go test -race` 因 cgo.exe 构建失败未运行，不能据普通单测宣称 race 检查通过。

## 尚未关闭的验证范围

1. 完整远程 SSH 加密链路、端点上线后重连、多台实际 Desktop 并行操作与远端文件/浏览器实机验收。本轮端点离线验收不等于这些场景通过。
2. 本轮初始缺少 ISCC；后续已用临时官方编译器完成完整 x64 包编译和隔离安装生命周期实测。最终版本包复测被自动审批阻断，普通用户虚拟机与完整回滚仍未完成，详见 [Oct 14 安装升级记录](../install-upgrade/README.md)；较早 CLI 实测见 [Oct 13](../../2026-10-13/lifecycle-install/README.md)。
3. 没有完整重测所有 Auto、Dreaming、Manage、审批类型与长时间运行场景；本轮优先保护产品边界与配置归属。

启动隧道测试 Node 的组合命令被自动审批审核拒绝，只有 `blocked by policy` 无具体原因；未绕过该拒绝。离线 Desktop 验收正常完成，连通实机测试保留未验收状态。验收结束后通过产品 CLI 关闭隔离 Desktop 并停止其受管 Node，18968/18969 均无监听；保留 fixture 日志与文档截图用于复查。

## 文档恢复说明

退役文档清理期间发生批量替换误覆盖，已停止该批操作。API 契约从此前编码会话的完整记录恢复，并重新验证 150 条路由；Desktop/品牌 README 按当前代码重建，其他受影响文档逐一核对 diff。恢复副本保存在忽略的 fixture recovery 目录。该事件没有用于重置其他既有代码变更。
