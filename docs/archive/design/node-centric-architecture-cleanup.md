# Node 中心架构：过时设计清理清单（归档）

> 已归档。一次性清理清单；多数项已完成。现行架构见 handbook 与 [workgroup-and-node-gateway.md](../../design/workgroup-and-node-gateway.md)。

> 分支：`cursor/remote-agent-placement-7e3e`  
> 背景：Registry / Placement / 多 Agent 实例已按 **Node** 运作；进程级 `ops`/`compliance` 角色模型不再适合。

## 已落地（本切片）

| 项 | 旧 | 新 |
|----|----|----|
| A2A 是否可被调 | `agent.role=compliance` → expose | **`manage.a2a.accept_inbound`** Node 开关 |
| Inbox 默认 | 跟 compliance | 跟 `accept_inbound`；可被 `a2a.enabled` 覆盖 |
| Inbox handler | 仅 compliance 角色挂载 | **inbox 开启即挂载**（不再看 role） |
| 可否被远端创建 Agent | 误绑 expose / 写死 true | **`placement.allow_peer_create`** Node 开关 |
| 屏幕旁观许可 | 写死 true | **`placement.allow_screen_view`** |
| Node 命名 | UI 叫「Agent 信息 / 角色」 | UI：**Node 名称**（`agent.name` → Manage `name`） |

Web UI：通用设置 → Node 信息 + 远端 Placement；连接设置 → 接受 A2A 入站。

---

## 仍过时、建议后续处理

### P0 语义 / 文档

1. **handbook / manage-communication** 仍写「一端口一 agent_id」「role=compliance 被调」——与 Node + 多实例冲突，需改写。  
2. **`agent.role` 字段**仍存于 config / card.metadata：仅作可选标签，应在文档标 deprecated，最终删除 UI 与种子默认 `ops`。  
3. **模板 `defaults.agent.role`**（assistant/reviewer）与进程旧 role **同词不同义**——建议模板改为 `persona` / `profile`。

### P1 命名与 API

4. **Config 块仍叫 `agent:`**，语义是 Node 名片 → 迁移为 `node:`（`name`/`description`）并做一次读取兼容。  
5. **HTTP Header `x-dagents-agent-id`** 实际传 `node_id` → 增加 `x-dagents-node-id`，旧头兼容一期。  
6. **Registry 表主键列名 `agent_id`**、Console「Agent」列表 → 对外改称 Node（P5 已双写 `node_id`，展示层未跟完）。  
7. **`ComplianceExecutor` 命名** → 改为通用 `InboxExecutor` / `InboundTurnRunner`。

### P2 模型演进

8. **跨机器** → **仅工作组**（[workgroup-and-node-gateway.md](./workgroup-and-node-gateway.md)）；**拆除** Placement / 远程 Agent / Edge。  
9. **权限** → ACL 以 **`node_id`** 为单位；解散 **归档**。  
10. **产品内沙箱** → 废弃；隔离 = 独立 Node。  
11. **InboxPoller / Node 级 invoke / ops·compliance** → 删除。  
12. **`discovery_group`** → 不再支撑 Placement；可降级或删除产品依赖。

### 建议保留

- Manage 分配 `discovery_group`（Node 不自报）  
- Placement peers **不依赖** A2A `expose_to_peers`  
- Registry 双写 `node_id` + 兼容 `agent_id`  
- 实例 `display_name` 与 Node 展示名分离

---

## 配置对照（Node）

```yaml
agent:
  name: "办公楼 Windows 工位"   # Node 展示名
  description: "..."

placement:
  allow_peer_create: true      # 允许同组在本机创建 Agent
  allow_screen_view: true      # 允许旁观（无 GUI 仍可能 unavailable）

manage:
  enabled: true
  url: http://127.0.0.1:8020
  a2a:
    accept_inbound: false      # A2A 被发现/被调；默认 false
    enabled: null              # nil 则跟随 accept_inbound
```

**不要再**用 `agent.role: compliance|ops` 控制上述能力。
