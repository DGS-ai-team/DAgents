# REFERENCE — `shared/config`

## `config.go`

| 符号 | 类型 | 说明 |
|------|------|------|
| `DefaultListenHost` | `const string` | 默认监听 `127.0.0.1` |
| `DefaultListenPort` | `const int` | 默认端口 `18765` |
| `Config` | `struct` | 根配置：agent_id、listen、local、llm、fs_root、manage 等 |
| `ListenConfig` | `struct` | HTTP 监听 host/port |
| `LocalConfig` | `struct` | Client 用 endpoint 与可选 agent_id 校验 |
| `LLMConfig` | `struct` | LLM 配置；`mock=true` 时用 MockClient；`max_tool_loops` 默认 16 |
| `SkillsConfig` | `struct` | skills 开关、`max_in_prompt` |
| `CompressionConfig` | `struct` | `silent_trigger_tokens`、`blocking_trigger_tokens`（`<=0` 关闭对应档位） |
| `TriggersConfig` | `struct` | `enabled`、`poll_seconds` |
| `RawMessageHistoryConfig` | `struct` | 原始消息 JSONL 开关（`enabled` 指针，缺省 true） |
| `EnvRawMessageHistoryEnabled` | `const string` | 环境变量 `AGENT_RAW_MESSAGE_HISTORY_ENABLED` |
| `LogConfig` | `struct` | Node stderr 日志级别（`level`，默认 `info`） |
| `ToolsConfig` | `struct` | `bash_output_encoding`：bash_run 子进程输出解码（空=按 OS/shell 自动） |
| `ManageConfig` | `struct` | Manage 注册开关与 URL |
| `LoadFile` | `func(path string) (*Config, error)` | 读 YAML、展开 env、默认值、校验 |
| `(c *Config) ApplyDefaults` | `method` | 填充 listen/local 缺省 |
| `(c *Config) Validate` | `method` | 校验 agent_id、端口、endpoint |
| `(c *Config) ListenAddr` | `method` | 返回 `host:port` |
| `(c *Config) RuntimeDir` | `method` | 与 `fs_root` 一致（默认 `./.runtime`） |
| `(c *Config) DataDir` | `method` | 默认 `{fs_root}/data` 临时工作区 |
| `(c *Config) SkillsRoot` | `method` | 默认 `{fs_root}/skills` |
| `(c *Config) PolicyDir` | `method` | 默认 `{fs_root}/policy` |
| `(c *Config) ToolPolicyPath` | `method` | 默认 `.runtime/policy/tool.approval.txt` |
| `(c *Config) ShellPolicyDir` | `method` | 默认 `.runtime/policy/shell` |
| `(c *Config) MemoryDir` | `method` | 默认 `.runtime/memory` |
| `(c *Config) SessionDBPath` | `method` | 默认 `{runtime}/memory/sessions.db` |
| `(c *Config) RawMessageHistoryEnabled` | `method` | 是否写 JSONL；env 优先，默认 true |
| `(c *Config) RawMessageHistoryDir` | `method` | 默认 `.runtime/history` |
| `(c *Config) TriggersStorePath` | `method` | 默认 `{fs_root}/triggers/triggers.json` |
| `(c *Config) Capabilities` | `method` | 能力列表（含可选 triggers） |
| `(c *Config) ManageRegistered` | `method` | N0 恒为 false |

## `agent_id.go`

| 符号 | 类型 | 说明 |
|------|------|------|
| `EnvAgentID` | `const string` | 环境变量名 `AGENT_ID` |
| `(c *Config) AgentIDFilePath` | `method` | 默认 `.runtime/agent/agent_id` |
| `(c *Config) ResolveAgentID` | `method` | 从文件/环境/YAML 解析 agent_id 并持久化 |

## `resolve.go`

| 符号 | 类型 | 说明 |
|------|------|------|
| `EnvConfigPath` | `const string` | 环境变量名 `DAGENTS_CONFIG` |
| `ResolveConfigPath` | `func(explicit string) (string, error)` | 解析 -config / 环境变量 / 默认候选路径 |
