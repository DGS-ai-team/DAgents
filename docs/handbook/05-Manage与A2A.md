# 05 · Manage 与协作面

## 本章回答什么问题

> **2026-08**：A2A **inbox / `agent_invoke` / Console Inbox** 已拆除。跨机器协作请使用 **工作组（Workgroup）**。本章保留 Registry / Console / 制品等现网能力；旧 A2A Task 叙述已归档到 [docs/future/a2a-via-manage.md](../future/a2a-via-manage.md)（历史）。

读完本章，你应能：

- 说明 Manage 与 Node、Client 的连接关系  
- 跟读 Node 注册与心跳  
- 理解工作组作为跨 Node 协作主路径  
- 使用 Manage Console 做目录与制品运维  

---

## 1. Manage 角色

```text
┌─────────────┐         ┌────────────────────────────┐         ┌─────────────┐
│   Client    │  本地   │      Manage (:8020)         │  出站   │ Agent Node  │
│  / Web UI   │────────►│ Registry · Workgroup · UI   │◄────────│   (Go)      │
└──────▲──────┘         └────────────────────────────┘         └──────▲──────┘
       └────────────────── Client 只连 Node ──────────────────────────┘
```

- **产品 Web UI**：只连本机 Node；工作组 API 经 Node 反代 Manage  
- **Manage Console**：浏览器直连 Manage（目录 / 案例 / 制品）  
- **跨 Node**：工作组 Dialer（WS）+ Timeline；**不是** A2A inbox  

其余 Registry / 制品 / 案例细节见 [manage/README.md](../../manage/README.md) 与 [workgroup-and-node-gateway.md](../design/workgroup-and-node-gateway.md)。
