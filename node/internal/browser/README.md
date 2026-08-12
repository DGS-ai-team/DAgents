# node/internal/browser

伴生浏览器 session 管理；经 **RemoteDriver** 调用本机 **dagents-browser**（Python + browser-use Agent）。

## 架构

| 文件 | 说明 |
|------|------|
| `manager.go` | `BrowserManager` Start/Stop、session 上限 |
| `manager_task.go` | `RunTask*` / `TaskStatus` / `TaskCancel` |
| `remote_driver.go` | HTTP → dagents-browser |
| `mock_driver.go` | 单测 mock |

薄服务：`browser-service/`（browser-use Agent + CDP attach 本机 Chrome）。

产品路径：主 Agent 仅 `browser_run_task` / `browser_task_status` / `browser_task_cancel`。

设计：[browser-remote-service-mode-a.md](../../../docs/design/browser-remote-service-mode-a.md)

## 启用

```yaml
browser:
  enabled: true
  service_url: http://127.0.0.1:18766
  headed: true
tools:
  enabled_groups:
    - browser
```

```bash
# 同机另起（与 Node 共用 config.yaml）
cd browser-service && pip install -r requirements.txt
python -m dagents_browser.main --config /path/to/config.yaml
```

## 相关

- [browser-tools-and-demonstration.md](../../../docs/design/browser-tools-and-demonstration.md)
- [browser-service/README.md](../../../browser-service/README.md)
