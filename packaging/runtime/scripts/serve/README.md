# `serve` startup / shutdown hooks

`dagents serve` 在后台启动/停止 Agent API 时会按文件名顺序执行本目录下的钩子（可用 `--no-hooks` 跳过）。

| 目录 | 时机 |
|------|------|
| `startup.d/` | 后台进程 `Popen` 之前 |
| `shutdown.d/` | `dagents serve --stop` 在进程退出之后 |

支持脚本后缀：Linux/macOS 为 `.sh`；Windows 为 `.bat` / `.cmd`（`.sh` 需系统有 `bash`）。

工作目录为安装根目录（与 `dagents-api.exe`、` .env` 同级），可直接访问 `.runtime/`。

示例：

```bash
dagents serve              # 后台启动 + startup.d
dagents serve --status     # 查看是否在跑
dagents serve --stop       # 停止 + shutdown.d
dagents serve --foreground # 前台调试，不写 PID 文件
```
