# node/internal/browser

主 Agent 所有者的浏览器 Session 管理；经 **RemoteDriver** 调用本机 **dagents-browser**（Python + Playwright）。

## 架构

| 文件 | 说明 |
|------|------|
| `manager.go` | Browser session lifecycle、call/evaluate 串行化 |
| `remote_driver.go` | HTTP → dagents-browser |
| `mock_driver.go` | 单测 mock |

薄服务：`browser-service/`（Playwright launch/attach 本机 Chromium/Chrome）。

产品路径：主 Agent 直接调用 `browser_call` / `browser_evaluate`，两者均为同步工具。

设计：[browser-call-main-agent-playwright-migration.md](../../../docs/design/browser-call-main-agent-playwright-migration.md)

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
cd browser-service && pip install -r requirements.lock
python -m dagents_browser.main --config /path/to/config.yaml
```

## 相关

- [browser-call-main-agent-playwright-migration.md](../../../docs/design/browser-call-main-agent-playwright-migration.md)
- [browser-service/README.md](../../../browser-service/README.md)
