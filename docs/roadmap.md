# DAgents 路线图

> 机器可读版本事实以根目录 [`VERSION`](../VERSION) 为准，变更说明以 [`CHANGELOG.md`](../CHANGELOG.md) 为准。本页只保留当前产品方向、已完成基线和下一阶段优先级。

## 1. 产品定位

DAgents 面向个人使用者：Windows Desktop 提供交互，默认启动或连接同机 Node；Node 持有数据、会话并持续执行任务。Desktop 不保存远程 Node 档案，也不支持 SSH 隧道连接。多个 Desktop 可在各自机器上连接各自的本机 Node；当前不引入多人账号、私人上下文或租户隔离。Manage 是可选的跨节点工作组控制面。

长期任务及反馈闭环已在 v0.11.0 建立，并在 v1.0.0 中随独立 Desktop 形成完整的个人使用产品。

## 2. 当前发布基线（v1.0.1）

v1.0.0 是首个稳定版，覆盖独立 Desktop、同机 Node 与可选 Manage 协作。

- Windows x64 DAgents Desktop 是个人使用入口，Node 只提供 API 并管理会话、工具、记忆、模型和自主任务；Desktop 与 Node 需要在同一台机器。
- 对话工作台包含资源页签、文档预览和引用、终端、浏览器会话查看、截图、工具审批及上下文管理。
- Auto Agent 支持自主激活、Todo、Dreaming、经验和文件系统手册；长期任务与普通聊天共享 Agent 身份和会话。
- 可选 Manage 提供 Node 注册、工作组协作、反馈处理及产品包分发；单机使用不依赖 Manage。
- Linux 发布 Node、Client 和 Browser 服务包，不发布 Linux Desktop；Windows 暂停 x86 完整包。

## 3. 后续方向

v1.0.0 发布后优先维护稳定性、升级迁移和日常使用体验。尚无下一版本号或发布日期；新需求先根据真实使用反馈排优先级。

### 稳定性与维护

- 持续验证 Windows x64 安装、升级与 v0.11.0 数据迁移；Linux 服务包按独立发布矩阵维护。
- 根据实际用户反馈完善模型/MCP 配置、资源预览、终端与 Manage 协作，并保持已发布 API 契约稳定。
- 维护依赖审计、供应链校验、版本元数据和发布资产清单；修复问题使用 hotfix 流程。

### 后续探索

- 根据用户需求评估远程 Desktop→Node 连接、macOS/Linux Desktop、多用户隔离和更丰富的事件触发。
- 浏览器人工接管、可交互嵌入式页签、工作流画布与插件生态单独立项，不作为当前产品承诺。

远程主机仍可通过本机 Node 的 SSH/远程终端工具操作。安全或数据一致性缺陷始终按最高优先级处理。

## 4. 明确不作为主线

- 通用可视化工作流画布；
- Node-to-Node 直连派活或 Manage 反向访问 Node；
- 通过 Workgroup 隐式创建受限 Agent；
- 把完整 Timeline/raw tool output 广播给所有成员；
- 在没有真实隔离边界前宣传产品级沙箱。

## 5. 参与与变更规则

- 新功能先写当前架构/契约，再实现和补真实验证。
- 日期化分析、一次性实验和版本验收清单放入 [`docs/archive/`](./archive/README.md)。
- 跨组件设计从 [`docs/design/README.md`](./design/README.md) 进入；用户操作从 [`docs/user/README.md`](./user/README.md) 进入。
- 发现实现与文档不一致时，以代码和测试为准，提交修正文档的 PR。

**最后更新**：2026-10-22。
