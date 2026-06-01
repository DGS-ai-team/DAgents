# `scripts/windows/`

Windows 运维脚本。

| 文件 | 说明 |
|------|------|
| `install_node_service.cmd` | 将 **dagents-node** 注册为 **SYSTEM 开机计划任务**（`DAgents\AgentNode`） |

Node 未实现 Windows SCM，故非 Services.msc 服务。用法见 [`../README.md`](../README.md)。
