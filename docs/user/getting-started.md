# 快速开始

## 1. 只运行本机 Agent Node

```bash
cp packaging/agent-client/config.example.yaml packaging/agent-client/config.yaml
npm run build --prefix node/webui/frontend
go run ./node/cmd/dagents-node -config packaging/agent-client/config.yaml
```

Windows PowerShell 中将最后一条命令写为：

```powershell
go run ./node/cmd/dagents-node -config packaging/agent-client/config.yaml
```

打开 `http://127.0.0.1:18765/ui/`，完成首次设置并新建 Agent。模型配置在“设置 → 连接”中保存；没有密钥时可以使用 Mock LLM 做结构联调。

## 2. 启动 Manage

```bash
npm run build --prefix manage/console/frontend
python3 run_manage.py
```

打开 `http://127.0.0.1:8020/console/`。Manage 是可选控制面：本机对话不依赖它，工作组、Node 注册和集中管理才需要它。

## 3. 首次检查

```text
Node：GET http://127.0.0.1:18765/health
Manage：GET http://127.0.0.1:8020/health
```

先确认 Node Web UI 能完成一次普通对话，再配置 Manage/Workgroup。Node 必须主动连接 Manage；网络策略只需允许 Node 到 Manage 的出站连接。

## 4. 工具与数据边界

- 文件、Shell、MCP、浏览器和终端能力由 Agent 的工具组、policy 和运行时配置共同决定；模型并不能绕过 policy。
- 文件工具使用工作区相对路径；不要把生产目录直接作为测试工作区。
- 危险工具可能进入 HITL 审批；审批通过、拒绝、取消和超时都会形成可追踪状态。
- `.runtime/`、SQLite、skills、policy 和终端状态属于本地运行数据，不要提交到 Git。

详细工具、配置、SSE 和 Workgroup Schema 见 [reference/README.md](../reference/README.md)。
