# Browser 模式 A：薄服务架构

Go Node 保留 **turn loop、Policy、HITL、伴生 Agent 与任务级 `browser_*` 工具**；Chrome/CDP 与 **browser-use Agent 闭环** 由本机 **`dagents-browser`（Python）** 执行。

## 架构

```text
LLM（主 Agent）→ browser_run_task / task_status / task_cancel
        → BrowserManager（Start/Stop/RunTask*）
        → RemoteDriver ──HTTP──► dagents-browser :18766
                                      → browser_use.Agent + 本机 Chrome (CDP)
```

| 组件 | 职责 |
|------|------|
| **dagents-node** | 伴生 Agent、任务工具 schema、审批、session 映射 |
| **dagents-browser** | session 生命周期 + `run_task`（browser-use Agent 闭环） |
| **本机 Chrome** | 实际渲染与交互 |

Go 进程内 **不** 嵌入 rod/CDP；**不** 再向主 Agent 暴露 navigate/click 等细粒度工具。细粒度 DOM 动作仅发生在 sidecar 的 `browser_use.Agent` 内部。

## 配置

### dagents-node

```yaml
browser:
  enabled: true
  service_url: http://127.0.0.1:18766
  headed: true
  chrome_path: ""
  cdp_url: ""
  debug_port: 9222
tools:
  enabled_groups:
    - browser
```

### dagents-browser

与 Node **共用** `config.yaml`（`fs_root`、`browser.*` 须一致）：

```bash
cd browser-service
pip install -r browser-service/requirements.lock
python -m dagents_browser.main --config /path/to/config.yaml --listen 127.0.0.1:18766
```

## HTTP API

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/health` | 存活探测 |
| GET | `/v1/browser/ping` | 驱动 ping |
| POST | `/v1/browser/call` | body = Go `browser.Request`，响应 = `browser.Response` |

### 支持的 `op`

| `op` | 说明 |
|------|------|
| `ping` | 探活 |
| `start` / `stop` | Chrome session 生命周期（由 Manager / `run_task` 自动调用） |
| `run_task` | 派发自然语言任务给 `browser_use.Agent` |
| `task_status` / `task_cancel` | 查询 / 取消任务 |

其它 `op`（历史 navigate/click/snapshot 等）一律返回 `unknown op`。

## 部署（内网同机）

```text
Win Server（RDS/桌面会话）
├── dagents-node        :18765
├── dagents-browser     :18766   # Python，仅 127.0.0.1
└── Chrome              CDP
```

## 相关

- [browser-tools-and-demonstration.md](./browser-tools-and-demonstration.md)
- [browser-service/README.md](../../browser-service/README.md)
- Go：`node/internal/browser/remote_driver.go`、`manager.go`、`manager_task.go`
- Python：`browser-service/dagents_browser/`
