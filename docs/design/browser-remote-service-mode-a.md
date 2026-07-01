# Browser 模式 A：薄服务架构

Go Node 保留 **turn loop、Policy、HITL、`browser_*` 工具**；Chrome/CDP 与 **browser-use DOM 序列化** 由本机 **`dagents-browser`（Python）** 执行。

## 架构

```text
LLM → dagents-node (browser_* tools)
        → BrowserManager
        → RemoteDriver ──HTTP──► dagents-browser :18766 (Python + browser-use)
                                      → 本机 Chrome (CDP)
```

| 组件 | 职责 |
|------|------|
| **dagents-node** | 工具 schema、审批、session 映射、URL 校验 |
| **dagents-browser** | browser-use：DOM 序列化、navigate/click/fill/… |
| **本机 Chrome** | 实际渲染与交互 |

Go 进程内 **不再** 嵌入 rod/CDP；已删除 interim Go 薄服务与自研 snapshot JS。

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
pip install -r requirements.txt
python -m dagents_browser.main --config /path/to/config.yaml --listen 127.0.0.1:18766
```

## HTTP API

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/health` | 存活探测 |
| GET | `/v1/browser/ping` | 驱动 ping |
| POST | `/v1/browser/call` | body = Go `browser.Request`，响应 = `browser.Response` |

`snapshot` 响应：`llm_representation` 提升到工具 JSON 顶层；`detail.elements` 为 index/tag/text 摘要。

`click` / `fill`：**优先 `index`**；`selector` 仅 fallback。

`click_coordinate`：视觉模式（`multimodal.enabled: true`）下 `(x,y)` 点击。

视觉模式下 `snapshot` 带 `include_screenshot` + `path`，截图路径写入 `screenshot_path` 并由 Go 自动 vision 注入。

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
- Go：`node/internal/browser/remote_driver.go`
- Python：`browser-service/dagents_browser/`
