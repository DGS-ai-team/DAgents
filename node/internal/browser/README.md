# node/internal/browser

browser_* 工具的 session 管理；经 **RemoteDriver** 调用本机 **dagents-browser**（Python + browser-use）。

## 架构

| 文件 | 说明 |
|------|------|
| `manager.go` | `BrowserManager`、URL 校验、session 上限 |
| `remote_driver.go` | HTTP → dagents-browser |
| `mock_driver.go` | 单测 mock |

薄服务：`browser-service/`（browser-use + CDP attach 本机 Chrome）。

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
