# node/internal/browser

主 Agent 所有者的浏览器 Session 管理；经 **RemoteDriver** 调用本机 **dagents-browser**（Python + Playwright）。

## 架构

| 文件 | 说明 |
|------|------|
| `manager.go` | Browser session lifecycle、call/evaluate 串行化 |
| `remote_driver.go` | HTTP → dagents-browser |

安装版 Node 在 `browser.enabled: true` 时会自动启动同目录的 `dagents-browser` sidecar，并在 Node 退出时停止自己启动的进程。服务地址固定为 `http://127.0.0.1:18766`，由 Node 管理，不支持远程 endpoint 或 attach 到外部 Chrome。源码部署如需此功能，需将构建好的 `dagents-browser` 放到 `dagents-node` 同目录。
| `mock_driver.go` | 单测 mock |

薄服务：`browser-service/`（Playwright launch/attach 本机 Chromium/Chrome）。

产品路径：主 Agent 直接调用 `browser_call` / `browser_evaluate`，两者均为同步工具。

设计：[browser-call-main-agent-playwright-migration.md](../../../docs/design/browser-call-main-agent-playwright-migration.md)

## 启用

```yaml
browser:
  enabled: true
  headed: true
tools:
  enabled_groups:
    - browser
```

```bash
# 独立开发调试（生产安装由 Node 自动启动）
cd browser-service && pip install -r requirements.lock
python -m dagents_browser.main --config /path/to/config.yaml
```

## 相关

- [browser-call-main-agent-playwright-migration.md](../../../docs/design/browser-call-main-agent-playwright-migration.md)
- [browser-service/README.md](../../../browser-service/README.md)
