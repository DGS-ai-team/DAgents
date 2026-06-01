# Go Agent Node / Client 兼容性与构建矩阵（N7）

本文定义 **Phase AC** 发布物的目标平台、构建方式与验收方法。Python PyInstaller 矩阵见 [os-compatibility.md](./os-compatibility.md)。

---

## 1. 目标平台

| 环境 | 典型 glibc | Node + Client 目标 | 备注 |
|------|------------|-------------------|------|
| **RHEL 6 / CentOS 6.9** | 2.12 | **需真机验收** | 无 systemd；用 SysV init；Client 用 `tui --plain` 兜底 |
| **RHEL 7 / CentOS 7** | 2.17 | 支持 | systemd 服务脚本 |
| **RHEL 8+ / Ubuntu 20.04+** | ≥2.28 | 支持 | 默认 `tui` 全屏 |
| **Windows Server 2012 R2** | — | Client 交叉编译 **amd64** | conhost ANSI 弱，优先 plain 或 Windows Terminal |
| **WSL2 / 现代 Linux 桌面** | 新 | 全功能 | Textual 或 Go full TUI |

**原则**：Go 产物使用 **`CGO_ENABLED=0`** 静态链接（`modernc.org/sqlite` 无 CGO），降低对目标机 glibc 的 **动态**依赖；仍须在目标内核上实测（Go 1.22+ 对极老内核可能有限制）。

---

## 2. 构建命令

```bash
scripts/package_go_agent_client.sh
# → dist/dagents-agent-client-linux-amd64-${VERSION}.tar.gz
# → dist/dagents-agent-client-windows-amd64-${VERSION}.zip
```

单平台：

```bash
OUT_DIR=dist/pkg GOOS=windows GOARCH=amd64 scripts/ci/build_go_static.sh
```

产物：

```text
dist/dagents-agent-client-linux-amd64/
  bin/dagents-node
  bin/dagents-client
  config.example.yaml
  README.txt
```

Windows（Server 2012+ / Windows Terminal）：

```bat
bin\dagents-node.exe -config config.yaml
bin\dagents-client.exe -config config.yaml tui --plain
```

---

## 3. 验收清单（每台目标机）

1. `ldd --version` 记录 glibc。
2. 解压 tarball，`cp config.example.yaml config.yaml` 并编辑 LLM。
3. `./bin/dagents-node -config config.yaml` → `curl …/health` 返回 200。
4. `./bin/dagents-client probe -config config.yaml` → `ok`.
5. **SSH 交互**：`./bin/dagents-client tui`（全屏）；若乱码则 `tui --plain`。
6. 触发器：配置 schedule trigger，确认无 Client 也能 fire。
7. **重启 Node** 后 session 历史仍在。

---

## 4. RHEL 6 部署注意

| 项 | 做法 |
|----|------|
| 服务托管 | `scripts/linux/install_node_service_sysv.sh`（SysV init，非 systemd） |
| Python Textual | **不要**在 6.9 上部署；用 Go Client |
| TERM | SSH 客户端建议 `xterm-256color`；tmux 内设 `screen-256color` |
| 防火墙 | Node 默认 `127.0.0.1`，仅本机 Client |

---

## 5. 已知限制

- **Windows Go 二进制**已纳入 `package_go_agent_client.sh` 与 Release CI（zip）。
- **Go 1.25** 与 **RHEL 6 内核 2.6.32**：若二进制无法启动，需在 6.9 真机记录并考虑降 GO 版本重编。

---

## 6. 相关文档

- [client-packaging.md](./client-packaging.md)
- [local-assistant.md](./local-assistant.md)
- [agent-client-refactor-plan.md](../design/agent-client-refactor-plan.md) § N7
