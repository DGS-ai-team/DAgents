# 从 v0.11.0 迁移到当前版

这份说明适用于从正式版 `v0.11.0` 的 Windows 安装迁移到当前 Desktop + Node API 版本。建议采用“完整备份旧数据、卸载旧程序、安装新版、恢复数据”的方式。旧安装没有当前安装器要求的 ownership marker，因此新版安装器会拒绝直接覆盖；不要把 QA 安装器或同一源码下的测试当作旧版应用升级验收。

目前没有完成真实 v0.11.0 安装包到当前安装包的端到端迁移验收。仓库内的 Go in-process 测试只验证脱敏 v0.11 数据文件可由当前持久化 Store 读取，以及 pending trigger 的安全恢复行为；它不能证明 Windows 安装器、卸载器、快捷方式、WebView2 或用户工作目录迁移全部正常。

## v0.11.0 的数据位置

v0.11.0 已采用精简引导 YAML 和 SQLite/JSON 持久化，不是把所有业务设置都放在 YAML 的版本。当前版的 `config.yaml` 是 listen-only 引导配置。Node ID 通常保存在 `.runtime/node/node_id`，`NODE_ID` 环境变量可覆盖它；不要把历史 YAML 中可被读取的 `local` 或 `node_id` 字段当成当前版应恢复的配置项。正常安装的数据分为两部分：

- Node 安装目录下的 `config.yaml` 保存监听地址；同目录 `.runtime/` 保存 Node ID、Node 设置、模型档案、Agent 注册表、会话、触发器和其它 Node 状态。
- `.runtime/llm_configs.db` 中的密文必须与 `.runtime/.llm_secret_key` 配套。**只备份数据库、不备份 `.llm_secret_key`，不能恢复 API Key。** 密钥文件缺失或与数据库不匹配时，当前解密会失败；密文行仍存在，但不能作为凭据可用的证据。
- Agent 的模板、Auto 配置和模型绑定保存在 `.runtime/agents.db` 的 `config_snapshot_json`。自定义 workspace 的绝对路径也保存在该快照；workspace 内容在该绝对路径的文件系统目录中，不在 `agents.db` 里。
- 私有 Agent workspace 位于 `.runtime/agents/<agent_id>/workspace`。自定义 workspace 之外的文件及其 `.dagents/<agent_id>/` 历史、memory 等 sidecar 必须另外备份。
- Trigger 定义及 fire history 位于 `.runtime/triggers/triggers.json`。v0.11.0 已写入 schema version 2；若存在未确认是否已投递的 pending delivery，重启后系统会暂停 trigger 并要求显式恢复，避免重复执行。
- 会话快照位于 `.runtime/memory/sessions.db`。Agent workspace 外的记忆和文件仍需按对应 workspace 路径备份。

对照 `v0.11.0` tag 和当前源码，Node settings、LLM configs、Agent、session 的 SQLite 表结构没有需要本次升级执行的协议迁移；Trigger schema 2 也与当前 reader 一致。当前 runtime JSON 增加的字段是可选字段，旧记录可读取。当前 Go fixture 测试验证了代表性 SQLite 记录、配对加密 key、Auto/Agent 快照、workspace 路径、会话 transcript 与 trigger pending-delivery recovery。它是持久化兼容性测试，不是旧版安装迁移通过记录。

`config.yaml` 不是 v0.11.0 的业务设置备份替代品。当前代码在 `.runtime/node_settings.db` 为空时会写入产品默认设置；它不会从早期 fat YAML 自动重建 Node settings 数据库。若旧目录没有 `node_settings.db`，或没有 `llm_configs.db`，先保留 `config.yaml` 和整个旧目录，不要先启动新版覆盖现场；需要人工核实旧安装来源并重新录入无法从数据库恢复的设置。仓库没有声称完成 fat-YAML 自动导入。

v0.11.0 的 `compression.idle_auto_compress_seconds`、`idle_auto_compress_poll_seconds`、`idle_auto_compress_min_tokens` 不会迁移成当前 `idle_new_conversation_seconds`：旧行为是空闲时模型压缩，新行为是在新的人类输入前开启上下文边界，语义不同。旧字段会被当前配置反序列化忽略；新设置默认 `0`（禁用），如需使用请在当前 Node 设置中单独启用并选择阈值。

## 操作步骤

1. 在 Desktop 与 Node 都停止后，复制整个旧安装目录到另一块安全位置。至少确认备份含 `config.yaml`、整个 `.runtime/`、`.runtime/.llm_secret_key` 和所有 `.db`、WAL/SHM 文件。若 Node 不是从安装目录启动，另行备份实际 Node 工作目录中的 `.runtime/`。
2. 对每个自定义 workspace，单独备份其完整目录，包括隐藏的 `.dagents/<agent_id>/` 内容。记下每个自定义 workspace 的绝对路径，并在恢复时保留该路径：Agent 创建后 workspace 路径不可修改。若原路径无法继续使用，应创建新 Agent，并显式迁移所需的业务文件和记忆；不要编辑 Agent 快照来绕过路径规则。保留原始目录备份，直到新 Agent 验证完成。
3. 备份 Desktop 本地偏好文件（若存在）：`%APPDATA%\DAgents\desktop\settings.json`。它与 Node `.runtime/` 分开，不能由 Node 数据库备份代替。
4. 记录旧安装目录和版本、Node ID、workspace 路径；对备份做第二份离线副本，并比较目录清单或文件哈希。不要把 `.llm_secret_key` 内容贴入工单、日志或聊天。
5. 卸载 v0.11.0，再从当前发行包进行全新安装。不要对旧无 marker 安装执行原地升级。将旧 `.runtime/` 整个恢复到新版 Node 的运行目录，确保 `.runtime/node/node_id`、数据库及 `.llm_secret_key` 配套恢复；`config.yaml` 按当前 listen-only 格式配置监听地址，不要盲目恢复旧 `local` 或 `node_id` YAML 字段。若有 `NODE_ID` 环境变量，它会覆盖 runtime 中保存的 Node ID。保持 Node 停止状态直到确认数据文件都在位。若安装器在卸载/安装时保留了文件，也应以独立备份作为恢复源，不依赖这一点。
6. 恢复自定义 workspace 到数据库中记载的原绝对路径。恢复 Desktop 偏好到其原本用户目录。不要移动 workspace 文件来“匹配”新版安装目录；workspace 与程序安装路径互相独立。
7. 首次启动后核对 Node ID、Node settings、Agent 列表、Auto 配置、LLM profile 名称/供应商/模型/key 状态、Trigger 目标与 pending recovery 状态，以及每个 workspace 文件。先确认 Agent 绑定到的 `active` LLM profile ID 在 `llm_configs.db` 中仍存在；改名 profile 可能需要另外修正 Agent 的模型引用。用脱敏 fixture 通过不表示真实 Key 可用，真实 API 请求需由使用者在受控环境中验证。
8. 若新版不能读取恢复的数据，停止新版 Node/Desktop，保留失败现场，移除新版程序后从离线副本恢复旧安装目录和外部 workspace，再使用原 v0.11.0 程序恢复服务。不要让新版首次启动写入默认数据库后覆盖唯一的旧数据副本。

## 当前验证范围

已由 Go in-process 脱敏 fixture 覆盖：v0.11 形态的 Node settings 与 LLM 档案读取、配套 key 解密、缺少配套 key 时显式解密失败、Agent/Auto 配置快照与 workspace 路径保留、session 历史读取，以及 schema 2 trigger 恢复时暂停 pending delivery 并保留历史。没有读取真实密钥，也没有启动 Node、Desktop、浏览器或安装器。

仍需在一次性 Windows 用户或 VM 中按 [安装升级验收清单](../../packaging/windows/install-upgrade-acceptance.md) 做真实旧版备份、卸载、新版安装和恢复；验证 Desktop 本地设置、权限、快捷方式、自启和故障回滚。完成前不要将 P0 “v0.11.0 用户迁移”标为通过。
