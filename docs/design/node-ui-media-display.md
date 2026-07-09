# Node → Web UI 图片 / 截图展示方案

**状态（2026-07）**：规划中。  
**范围**：Agent Node 将工具截图、读图结果、用户上传图等 **展示到 Web UI**（含 SSE 实时与 Hydrate 回放）。  
**读者**：Node / Web UI 实现；与 [browser-remote-service-mode-a.md](./browser-remote-service-mode-a.md)、[windows-desktop-shell.md](./windows-desktop-shell.md) §3.9 Hydrate 配套。

---

## 1. 背景与问题

### 1.1 现状

| 能力 | Node / 工具侧 | Web UI 侧 |
|------|---------------|-----------|
| 用户发图（multimodal） | `POST /v1/messages` + `content_parts`（`data:` URL） | ✅ 输入框预览 + `MessageBubble` 展示 |
| `read_image` | 工具返回 **文本元数据**；图像经 `ReadImageVisionPayload` **注入 LLM**（`content_parts`） | ❌ 工具气泡仅文本 |
| `browser_snapshot` / `browser_screenshot` | JSON 结果含 `screenshot_path`（相对 `fs_root`）；视觉模式同样注入 LLM | ❌ 仅 `ToolExecBubble` 文本；CSS 已有 `.tool-exec-bubble__image` 未使用 |
| Hydrate / 历史回放 | `messages[]` 含 `content_parts`（可能巨大 `data:` URL） | ❌ 切换 session 清空 transcript；无 media API |
| 静态文件服务 | **无** `GET` 读 `fs_root` 图片的 UI 专用 API | — |

**核心缺口**：图像 **已进 LLM**，但 **未以稳定、可回放的引用** 暴露给 Client UI。

### 1.2 目标场景

1. **Browser 自动化**：`browser_snapshot` / `browser_screenshot` / 带截图的 `browser_navigate` 后在聊天区看到页面截图。  
2. **读图工具**：`read_image` 成功后展示缩略图 + 路径，而非仅 `[READ_IMAGE] path=…`。  
3. **用户上传**：刷新 / Hydrate 后仍能显示用户曾发送的图片（不依赖浏览器内存里的 `data:` URL）。  
4. **Hydrate 一致**：`MessagesToTranscriptEntries` / `/hydrate` 的 transcript 含 **可渲染 media 引用**，与 SSE 增量去重（F-H9）。

### 1.3 非目标（当前阶段）

- Shell 代理图片（Web UI 仍 **直连 Node**）。  
- 在 SSE 中内联 **base64 大图**（Hub 256、带宽、SQLite 体积）。  
- 视频 / PDF 预览（可后续扩展 `kind`）。  
- 将图片存 Manage / 外网 CDN。

---

## 2. 设计原则

| # | 原则 | 说明 |
|---|------|------|
| P1 | **引用，不内联** | SSE / hydrate JSON 只带 **`media[]` 元数据 + URL**；字节流走 `GET …/media/{id}`。 |
| P2 | **Session 绑定** | `media_id` 归属 `session_id`；鉴权与现有 Node API key 一致。 |
| P3 | **沙箱路径** | 仅允许 `fs_root` 内已有文件，或 `.runtime/media/{session_id}/` 下拷贝；禁止任意绝对路径。 |
| P4 | **与 LLM 路径解耦** | 注入 LLM 仍可用 `data:` URL（现有 vision stash）；**UI 展示**走 artifact 注册，避免重复读盘逻辑散落 UI。 |
| P5 | **Hydrate 同源** | 实时 SSE 与 `/hydrate` transcript 使用 **同一套 `media` 结构**（F-H14）。 |
| P6 | **显式 + 自动** | Agent 可调 **`show_image`** 主动出示；`browser_*` / `read_image` **自动**挂 `media[]`，避免模型漏调。 |

---

## 3. 架构：Session Media Artifact + `show_image` 工具

### 3.0 双通道展示策略（产品确认 2026-07）

| 通道 | 机制 | 适用 |
|------|------|------|
| **显式** | Agent 调用 **`show_image(path, caption?)`** | 对比图、历史文件、任意「请用户看这张图」 |
| **自动** | Node 在 `browser_*` / `read_image` 成功时注册 media 并附 SSE `media[]` | 截图、读图后 **默认**出图，无需再调 `show_image` |

二者共用 **MediaRegistry + GET media API**；Web UI 优先渲染 `media[]`，并对 `tool_name === "show_image"` 做 **工具特化**（与 `read_file` → `ReadFileResultPreview` 同模式）。

**分工**：`read_image` 面向 **LLM vision**；`show_image` 面向 **人眼 UI**；可只调其一，也可先 `read_image` 再 `show_image` 同一路径。

### 3.0.1 `show_image` 工具定义（草案）

```yaml
name: show_image
policy: auto                    # 纯展示，无 HITL
parameters:
  path: string                  # 必填，相对 fs_root
  caption: string               # 可选，气泡说明文字
```

**Node 行为**：

1. 校验 `path` 在 `fs_root` 内、后缀为 `.jpg|.jpeg|.png|.gif|.webp`、≤10MB  
2. `MediaRegistry.RegisterFromRelPath(session, toolCallID, path, source=show_image)`  
3. 工具文本结果（给 LLM）：`[SHOW_IMAGE] path=… status=ok` + 可选 caption  
4. `publishToolResult(..., extra{ media: [{ id, url, mime, label: "show_image" }] })`

**Web UI 行为**（F-M3）：

```javascript
// 与 isReadFileTool 并列
export function isShowImageTool(name) {
  return String(name || "").trim() === "show_image";
}
// ToolExecBubble：isShowImageTool || entry.data.media?.length → ImageResultPreview
```

**TUI**（F-M8）：打印 `path` 与 media URL，不渲染像素。

### 3.1 概念

**MediaArtifact**：一次可在 UI 展示的二进制资源（主要为 image/png、jpeg、webp、gif）。

```text
Session
  └── MediaRegistry（内存 + 可选 SQLite 索引 / 随 session persist 轻量元数据）
        └── MediaArtifact
              · id          med_{ulid}
              · session_id
              · kind        image（远期 video）
              · mime
              · source      tool | user_upload | browser
              · tool_call_id（可选）
              · rel_path    相对 fs_root 或 .runtime/media/…
              · bytes       文件大小
              · width/height（可选，png/jpeg 头解析）
              · label       如 "browser_snapshot"
              · created_at
```

### 3.2 注册时机（Node 工具层）

| 来源 | 触发 | 存储策略 |
|------|------|----------|
| **`show_image`** | `execShowImage` | **引用** `fs_root` 内 `path`（不拷贝） |
| `read_image` 成功 | `execReadImage` | **引用** + 自动 `media[]`（可与 show 并存） |
| `browser_*` 带 `screenshot_path` | `stashBrowserVisionFromScreenshot` 同级 | **引用** browser 输出 PNG |
| 用户 `content_parts` 含 `data:` URL | `EnqueueMessage` / `handleHumanMessage` | **拷贝** 到 `.runtime/media/{session_id}/` |
| 未来工具 | 工具返回 `MediaRef` | 统一走 Registry |

工具执行完成后，turn 层在 `publishToolResult` 的 **`extra`** 中附带 `media: [{ id, kind, mime, url, label }]`。

### 3.3 数据流

```text
工具成功 / 用户发图
    → MediaRegistry.Register(session, artifact)
    → publishToolResult(..., extra{ media: [...] })   // SSE
    → persist 时 messages 或 sidecar 存 media 元数据   // Hydrate

Web UI
    → tool_result SSE / hydrate entry 见 media[]
    → <img src="{url}?token=…"> 或 Authorization header（见 §4.2）
    → GET /v1/sessions/{session_id}/media/{media_id}
    → Node 读 rel_path → stream bytes（Content-Type / Cache-Control）
```

与 [windows-desktop-shell.md](./windows-desktop-shell.md) D24 一致：**窄 HTTP GET**，不走 Shell 代理。

---

## 4. API 设计

### 4.1 `GET /v1/sessions/{session_id}/media/{media_id}`

**用途**：UI `<img>` / 新标签打开 / 下载。

| 项 | 说明 |
|----|------|
| 鉴权 | 与 Node 其它 API 相同（`Authorization` / 配置 API key）；**须**校验 `media.session_id == path session_id` |
| 响应 | `200` + `Content-Type: image/*`；`404` media 不存在或文件已删 |
| Query | `thumbnail=1`（P2）：最长边 ≤480px，JPEG/PNG/GIF 服务端缩放；WebP 暂回退原图 |
| 缓存 | `Cache-Control: private, max-age=3600`；artifact 不可变（id 对应固定文件） |

**不提供**泛化 `GET /fs?path=`：避免路径遍历与 UI 侧解析工具文本。

### 4.2 Web UI 如何加载

**推荐（P0）**：相对 URL + 与 `apiFetch` 相同鉴权头（`fetch` blob → `object URL`，或 cookie-less Bearer）。

```http
GET /v1/sessions/sess_abc/media/med_01HXYZ
Authorization: Bearer <node_api_key>
```

**备选（P2）**：短期 signed query `?sig=…&exp=…` 供 `<img src>` 无 header 场景（需评估密钥轮换）。

### 4.3 SSE `tool_result` 扩展

在现有 payload 上 **可选** 增加 `media` 数组（旧 Client 忽略）：

```json
{
  "tool_call_id": "call_1",
  "tool_name": "browser_snapshot",
  "content": "{ \"ok\": true, \"screenshot_path\": \".runtime/browser/…/shot.png\", … }",
  "media": [
    {
      "id": "med_01HXYZ",
      "kind": "image",
      "mime": "image/png",
      "url": "/v1/sessions/sess_abc/media/med_01HXYZ",
      "label": "browser_snapshot",
      "width": 1280,
      "height": 720
    }
  ]
}
```

`read_image` / **`show_image`** 示例：`content` 为结构化文本；**展示靠 `media[0]`**。

**`show_image` SSE 示例**：

```json
{
  "tool_call_id": "call_2",
  "tool_name": "show_image",
  "content": "[SHOW_IMAGE]\npath=reports/chart.png\nstatus=ok",
  "arguments": { "path": "reports/chart.png", "caption": "本季度趋势" },
  "media": [
    {
      "id": "med_01HABC",
      "kind": "image",
      "mime": "image/png",
      "url": "/v1/sessions/sess_abc/media/med_01HABC",
      "label": "show_image",
      "caption": "本季度趋势"
    }
  ]
}
```

### 4.4 Hydrate / Transcript 条目

与 F-H14 `MessagesToTranscriptEntries` 对齐：

```json
{
  "kind": "tool_result",
  "tool_name": "show_image",
  "text": "[SHOW_IMAGE]…",
  "arguments": { "path": "reports/chart.png" },
  "media": [ { "id": "med_…", "kind": "image", "mime": "image/png", "url": "…" } ]
}
```

```json
{
  "kind": "tool_result",
  "tool_name": "browser_snapshot",
  "text": "…",
  "media": [ { "id": "med_…", "kind": "image", "mime": "image/png", "url": "…" } ]
}
```

```json
{
  "kind": "user",
  "text": "请分析这张图",
  "media": [ { "id": "med_…", "kind": "image", "mime": "image/jpeg", "url": "…" } ]
}
```

**去重（F-H9）**：若 SSE 已展示某 `media.id`，hydrate 合并时按 `id` 跳过重复渲染。

### 4.5 可选：`POST /v1/sessions/{session_id}/media`（P1）

用户发图前先上传 multipart，返回 `{ id, url }`，`POST /v1/messages` 的 `content_parts` 改为 **引用 URL** 而非巨型 `data:`。  
P0 可在 **Enqueue 时服务端从 data URL 落盘** 代替独立 upload 端点。

---

## 5. Node 实现要点

### 5.1 包结构（建议）

```text
node/internal/media/
  registry.go      Register / Get / ListBySession / BindToolCall
  serve.go         HTTP handler + path resolve + thumbnail
  user_ingest.go   data URL → file under .runtime/media/
  tool_hook.go     RegisterFromRelPath(...)

node/internal/tools/
  fs_show_image.go execShowImage（新）
```

### 5.2 与现有代码衔接

| 位置 | 改动 |
|------|------|
| **`tools/fs_show_image.go`** | **新工具** `show_image`；注册 handler + tool def |
| `tools/registry_enabled.go` | `show_image` 纳入 FS 工具组（**不**依赖 multimodal.enabled） |
| `tools/fs_read_image.go` | 成功后注册 media + SSE `media[]` |
| `tools/browser_vision.go` | `stashBrowserVisionFromScreenshot` 同时注册 media |
| `turn/sse_publish.go` | `publishToolResult` 合并 `extra["media"]` |
| `session/runtime.go` | 用户消息含 image part 时 `user_ingest` 落盘 |
| `api/server.go` | 注册 `GET …/media/{media_id}` |
| `store/`（P1） | session persist 时序列化 registry 元数据 |

### 5.3 LLM 与 UI 双路径

```text
read_image / browser 截图
    ├─► LLM：现有 ReadImageVisionPayload → data URL 注入 user 消息（不变）
    └─► UI：MediaArtifact 引用 fs 上同一文件（P4 不重复 base64 存三处）
```

用户上传若已落盘为 artifact，LLM 请求仍可在 turn 内 **按需** 读文件生成 data URL（与现 multimodal 一致），SQLite 中 `content_parts` 可逐步改为 **存 media id 引用** 以降低库体积（P1 迁移）。

---

## 6. Web UI 实现要点

### 6.1 组件

| 组件 | 职责 |
|------|------|
| `ImageResultPreview.vue`（新） | 缩略图 + caption + 点击放大；复用 `.tool-exec-bubble__image` |
| `ToolExecBubble.vue` | `isShowImageTool` **或** `entry.data.media?.length` → `ImageResultPreview` |
| `MessageBubble.vue` | `entry.media` 优先于 `entry.images`（data URL 兼容） |
| `utils/showImage.js`（新） | `isShowImageTool(name)`，与 `readFilePreview.js` 并列 |
| `utils/media.js` | `fetchMediaBlob(sessionId, item)` + object URL |

参照已有 [`ReadFileResultPreview.vue`](../../node/webui/frontend/src/components/ReadFileResultPreview.vue) 模式：**工具特化预览**，而非改全局 transcript 形态。

### 6.2 Client 解析规则（正式）

| 条件 | UI 行为 |
|------|---------|
| `tool_result.media[]` 非空 | **优先** `ImageResultPreview`（任意工具） |
| `tool_name === "show_image"` 且 `arguments.path` | 同上（media 应已附带；无 media 时显示 path + 加载失败提示） |
| `tool_name === "browser_snapshot"` 等 + `media[]` | 自动展示，**无需** Agent 再调 `show_image` |
| hydrate `kind: user` + `media[]` | `MessageBubble` 展示 |

**禁止**生产环境依赖 UI 解析 `screenshot_path` JSON 或 `[READ_IMAGE]` 文本拼路径（仅 dev 过渡）。

### 6.3 交互（P1/P2）

- 点击缩略图：**lightbox** 全屏。  
- 右键 / 按钮：另存为、复制路径（`rel_path` 展示在 alt/title）。  
- 加载失败：占位 +「文件已删除或不可访问」。

---

## 7. 安全与配额

| 项 | 策略 |
|----|------|
| 路径 | `rel_path` 解析后必须在 `fs_root` 或 `.runtime/media/{session_id}/` 下 |
| 大小 | 单文件 ≤10MB（与 `readImageMaxBytes` 一致）；用户上传累计可配置 session 上限 |
| 类型 | 仅 `image/jpeg|png|gif|webp` |
| 跨 session | 禁止用 session A 的 id 读 session B 的 media |
| 清理 | session `Delete` 时删 `.runtime/media/{session_id}/`；引用 `fs_root` 内 browser 截图 **不删**（属 agent 产出，随 fs 策略） |

---

## 8. 功能清单与分期

| ID | 优先级 | 功能 | 层 |
|----|--------|------|-----|
| **F-M0** | P0 | **`show_image` 工具**（path + caption，policy auto） | Node tools |
| F-M1 | P0 | `MediaRegistry` + `GET …/media/{id}` | Node |
| F-M2 | P0 | `show_image` / `read_image` / `browser_*` 注册 + SSE `media[]` | Node tools + turn |
| F-M3 | P0 | `isShowImageTool` + `ImageResultPreview` + `ToolExecBubble` | Web UI |
| F-M4 | P1 | Hydrate transcript 含 `media`（依赖 F-H14） | Node + Web UI |
| F-M5 | P1 | 用户发图落盘 + Hydrate 回放 | Node + Web UI |
| F-M6 | P2 | `thumbnail=1` | Node |
| F-M7 | P2 | Lightbox / 下载 | Web UI |
| F-M8 | P2 | TUI 打印 path / URL（不渲染图） | Client |

### 版本归属

见 **[v0.6-v0.7-roadmap.md](./v0.6-v0.7-roadmap.md)**：

| 版本 | F-M* |
|------|------|
| **v0.6.1** | F-M0–M5, F-H10–H11 |
| **v0.7.0** | F-M6–M8 |

---

## 9. 方案对比（摘要）

| 方案 | 优点 | 缺点 | 结论 |
|------|------|------|------|
| **A. SSE 内联 base64** | 实现快 | Hub/SQLite/带宽爆炸 | ❌ |
| **B. UI 解析 `screenshot_path` 直连 fs API** | Node 改动小 | 无统一鉴权/注册；hydrate 难 | ❌ |
| **C. Session Media Artifact + show_image（本文）** | 显式工具 + 自动 media；统一 SSE + hydrate | 需 Registry + GET API | ✅ **推荐** |
| **D. 仅 Client 解析 path、无 API** | 看似简单 | 不安全；hydrate 不可用 | ❌ |
| **E. 仅 show_image、无自动 browser media** | Agent 完全可控 | 易漏截图展示 | ❌ 作子集非完整方案 |

---

## 10. 相关代码与文档

| 资源 | 路径 |
|------|------|
| read_image | `node/internal/tools/fs_read_image.go` |
| browser 截图 | `node/internal/browser/path.go`, `browser_vision.go` |
| SSE tool_result | `node/internal/turn/sse_publish.go` |
| Web 工具气泡 | `node/webui/frontend/src/components/ToolExecBubble.vue` |
| 用户多模态 | `node/webui/frontend/src/components/MainChatPanel.vue` |
| Hydrate 设计 | [windows-desktop-shell.md](./windows-desktop-shell.md) §3.9 |
| **v0.6–v0.7 总路线图** | [v0.6-v0.7-roadmap.md](./v0.6-v0.7-roadmap.md) |

---

## 11. 变更记录

| 日期 | 变更 |
|------|------|
| 2026-07 | 初稿：Session Media Artifact、API、SSE/hydrate、分期 F-M1–M8 |
| 2026-07 | 纳入 **`show_image` 工具**（F-M0）、双通道策略 P6；版本归属并入 v0.6-v0.7-roadmap |
