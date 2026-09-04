# REFERENCE — `shared/config`

## `config.go`

| 符号 | 类型 | 说明 |
|------|------|------|
| `DefaultListenHost` | `const string` | 默认监听 `127.0.0.1` |
| `DefaultListenPort` | `const int` | 默认端口 `18765` |
| `Config` | `struct` | 根配置：node_id、listen、local、llm、runtime_root、manage 等 |
| `ListenConfig` | `struct` | HTTP 监听 host/port |
| `LocalConfig` | `struct` | Client 用 endpoint 与可选 agent_id 校验 |
| `LLMConfig` | `struct` | LLM 配置；`mock=true` 时用 MockClient；工具步数上限见 Agent snapshot `max_steps` |
| `SkillsConfig` | `struct` | skills 开关、`max_in_prompt` |
| `CompressionConfig` | `struct` | `silent_trigger_tokens`、`blocking_trigger_tokens`（`<=0` 关闭对应档位）；`idle_auto_compress_seconds` / `idle_auto_compress_poll_seconds` / `idle_auto_compress_min_tokens`（无动作自动压缩） |
| `MemoryConfig` | `struct` | `auto_extract`（默认 false）；`candidate_queue_size`（默认 16）；`max_candidates`（默认 8）；`core_budget_tokens`（默认 2000）；仅控制压缩后的可选后台候选整理 |
| `TriggersConfig` | `struct` | `enabled`、`poll_seconds` |
| `RawMessageHistoryConfig` | `struct` | 原始消息 JSONL 开关（`enabled` 指针，缺省 true） |
| `EnvRawMessageHistoryEnabled` | `const string` | 环境变量 `AGENT_RAW_MESSAGE_HISTORY_ENABLED` |
| `LogConfig` | `struct` | Node stderr 日志级别（`level`，默认 `info`） |
| `ToolsConfig` | `struct` | `bash_output_encoding`；`file_encoding`；`bash_compress`（工具组由 Agent 快照配置） |
| `AllBuiltinToolGroupNames` | `func() []string` | 可配置工具组名字典序全集 |
| `BuiltinToolGroupMembers` | `func(group string) ([]string, bool)` | 展开组内工具名 |
| `AllBuiltinToolNames` | `func() []string` | 全部内置工具名字典序全集 |
| `NormalizeBuiltinToolGroups` | `func([]string) []string` | 去重规范化工具组名 |
| `ExpandBuiltinToolGroups` | `func([]string) []string` | 将工具组展开为工具名 |
| `ValidateBuiltinToolGroups` | `func([]string) error` | 校验工具组名与展开结果 |
| `ManageConfig` | `struct` | Manage 开关、URL、node_token、`registration`、`update`、`workgroup` |
| `AgentConfig` | `struct` | `name`、`description`、`capabilities`、`metadata` |
| `ManageRegistrationConfig` | `struct` | `base_url`、`interval_seconds`（默认 30）、`ttl_seconds`（默认 60）、`team` |
| `LoadFile` | `func(path string) (*Config, error)` | 读 YAML、展开 env、默认值、校验 |
| `(c *Config) ApplyDefaults` | `method` | 填充 listen/local/manage 缺省 |
| `(c *Config) Validate` | `method` | 校验 node_id、端口、endpoint；manage.enabled 时要求 url |
| `(c *Config) DiscoveryGroups` | `method` | YAML `groups` 字段（**不**发往 Manage） |
| `(c *Config) ManageRegistrationInterval` | `method` | 注册/心跳轮询间隔 |
| `(c *Config) ManageRegistryBaseURL` | `method` | 上报 Manage 的 base_url（优先 registration.base_url） |
| `(c *Config) ManageRegistryBaseURLIsLoopback` | `method` | 上报地址是否为 loopback |
| `(c *Config) ListenAddr` | `method` | 返回 `host:port` |
| `(c *Config) RuntimeDir` | `method` | 返回 Node 控制面运行目录（默认 `./.runtime`） |
| `(c *Config) SkillsRoot` | `method` | 默认 `{runtime_root}/skills`，不属于 Agent workspace |
| `(c *Config) PolicyDir` | `method` | 默认 `{runtime_root}/policy` |
| `(c *Config) ToolPolicyPath` | `method` | 默认 `.runtime/policy/tool.approval.txt` |
| `(c *Config) ShellPolicyDir` | `method` | 默认 `.runtime/policy/shell` |
| `(c *Config) MemoryDir` | `method` | 默认 `.runtime/memory`，Node 控制面目录，不是 Agent workspace 状态 |
| `(c *Config) SessionDBPath` | `method` | 默认 `{runtime}/memory/sessions.db`，按 `agent_id` 隔离的控制面快照 |
| `(c *Config) RawMessageHistoryEnabled` | `method` | 是否写 JSONL；env 优先，默认 true |
| `(c *Config) RawMessageHistoryDir` | `method` | 未绑定 Agent 时默认 `.runtime/history`；正常 Agent runtime 改用 workspace 下 `.dagents/<agent_id>/history/` |
| `(c *Config) TriggersStorePath` | `method` | 默认 `{runtime_root}/triggers/triggers.json` |
| `(c *Config) Capabilities` | `method` | 能力列表（含可选 triggers） |

## `node_id.go`

| 符号 | 类型 | 说明 |
|------|------|------|
| `EnvNodeID` | `const string` | 主环境变量名 `NODE_ID` |
| `(c *Config) NodeIDFilePath` | `method` | 默认 `.runtime/node/node_id` |
| `(c *Config) ResolveNodeID` | `method` | 从环境、`.runtime/node/node_id` 或配置解析 node_id 并持久化 |

## `resolve.go`

| 符号 | 类型 | 说明 |
|------|------|------|
| `EnvConfigPath` | `const string` | 环境变量名 `DAGENTS_CONFIG` |
| `ResolveConfigPath` | `func(explicit string) (string, error)` | 解析 -config / 环境变量 / 默认候选路径 |
