# 快速开始

使用 Windows 发布包时，安装完成后从开始菜单打开 **DAgents Desktop**。安装器会准备本机 Node，Desktop 会启动或连接它；无需先执行下面的源码启动命令。桌面快捷方式、登录自启动默认不勾选，可在安装任务中选择。Desktop 需要 WebView2 Evergreen Runtime；缺失时须先安装该运行时。配置入口为顶栏 **DAgents → 通用设置**。

## 1. 启动本机 Node

```bash
cp packaging/agent-client/config.example.yaml packaging/agent-client/config.yaml
go run ./node/cmd/dagents-node -config packaging/agent-client/config.yaml
```

Node 默认监听 `http://127.0.0.1:18765`，提供 API。Node 不再提供浏览器 UI；Windows 安装包附带 DAgents Desktop，Desktop 会启动或连接本机 Node 并提供交互界面。Linux Node 包只提供服务端与客户端工具，不包含桌面运行时。

从源码启动 Windows Desktop 时，在仓库根目录 PowerShell 中设置 Node 的安装根目录和配置路径，再启动 Tauri 开发版：

```powershell
$env:DAGENTS_HOME = (Resolve-Path .).Path
$env:DAGENTS_CONFIG = Join-Path $env:DAGENTS_HOME 'packaging/agent-client/config.yaml'
npm install --prefix desktop/tray-tauri
npm run tauri:dev --prefix desktop/tray-tauri
```

在 DAgents Desktop 中配置模型、LLM 密钥和 Agent。没有 API Key 时可启用 Mock LLM 做结构联调；真实对话需要配置对应 Provider 的密钥。

## 2. 启动 Manage

```bash
npm run build --prefix manage/console/frontend
python3 run_manage.py
```

打开 `http://127.0.0.1:8020/console/`。Manage 是可选控制面：本机对话不依赖它，工作组、Node 注册和集中管理才需要它。Windows 也可以使用 `py -3` 替代 `python3`。

## 3. 首次检查

```text
Node：GET http://127.0.0.1:18765/health
Manage：GET http://127.0.0.1:8020/health
```

确认 Desktop 能连接本机 Node 并完成一次普通对话，再配置 Manage/Workgroup。Node 必须主动连接 Manage；网络策略只需允许 Node 到 Manage 的出站连接。

## 4. 工具与数据边界

- 文件、Shell、MCP、浏览器和终端能力由 Agent 的工具组、policy 和运行时配置共同决定；模型并不能绕过 policy。
- 文件工具使用工作区相对路径；不要把生产目录直接作为测试工作区。
- 危险工具可能进入 HITL 审批；审批通过、拒绝、取消和超时都会形成可追踪状态。
- `.runtime/`、SQLite、skills、policy 和终端状态属于本地运行数据，不要提交到 Git。

详细工具、配置、SSE 和 Workgroup Schema 见 [reference/README.md](../reference/README.md)。
