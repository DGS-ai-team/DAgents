# manage

Node 向 Manage 控制面的出站集成：注册、A2A inbox、Release 检查、制品上传等。

| 文件 | 说明 |
|------|------|
| `registrar.go` | 向 Manage 注册/心跳/deregister |
| `a2a_profile.go` | `RegistrationCard`、启动校验与 A2A 配置警告 |
| `inbox_poller.go` | long poll inbox |
| `compliance_executor.go` | `role=compliance` inbox handler |
| `update_checker.go` | 周期查询 `/v1/releases/check` |
| `package_uploader.go` | 向 Manage 上传 skill/plugin/externaltool 包 |
| `task_replier.go` | ack / reply / caller_input |
