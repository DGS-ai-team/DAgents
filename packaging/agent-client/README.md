# Agent + Client 同包示例

Node 与 Client 共用 YAML 配置；**仓库只提交 `*.example.yaml`**，本地副本见根目录 `.gitignore`。

## 文件

| 文件 | 说明 |
|------|------|
| `config.example.yaml` | **引导配置**（可提交）：仅 `listen` / `local` |

其余 Node 设置在首次启动写入 `./.runtime/node_settings.db`（路径固定）；LLM 档案在 `llm_configs.db`；Agent 策略/侧车在 `agents.db`。Triggers 语法见 [`node/internal/triggers/README.md`](../../node/internal/triggers/README.md)；工具组见 [handbook/04-能力与策略.md](../../docs/handbook/04-能力与策略.md)。

## 本地配置

```bash
# 在仓库根目录执行
cp packaging/agent-client/config.example.yaml packaging/agent-client/config.yaml
```

按需改 `listen` / `local`。LLM、压缩、skills/triggers、hooks 等请在 **Web UI「设置」** 修改（写入 SQLite）。旧版胖 YAML 首次启动会自动迁入 `node_settings.db` 并瘦身引导文件。

`config.yaml` **不会被 Git 跟踪**。

### Agent 身份与 Manage 注册

身份在 **`node_settings.db`（Web UI）** 中配置；Node 注册 Manage 时自动组装 `card` JSON 上报。字段含义：

| 字段 | 说明 |
|------|------|
| `name` | Manage 展示名；空则回退 `agent_id` |
| `description` | Console 简介 |
| `role` | 展示用角色标签（历史 `compliance` / `ops` 等） |
| `capabilities` | 可选；非空时覆盖 config 默认 capabilities（shell/filesystem 等） |
| `metadata` | 可选自定义键，原样进入 Manage `card.metadata` |

Manage 注册时合并 **`manage.registration`**（`base_url`、`team`、心跳参数）与运行时上报的 `tools` 列表。跨机器协作请用 **工作组（Workgroup）**（旧 A2A inbox / `agent_invoke` 已拆除）。

## 注册为系统服务（Node）

解压包内（非开发仓库）：

```bash
sudo ./scripts/linux/install_node_service.sh install --config config.yaml
```

**Linux**（开发仓库，需 root）：

```bash
sudo scripts/linux/install_node_service.sh install --config packaging/agent-client/config.yaml --build
scripts/linux/install_node_service.sh status
sudo scripts/linux/install_node_service.sh uninstall
```

**Windows**（管理员 CMD）：

```bat
scripts\windows\install_node_service.cmd install packaging\agent-client\config.yaml --build
scripts\windows\install_node_service.cmd status
scripts\windows\install_node_service.cmd uninstall
```

Linux 注册 **systemd**（`dagents-node.service`）；Windows 为 **SYSTEM 开机计划任务**。详见 [`scripts/README.md`](../../scripts/README.md)。

## 快速联调（一份配置，三个进程共用）

```bash
cp packaging/agent-client/config.example.yaml packaging/agent-client/config.yaml
go run ./node/cmd/dagents-node -config packaging/agent-client/config.yaml
go run ./client/cmd/dagents-client -config packaging/agent-client/config.yaml
```

Manage 与工作组见 [manage/README.md](../../manage/README.md)、[docs/user/workgroups.md](../../docs/user/workgroups.md)。
