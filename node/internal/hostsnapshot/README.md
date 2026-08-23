# node/internal/hostsnapshot

进程级主机环境快照，Node 启动时采集，通过请求级 ContextInjection 注入模型可见的「当前运行环境」段。

| 文件 | 说明 |
|------|------|
| `hostsnapshot.go` | `Snapshot`、`CaptureAtStartup`、`Get`、`FormatEnvironmentSection` |
