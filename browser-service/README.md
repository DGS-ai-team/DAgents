# browser-service（dagents-browser）

薄服务：**Playwright** 驱动本机 Chromium/Chrome，HTTP 契约与 Go `browser.Request/Response` 对齐。主 Agent 直接控制浏览器，sidecar 不调用 LLM。

对外工具为 `browser_call` 和 `browser_evaluate`；HTTP 路径为 `/v2/browser/call` 与 `/v2/browser/evaluate`。

## 平台要求

浏览器能力使用当前 Playwright Chromium 支持的平台矩阵：Windows 工作站要求
Windows 11（build 22000）或更高版本；Windows Server 要求 Windows Server 2019
或更高版本。Windows 10 及更早的 Windows 工作站不会暴露原生浏览器工具，Node
会返回 `unsupported_browser_platform`；Linux/macOS 仍按 Playwright/Chromium 的
官方支持矩阵执行。Windows 10 上不能通过保留旧 backend 绕过这一门槛。

## 启动

与 `dagents-node` 共用 `config.yaml`（须 `browser.enabled: true`；浏览器服务使用固定的 `runtime_root`，默认 `./.runtime`）：

**开发（源码）：**

```bash
cd browser-service
pip install -r requirements.lock
python -m playwright install chromium
python -m dagents_browser.main --config ../packaging/agent-client/config.yaml --listen 127.0.0.1:18766
```

**发布包（PyInstaller 单文件，CI 产出 `dist/dagents-browser`）：**

```bash
# Windows
dagents browser --background
# 或
bin\dagents-browser.exe --config config.yaml

# Linux
./dagents browser --background
```

本地打包：`scripts/ci/build_dagents_browser.sh`（参数见 workflow `BROWSER_PYINSTALLER_ARGS`）。

## Node 配置

```yaml
browser:
  enabled: true
  service_url: http://127.0.0.1:18766
```

## API

| 方法 | 路径 |
|------|------|
| GET | `/health` |
| GET | `/v2/browser/ping` |
| POST | `/v2/browser/call` |
| POST | `/v2/browser/evaluate` |

设计说明：[browser-call-main-agent-playwright-migration.md](../docs/design/browser-call-main-agent-playwright-migration.md)
