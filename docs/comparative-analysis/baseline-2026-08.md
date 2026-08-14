# DAgents、DeepSeek Harness 与 OpenAI Codex 基线对比报告

> 检查日期：2026-08-14
>
> 说明：外部项目状态会快速变化；后续分析应重新检查官方仓库和文档。

## 1. 一句话定位

| 项目 | 定位 |
|---|---|
| DAgents | 面向企业落地的本地优先 Agent Runtime、控制面和工作组协作平台 |
| DeepSeek Harness | 可编程、可插拔的通用 Agent Harness |
| OpenAI Codex | 面向软件工程任务的完整 Coding Agent |

可以概括为：DeepSeek Harness 强在可组合性，Codex 强在代码执行环境，DAgents 强在企业部署、治理和跨机器协作。

## 2. 技术与架构对比

| 方面 | DAgents | DeepSeek Harness | Codex |
|---|---|---|---|
| 核心语言 | Go；Manage 使用 Python；Web UI 使用 Vue | TypeScript/JavaScript、Node.js、Web UI | Rust 为主 |
| 主要入口 | Agent Node 内嵌 Web UI；可选 Manage Console | `dsh web`，默认 `127.0.0.1:3080` | CLI/TUI、App Server、IDE/Desktop 集成 |
| Agent 运行位置 | 目标机器上的 Go Agent Node | Node.js 进程 | 本地或远程 exec/app server |
| 控制面 | Python Manage、Registry、Workgroup、Console | Profile/Bundle/Patch 和插件上下文 | App Server、Remote/Exec Server |
| 持久化 | SQLite、Agent/Session/事件和 Manage 数据 | Append-only SessionEvent 和 Harness home | Thread/history/rollout、SQLite 等 |
| 执行模型 | Node 内部工具执行，按 Agent policy 控制 | 可替换 shell/fs/subprocess/sandbox provider | 独立 exec-server、PTY、filesystem RPC、sandbox |
| 跨机器 | Manage 出站连接和 Workgroup | 需要自行组合远程 provider | SSH 隧道、远程 exec-server、Remote Relay |
| 发布形态 | Go 二进制，前端资源嵌入 | npm/Node.js | 原生预编译二进制 |
| 当前成熟度 | 企业预览版本 | Developer Preview，可能有破坏性变更 | 产品化程度较高 |

## 3. 值得借鉴的设计

### 3.1 DeepSeek Harness

Harness 的核心设计是“一切皆插件”：模型适配器、工具注册、Session Log、Agent Loop、文件系统和 sandbox 都能作为插件挂载。其能力通过 Service Definition、Provider 和 Consumer 分离，适合在不修改核心 loop 的情况下替换执行后端。

对 DAgents 的主要启发：

- 将 shell、filesystem、PTY、sandbox 抽象为 provider；
- 将 Agent Loop 的关键阶段做成稳定事件扩展点；
- 把会话事件作为恢复、审计、UI 和上下文投影的共同事实源；
- 引入类似 profile/bundle/patch 的配置组合机制，但不必整体引入 Cordis。

参考：[Harness README](https://github.com/deepseek-ai/deepseek-harness/blob/master/README.zh.md)、[Harness 架构文档](https://raw.githubusercontent.com/deepseek-ai/deepseek-harness/master/docs/architecture.md)、[Harness package.json](https://raw.githubusercontent.com/deepseek-ai/deepseek-harness/master/package.json)。

### 3.2 OpenAI Codex

Codex 的重点是把软件工程执行环境做可靠。`exec-server` 提供 JSON-RPC/WebSocket 协议，负责 PTY、进程生命周期、输入输出、文件系统 RPC 和断线清理。SSH 模式通过系统 OpenSSH、rsync 和本地端口转发连接远程服务。

对 DAgents 的主要启发：

- 增加独立的 Exec Server 协议；
- 将进程启动、读写、终止和 PTY 状态形成稳定契约；
- 抽象本地、SSH、容器和 Workgroup 执行后端；
- 加强执行前 policy、sandbox 和审计字段；
- 让远程 Linux 用户身份直接参与权限隔离；
- 以原生二进制和低运行时依赖作为部署目标。

参考：[Codex README](https://github.com/openai/codex)、[Codex Cargo workspace](https://raw.githubusercontent.com/openai/codex/main/codex-rs/Cargo.toml)、[Codex exec-server 文档](https://raw.githubusercontent.com/openai/codex/main/codex-rs/exec-server/README.md)、[Codex SSH 执行脚本](https://raw.githubusercontent.com/openai/codex/main/scripts/start-codex-exec.sh)。

## 4. DAgents 当前优势

- Agent Node 和 Manage 控制面分离，适合企业内网和目标机执行；
- Node 默认出站连接 Manage，降低网络暴露面；
- Workgroup 统一编排跨机器协作，避免 Node-to-Node 直接暴露；
- Agent 级 policy、HITL、resume、SSE cursor 和子 Agent 已有较明确契约；
- Go 原生二进制适合老旧基础设施和不希望依赖 Node.js 的部署环境；
- 具备 Registry、Release、升级、审计和 Console 方向，覆盖产品运维面。

## 5. DAgents 主要缺口

### 高优先级

1. 执行后端仍与 Node 内部工具实现耦合，缺少统一的 Local/SSH/Container Provider 接口。
2. 当前主要依赖工作目录和 policy 控制，缺少独立 sandbox/执行进程层。
3. PTY、远程进程生命周期、文件同步和断线清理尚未形成通用协议。
4. Durable session event、运行时事件和 SSE 展示事件需要进一步明确分层。
5. 插件的版本、依赖、生命周期、隔离和回滚机制仍需加强。

### 中优先级

1. 多用户远程连接和 Linux 用户隔离需要正式的 SSH Provider 设计。
2. 工具审计应统一记录 agent、session、tool call、risk level、policy decision 和 executor。
3. 需要统一本地执行、Workgroup 执行和未来容器执行的错误、取消、超时和重试语义。
4. 需要为老 Linux、Windows 和无头环境建立明确的能力降级矩阵。

## 6. 推荐演进路线

### Phase 1：执行 Provider 抽象

建议形成以下边界：

```text
Tool
  -> Policy Engine
  -> Execution Provider
       |- LocalProvider
       |- SSHProvider
       |- ContainerProvider
       `- WorkgroupProvider
```

候选接口包括：

```go
type ShellProvider interface {
    Start(ctx context.Context, req StartRequest) (Process, error)
}

type WorkspaceProvider interface {
    ReadFile(ctx context.Context, path string) ([]byte, error)
    WriteFile(ctx context.Context, path string, data []byte) error
}
```

接口应先稳定语义，再选择具体传输方式。

### Phase 2：Exec Server

参考 Codex，定义最小 JSON-RPC/WebSocket 协议：

```text
initialize
process/start
process/read
process/write
process/terminate
fs/read
fs/write
fs/list
```

本地 provider 和 SSH provider 共用协议，避免未来出现两套工具执行逻辑。

### Phase 3：会话事件统一化

将事件分成：

- Durable Events：用户消息、助手消息、tool call/result、HITL、turn 完成；
- Runtime Events：token delta、队列变化、PTY 输出、心跳、重试。

恢复、审计和上下文投影只依赖 Durable Events；SSE/UI 可以消费两类事件。

### Phase 4：SSH Provider 与用户隔离

支持独立的 SSH 登录用户、工作区、连接和远程 server 实例。私钥只交给系统 SSH/Agent，不写入 Manage 数据库。

### Phase 5：sandbox 与能力降级

按环境提供：

- Linux 新内核：Landlock、bubblewrap 或 namespace；
- 老 Linux：Linux 用户权限、工作目录、命令策略和审批；
- Windows：Job Object、受限 token 和独立工作目录；
- 容器环境：Docker/Podman Provider。

## 7. 适合向上游贡献的方向

### DeepSeek Harness

- SSH execution provider；
- 老 Linux 兼容 provider；
- 企业级 HITL/审计插件；
- SessionEvent 导出器；
- Workgroup/A2A provider；
- DeepSeek/OpenAI-compatible 模型适配器；
- 多租户 profile/bundle 示例。

### OpenAI Codex

- Windows 远程生命周期管理；
- SSH 连接发现、ProxyJump 和企业代理支持；
- exec-server 协议和远程 PTY 测试；
- 断线恢复、远程进程清理测试；
- 老 Linux 和低权限环境的 sandbox fallback；
- 多用户 `CODEX_HOME` 隔离文档；
- 中文部署和企业内网使用文档。

## 8. 后续定期分析模板

每次更新建议按以下结构记录：

1. 检查日期和版本/commit；
2. 三个项目的新增功能；
3. 架构、协议和依赖变化；
4. 对 DAgents 的直接影响；
5. DAgents 新增缺口或已有缺口的变化；
6. 建议进入 DAgents roadmap 的事项；
7. 适合提交给上游的独立 issue/PR；
8. 验证方式、风险和下一次复查时间。

## 9. 结论

DAgents 不应简单复制 Codex 或 Harness。推荐保留 Node + Manage + Workgroup 的企业架构，吸收 Harness 的 provider/plugin 抽象，并引入 Codex 风格的 Exec Server、PTY、SSH 和 sandbox 执行协议。这样既能提升本项目质量，也能形成对两个上游项目有价值的独立适配器、测试、文档和协议贡献。
