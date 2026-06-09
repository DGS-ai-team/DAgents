# 共享配置（`shared/config`）

Agent Node 与 Client 共用的 YAML 配置加载与校验。

## 文件

| 文件 | 说明 |
|------|------|
| `config.go` | `Config` 结构体、`LoadFile`、`Validate`、`ApplyDefaults` |
| `agent_id.go` | `ResolveAgentID`、`AgentIDFilePath`：`.runtime/agent/agent_id` 持久化 |
| `resolve.go` | `ResolveConfigPath`：`-config` / `DAGENTS_CONFIG` / 默认候选路径 |
| `config_test.go` | 默认值、必填项、环境变量展开单测 |

示例配置见 [`packaging/agent-client/config.example.yaml`](../../packaging/agent-client/config.example.yaml)。

## 主要配置块

| 块 | 说明 |
|----|------|
| `listen` / `local` | Node 监听与 Client 连接 endpoint |
| `llm` | 模型、mock、tool loop 上限 |
| `fs_root` | 工具文件沙箱根（缺省为 `data_dir` 的父目录，即 `./.runtime`） |
| `skills` | 技能目录与 prompt 上限 |
| `compression` | 上下文压缩 token 阈值 |
| `triggers` | 触发器调度（见下表） |
| `tools` | 内置工具；`bash_output_encoding` 控制 bash_run 输出解码（空=按 OS/shell 自动） |
| `log` | Node stderr 日志级别 |

### `triggers`

| 键 | 默认 | 说明 |
|----|------|------|
| `enabled` | `true` | 是否启动后台调度轮询 |
| `poll_seconds` | `5` | 到期扫描间隔（秒，至少 1） |
| `store_path` | `.runtime/triggers/triggers.json` | 触发器 JSON 持久化路径（可选覆盖） |

condition 语义（interval / fire_at / schedule / cmd）见 [`node/internal/triggers/README.md`](../../node/internal/triggers/README.md)。

## 测试

```bash
go test ./shared/config/...
```
