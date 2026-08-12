# 05 · Manage 与协作面

## 本章回答什么问题

跨机器协作主路径是 **Workgroup**。用户向操作说明：[07-Workgroup协作](./07-Workgroup协作.md)。产品规范：[workgroup-and-node-gateway.md](../design/workgroup-and-node-gateway.md)。

读完本章，你应能：

- 说明 Manage 与 Node、Web UI、Console 的连接关系  
- 跟读 Node 注册与心跳  
- 把工作组当作跨 Node 协作入口  
- 使用 Manage Console 做目录与制品运维  

用户向操作说明：[07-Workgroup协作](./07-Workgroup协作.md)。产品规范：[workgroup-and-node-gateway.md](../design/workgroup-and-node-gateway.md)。

---

## 1. Manage 角色

```text
┌──────────────┐  本机 HTTP   ┌────────────────────────────┐
│  Web UI /ui/ │─────────────►│      Agent Node             │
└──────────────┘              │  /v1/workgroups → 反代      │──出站──►┌──────────────────┐
                              └────────────────────────────┘         │ Manage (:8020)   │
┌──────────────┐  浏览器直连                                          │ Registry         │
│ Console      │─────────────────────────────────────────────────────►│ Workgroup Leader │
│ /console/    │                                                      │ 制品 / 案例      │
└──────────────┘                                                      └──────────────────┘
```

| 入口 | 连谁 | 用途 |
|------|------|------|
| **Node Web UI** | 本机 Node | 对话、设置、工作组聊天（API 经 Node 反代） |
| **Manage Console** | Manage | Registry、工作组管理、制品、案例 |
| **Node Dialer** | Manage WS | 拉 outbox、回传 tool.result / provision |

- **禁止** Node↔Node 直连驱动成员（工作组命令经 Manage）。  
- **禁止** 把工作组成员当成 `/v1/agents` 本地会话打开。

---

## 2. 注册与心跳

Node 在 `manage.enabled` 时向 Manage Registry 登记：

- 身份：**`node_id`**（不是单个本地 Agent id）  
- 心跳：在线状态、版本、可选 `local_agents` 公告  
- 首配未完成时：**不**启动 Registrar / Workgroup Dialer  

配置见 [附录/配置项参考](./附录/配置项参考.md) 与 Web UI「设置 › Manage」。

---

## 3. 工作组（主路径）

| 能力 | 说明 |
|------|------|
| 建组 / ACL / 订阅 | Console 或 Node UI |
| Member provision | Home Node 工作区 + tool manifest |
| Supervisor 编排 | `assign_workgroup_task` |
| `@member` 直达 | 跳过编排 |
| HITL / 取消 | 信息型询问；turn cancel + `tool.cancel` |
| 工具目录 | `GET /v1/workgroups/meta/member-tools`（同源 `shared/workgroup/member_tool_catalog.json`；默认仅 fs） |

契约与 fixtures：`docs/design/workgroup-d05-contracts.md`。  
预览验收：`docs/design/v0.9.1-smoke-checklist.md`。

实现目录：

| 侧 | 路径 |
|----|------|
| Manage | `manage/workgroup/` |
| Node Worker / Dialer | `node/internal/workgroup/` |
| Node HTTP 反代 | `node/internal/api/workgroup_api.go` |

---

## 4. Console 与制品

- Console：`http://127.0.0.1:8020/console/`（需先 `npm run build --prefix manage/console/frontend`）  
- Registry / Releases / Skills / Plugins：见 [manage/README.md](../../manage/README.md)  
- Release Hub：`docs/design/release-update-hub.md`

---

## 5. 下一章

→ [06-运维与案例](./06-运维与案例.md)  
→ [07-Workgroup协作](./07-Workgroup协作.md)（若尚未阅读）
