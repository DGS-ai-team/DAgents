# C2C 流式消息实现指引

本文档说明如何在当前 `botpy` 项目中实现 **C2C 私聊文本流式输出**（逐步更新同一条消息内容）。

> 适用场景：`examples/demo_c2c_reply_text.py` 这类私聊机器人回复。

---

## 1. 背景与限制

当前 `botpy` 已封装普通消息发送接口（如 `post_c2c_message`），但尚未提供 `stream_messages` 的高层封装。  
因此流式实现需要通过底层 HTTP 请求手动调用：

- 接口：`POST /v2/users/{openid}/stream_messages`
- 入口：`message._api._http.request(...)`
- 路由：`Route("POST", "/v2/users/{openid}/stream_messages", openid=...)`

---

## 2. 实现思路（核心流程）

流式发送分为 3 个阶段：

1. **启动流式会话**
   - 发送第一片，`input_state=1`
   - 不带 `stream_msg_id`
   - 平台会返回 `id`，后续作为 `stream_msg_id`

2. **持续更新内容**
   - 多次发送 `input_state=1`
   - `input_mode=replace`，每次发送“当前完整文本”
   - `index` 按 0,1,2... 递增

3. **结束流式会话**
   - 最后一片发送 `input_state=10`
   - 表示本次生成完成（DONE）

---

## 3. 请求字段说明

建议每片请求都携带这些字段：

- `input_mode`: `"replace"`
- `input_state`: `1`（生成中）或 `10`（结束）
- `content_type`: `"markdown"`
- `content_raw`: 当前阶段要展示的文本
- `event_id`: 触发事件 ID（可用 `message.id`）
- `msg_id`: 原消息 ID（可用 `message.id`）
- `msg_seq`: 同一会话固定值（demo 可固定 `1`）
- `index`: 当前分片序号（从 `0` 开始）
- `stream_msg_id`: 仅首片返回后，后续分片携带

---

## 4. 最小代码骨架

```python
from botpy.http import Route

async def send_c2c_stream(message, full_text: str):
    openid = message.author.user_openid
    stream_msg_id = None

    for idx in range(1, len(full_text) + 1):
        payload = {
            "input_mode": "replace",
            "input_state": 1,
            "content_type": "markdown",
            "content_raw": full_text[:idx],
            "event_id": message.id,
            "msg_id": message.id,
            "msg_seq": 1,
            "index": idx - 1,
        }
        if stream_msg_id:
            payload["stream_msg_id"] = stream_msg_id

        resp = await message._api._http.request(
            Route("POST", "/v2/users/{openid}/stream_messages", openid=openid),
            json=payload,
        )
        if not stream_msg_id and isinstance(resp, dict):
            stream_msg_id = resp.get("id")

    done_payload = {
        "input_mode": "replace",
        "input_state": 10,
        "content_type": "markdown",
        "content_raw": full_text,
        "event_id": message.id,
        "msg_id": message.id,
        "msg_seq": 1,
        "index": len(full_text),
    }
    if stream_msg_id:
        done_payload["stream_msg_id"] = stream_msg_id

    await message._api._http.request(
        Route("POST", "/v2/users/{openid}/stream_messages", openid=openid),
        json=done_payload,
    )
```

---

## 5. 异常回退建议（强烈推荐）

流式发送失败时，建议回退到普通 markdown 消息，保证用户至少能收到完整答复：

- 捕获异常后调用 `post_c2c_message(..., msg_type=2, markdown=...)`
- 记录日志便于排查（权限、参数、频控、网络问题）

---

## 6. 性能与体验调优

当前 demo 采用“逐字符分片”，视觉上最像打字机，但请求量最大。  
生产建议：

- 按 `5~20` 个字符一片（减少请求）
- 或按词、按句分片（阅读体验更自然）
- 控制最大总片数，避免长文本高频请求

---

## 7. 常见问题

- **Q: 为什么只有 C2C 可用？**  
  A: 当前流式接口是私聊场景接口，群/频道请使用普通发送。

- **Q: `stream_msg_id` 什么时候有？**  
  A: 第一片发送成功后从返回体 `id` 获取。

- **Q: `msg_seq` 要变化吗？**  
  A: 一个流式会话内建议固定同一个值（demo 固定 `1` 即可）。

- **Q: 为什么还要发 `input_state=10`？**  
  A: 这是结束标记，不发可能导致会话未正常收尾。

---

## 8. 参考实现

- 已落地示例：`examples/demo_c2c_reply_text.py`

