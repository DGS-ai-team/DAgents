# `packaging/linux/`

Linux 分发辅助（随 `dagents-local-assistant-linux-amd64-*.tar.gz` 打入包根目录）。

| 文件 | 说明 |
|------|------|
| **`dagents`** | 命令行入口（对齐 `packaging/windows/dagents.cmd`）：`chat` / `tui` / `node` / `register-center` |
| **`install.sh`** | 安装脚本：拷贝到固定目录、创建 `dagents` 符号链接、配置 `DAGENTS_HOME` 与 `PATH` |

## 便携使用（不解压安装）

```bash
tar -xzf dagents-local-assistant-linux-amd64-*.tar.gz
cd dagents-local-assistant-linux-amd64
cp config.example.yaml config.yaml   # 编辑 llm / agent_id
./dagents node                       # 或 ./scripts/startup/linux/start-node.sh
./dagents chat                       # Textual TUI
./dagents tui                        # Go 全屏 TUI
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

安装后在新 shell 中执行 `dagents doctor` 验证。
