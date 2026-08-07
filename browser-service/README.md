# browser-service（dagents-browser）

模式 A 薄服务：**browser-use Agent** 驱动本机 Chrome（CDP），HTTP 契约与 Go `browser.Request/Response` 对齐。

对外 `op` 仅：`ping` / `start` / `stop` / `run_task` / `task_status` / `task_cancel`。

## 启动

与 `dagents-node` 共用 `config.yaml`（须 `browser.enabled: true`、`fs_root` 一致）：

**开发（源码）：**

```bash
cd browser-service
pip install -r requirements.txt
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
  driver: remote
  service_url: http://127.0.0.1:18766
```

## API

| 方法 | 路径 |
|------|------|
| GET | `/health` |
| GET | `/v1/browser/ping` |
| POST | `/v1/browser/call` |

设计说明：[browser-remote-service-mode-a.md](../docs/design/browser-remote-service-mode-a.md)
