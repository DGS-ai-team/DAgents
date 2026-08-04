# manage

Node 向 Manage 控制面的出站集成：注册、Release 检查、制品上传、工作组客户端等。

| 文件 | 说明 |
|------|------|
| `registrar.go` | 向 Manage 注册/心跳/deregister |
| `a2a_profile.go` | `RegistrationCard`；提示 A2A inbox 已退役 |
| `control_client.go` / `workgroup_client.go` | Control / 工作组 HTTP |
| `update_checker.go` | 周期查询 `/v1/releases/check` |
| `package_uploader.go` | 向 Manage 上传 skill/plugin/externaltool 包 |

**已删除（2026-08）**：`inbox_poller.go` / `compliance_executor.go` / `task_replier.go`（A2A inbox callee）。跨机协作请用工作组 Dialer。
