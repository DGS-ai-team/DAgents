# DAgents

多 Agent / 工具调用场景下的 **Agent 运行时**仓库：**Go Agent Node** 为本地助手主线；Python 保留 **Textual TUI Client** 与 **Register Center**。

**协议**：[MIT License](LICENSE)。**Web 前端**独立仓库：[DAgentsUI](https://github.com/DGS-ai-team/DAgentsUI)。

**标记版本：`v0.2.0`（2026-05-30）** · [变更记录](CHANGELOG.md)

---

## 本地助手（Go Node）

单进程 Agent：**LLM turn**、工具、SQLite 会话、审批、skills、compression、triggers（含日历 schedule）。

```bash
cp packaging/agent-client/config.example.yaml packaging/agent-client/config.yaml
# 编辑 config.yaml（默认 llm.mock=true，无需 API Key）

# 终端 1 — Node
go run ./node/cmd/dagents-node -config packaging/agent-client/config.yaml

# 终端 2 — Client（二选一）
python -m app.cli.main chat --config packaging/agent-client/config.yaml   # Textual TUI（首选）
go run ./client/cmd/dagents-client -config packaging/agent-client/config.yaml tui  # Go TUI（full / --plain）
```

| 目录 | 说明 |
|------|------|
| [`node/`](node/README.md) | Agent Node（Go） |
| [`client/`](client/README.md) | Go TUI Client |
| [`shared/config/`](shared/config/README.md) | 共用 YAML 配置 |
| [`app/cli/`](app/cli/README.md) | Python Textual TUI（连 Go Node） |

文档：[docs/architecture/local-assistant.md](docs/architecture/local-assistant.md)、[docs/architecture/agent-node-api.md](docs/architecture/agent-node-api.md)。

---

## Register Center（Python）

Agent 登记、广播与中继（A2A 控制面）；**非** Agent 运行时。

```bash
python -m venv .venv && source .venv/bin/activate
pip install -r requirements.txt
python run_register_center.py    # 默认 :8010
python run_dev_stack.py          # 同上（开发便捷入口）
```

文档：[register_center/README.md](register_center/README.md)。

---

## 项目结构

```text
DAgents/
├── node/                 # Go Agent Node
├── client/               # Go TUI Client
├── shared/config/        # Node + Client 共用 YAML
├── app/cli/              # Python Textual TUI
├── register_center/      # Register Center（Python FastAPI）
├── packaging/            # 配置示例、离线安装、Go 本地助手包
├── docs/                 # 技术文档（见 docs/README.md）
├── tests/                # Python CLI / Register Center 单测
├── run_register_center.py
└── requirements.txt
```

---

## 测试

```bash
# Go（node/client 变更时 CI 同样执行）
go test ./node/... ./client/... ./shared/config/...

# Python CLI / Register Center
pip install -r requirements.txt
python -m unittest discover -s tests -p "test_*.py" -v
```

详见 [tests/README.md](tests/README.md)。

---

## 版本与兼容性

- **Python**：3.11+ 可运行；**CI 验证 3.13**。见 [docs/os-compatibility.md](docs/os-compatibility.md)。
- **Go**：见 `go.work`（`node`、`client`、`shared/config`）。

---

## 预编译包（本地助手）

资产见 GitHub Releases **`dagents-local-assistant-*`**：`dagents-node` + Textual/Go Client + `config.example.yaml`。

---

## 问题反馈与安全

- **Issues**：注明栈（Go Node / Python CLI）、版本、OS、复现步骤。
- **安全漏洞**：[SECURITY.md](SECURITY.md)。

---

## 文档入口

| 文档 | 说明 |
|------|------|
| [docs/README.md](docs/README.md) | 技术文档索引 |
| [docs/architecture/overview.md](docs/architecture/overview.md) | 架构总览 |
| [docs/architecture/agent-node-api.md](docs/architecture/agent-node-api.md) | Go Node HTTP/SSE API |
| [register_center/README.md](register_center/README.md) | Register Center API |
| [CHANGELOG.md](CHANGELOG.md) | 版本变更 |

---

## License

[MIT License](LICENSE)
