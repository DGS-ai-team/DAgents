# `packaging/linux/`

Linux 分发辅助（随 `dagents-local-assistant-linux-amd64-*.tar.gz` 打入包根目录）。

| 文件 | 说明 |
|------|------|
| **`dagents`** | 命令行入口（对齐 `packaging/windows/dagents.cmd`）：`chat` / `tui` / `node` / `register-center`；支持 `--withnode`、`node --background` |
| **`install.sh`** | 安装脚本：拷贝到固定目录、创建 `dagents` 符号链接、配置 `DAGENTS_HOME` 与 `PATH` |

## 便携使用（不解压安装）

```bash
tar -xzf dagents-local-assistant-linux-amd64-*.tar.gz
cd dagents-local-assistant-linux-amd64
cp config.example.yaml config.yaml   # 编辑 llm / agent_id
./dagents node --background          # 推荐；日志 .runtime/logs/node.log
./dagents chat --withnode            # Textual TUI（Node 未运行时会自动后台启动）
./dagents tui --withnode             # Go 全屏 TUI
```

## 安装到系统 / 用户目录

```bash
cd dagents-local-assistant-linux-amd64
./install.sh                         # ~/.local/share/dagents
sudo ./install.sh                    # /opt/dagents + /etc/profile.d/dagents.sh
./install.sh --prefix ~/dagents --bin-dir ~/bin
./install.sh --uninstall             # 卸载用户级默认路径
sudo ./install.sh --uninstall        # 卸载 /opt/dagents
```

安装后在新 shell 中执行 `dagents doctor` 验证。`install.sh` 会将 **`${PREFIX}/.runtime/scripts`** 加入 `PATH`，便于后续集成 Agent 可调用的独立工具（当前 Linux 包默认不含 OfficeCLI，该目录主要为占位与扩展用）。
