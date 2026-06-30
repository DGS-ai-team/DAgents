# `packaging/linux/`

Linux 分发辅助（随 `dagents-local-assistant-linux-amd64-*.tar.gz` 打入包根目录）。

| 文件 | 说明 |
|------|------|
| **`dagents`** | 命令行入口：`chat` / `tui` / `node`（默认后台）/ `node shutdown` / `node restart`；支持 `--withnode`；启动后 `cd` 到安装根（与 Windows `pushd` 一致，`fs_root: ./.runtime` 可从任意目录执行） |
| **`install.sh`** | 安装脚本：若目标目录已有 `.runtime/node.pid` 则先 `node shutdown` 停旧进程；**升级时**更新 `bin/`、`scripts/`、`dagents`，`.runtime/` **默认仅补缺失文件**；若已有 `policy/` 会**交互询问是否覆盖**（`--overwrite-policy` / `--keep-policy`） |

## 便携使用（不解压安装）

```bash
tar -xzf dagents-local-assistant-linux-amd64-*.tar.gz
cd dagents-local-assistant-linux-amd64
cp config.example.yaml config.yaml   # 编辑 llm / agent_id
./dagents node                       # 默认后台启动并等待就绪；日志 .runtime/logs/node.log
./dagents node shutdown              # 停止 Node
./dagents node restart               # 重启 Node
./dagents node --foreground          # 前台阻塞运行（调试）
./dagents chat --withnode            # Textual TUI（Node 未运行时会自动后台启动）
./dagents tui --withnode             # Go 全屏 TUI
```

## 安装到系统 / 用户目录

```bash
cd dagents-local-assistant-linux-amd64
./install.sh                         # ~/.local/share/dagents
sudo ./install.sh                    # /opt/dagents + /etc/profile.d/dagents.sh
./install.sh --prefix ~/dagents --bin-dir ~/bin
./install.sh --overwrite-policy        # 非交互：覆盖 .runtime/policy
./install.sh --keep-policy             # 非交互：保留已有 policy
sudo ./install.sh --uninstall        # 卸载 /opt/dagents
```

安装后在新 shell 中执行 `dagents doctor` 验证。`install.sh` 会将 **`${PREFIX}/.runtime/externaltools`** 加入 `PATH`，便于放置 Agent 可调用的独立工具（见 **[`../runtime/RECOMMENDED_CLI_TOOLS.md`](../runtime/RECOMMENDED_CLI_TOOLS.md)**）。
