# 运维与发布

## 安装与服务

发布包位于 `packaging/agent-client/`；Windows 与 Linux 安装脚本、桌面 Shell 和离线安装说明分别见：

- [agent-client packaging](../../packaging/agent-client/README.md)
- [offline install](../../packaging/OFFLINE_INSTALL.md)
- [Manage packaging](../../packaging/manage/README.md)

Node 默认监听 `127.0.0.1:18765`。Desktop 固定连接同机的 `127.0.0.1:18765`，不支持远程 Node 档案或 SSH 隧道连接；Node 与 Browser sidecar 之间固定使用 `127.0.0.1:18766`。Node bootstrap YAML 仍可配置监听地址，供 Node API 的独立服务部署使用；这不会改变 Desktop 的固定本机连接目标。Node HTTP API 没有通用设备鉴权，不要将它暴露到不可信网络。

下一版 Windows 完整安装包仅提供 x64，暂停 x86。历史 x86 安装仍按自身平台检查更新，不会自动切换到 x64；迁移到 x64 需确认操作系统支持并采用完整备份、重新安装和数据恢复流程，不能在 32 位系统上运行 x64 安装器。

## 升级与兼容性

升级前先完整备份 `.runtime/` 和所有自定义 Agent workspace。升级程序不会主动删除旧目录，但工作区解析规则和私有状态布局已经收敛，不能把“目录仍存在”理解为“旧路径仍会继续被使用”。

完整备份还应包括安装目录的 `config.yaml`、`.runtime/llm_configs.db` 配套的密钥材料和 Desktop 本机偏好。停止 Node 后再复制 SQLite 数据，或使用 SQLite 一致性备份；不要在运行中只复制一个 `.db` 文件而遗漏 WAL。保留原目录和原版程序，恢复时成套还原，不能假设较旧程序可读取新版已写入的数据库。

当前无安装协议 marker 的旧目录会被新安装器安全拒绝覆盖。这不表示旧数据已迁移成功；步骤见 [v0.11.0 迁移指南](migrate-from-v0.11.md)，未完成的环境验收见[发布收束清单](../release-readiness.md)。不应手工伪造 marker 绕过检查。

### 当前数据归属

| 数据 | 当前路径 | 说明 |
|---|---|---|
| Agent 元数据与配置快照 | `.runtime/agents.db` | 保存 Agent ID、模板、workspace 配置等控制面信息 |
| 会话快照与恢复状态 | `.runtime/memory/sessions.db` | Node 控制面存储，不属于项目 workspace |
| Agent 私有原始消息审计 | `<workspace>/.dagents/<agent_id>/history/` | JSONL 追加审计；不作为会话恢复真相源 |
| Agent 级记忆 | `<workspace>/.dagents/<agent_id>/memory/memory.db` | workspace 内 SQLite 记忆库 |
| 全局记忆 | `.runtime/memory/global.db` | 仅在 Agent 配置允许全局范围时使用 |
| 过长工具结果 | `<workspace>/tool_outputs/<agent_id>/` | 面向用户/模型的结果文件，按 Agent 隔离 |
| Node 管理资源 | `.runtime/skills/`、`.runtime/externaltools/` | 不属于 Agent workspace，不能用相对路径访问 |

### 升级行为

- 新安装不再创建 `.runtime/data/`，也不再把它作为默认 workspace。旧安装如果已有 `data/`，会原样保留，需由维护者确认内容后再决定是否归档或删除。
- 旧 Agent 快照缺少 `workspace` 配置时，升级后使用 `.runtime/agents/<agent_id>/workspace`。系统不会把旧 Node 根目录下的项目文件、JSONL 或旧长期记忆文件自动复制到新位置；需要保留时请先备份，再人工复制/整理。
- workspace 在 Agent 创建时固定，升级期间不要通过修改 API 请求尝试变更已有 Agent 的 workspace。需要新目录时应创建新的 Agent，并按业务需要迁移项目文件。
- `workspace_root` 只表示 Agent 可操作的文件、命令、终端和工具结果范围；`runtime_root` 只表示 Node 的管理目录。旧集成若仍传 `fs_root`，应在升级前改为明确的其中一个路径语义。
- 旧长期记忆兼容文件不再自动导入新 SQLite memory service。确认备份后，由维护者人工整理内容，再通过现行 memory API 或设置重新写入；不要直接把旧文件放入 `.dagents/<agent_id>/memory/` 伪装成新数据库。

### 升级后检查

1. 启动 Node，确认 `GET /health` 正常并检查 Agent 的 workspace 配置；
2. 打开一个旧 Agent，验证历史仍可恢复，再执行一次只读工具，确认相对路径以 workspace 为基准；
3. 检查 `.dagents/<agent_id>/history/`、`memory/memory.db` 和 `tool_outputs/<agent_id>/` 是否按预期生成；
4. 新建一个临时 Agent，确认不会创建 `.runtime/data/`，并确认 workspace 创建后不能修改；
5. 如使用桌面端，验证 DAgents Desktop 的 Node 连接和更新检查。Go Shell 已退役，不再构建或发布。

## 诊断顺序

1. `GET /health` 是否成功；
2. Desktop 是否连接了预期的 Node，事件流与 hydrate 是否正常；
3. Agent 是否有有效 LLM profile；
4. 工具组和 policy 是否允许目标工具；
5. 若是工作组，检查 Node 是否已注册、WS 是否在线、ACL/订阅和成员 Agent 是否 ready；
6. 若是终端，检查目标类型、连接状态、权限、终端 WebSocket 和命令退出状态。

## 发布门禁

```bash
npm test --prefix node/webui/frontend -- --run
npm run build --prefix manage/console/frontend
go test ./shared/config/... ./shared/logfiles/... ./shared/update/... ./node/... ./client/...
python3 -m unittest discover -s tests -p "test_*.py" -v
git diff --check
```

发布说明、版本号和资产以根目录 `CHANGELOG.md`、`.github/workflows/` 及 [发布流程](../release-process.md) 为准。
