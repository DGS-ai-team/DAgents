# Go Agent Node 构建矩阵（归档）

> **现行人机入口是 Web UI**，构建与部署请以手册 [06-运维与案例](../handbook/06-运维与案例.md) 与根 README 为准。下文保留早期 Client/TUI 构建矩阵，仅供查历史脚本，不作为产品说明。

本文原定义 Phase AC 发布物的目标平台、构建方式与验收方法。

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

**本地助手（Go Node + Textual TUI，Release 默认）：**

```bash
scripts/package_local_assistant.sh
# → dist/dagents-local-assistant-linux-amd64-${VERSION}.tar.gz   （在 Linux 上执行）
# → dist/dagents-local-assistant-windows-amd64-${VERSION}.zip  （在 Windows 上执行）
```

CI 在 `v*` 标签推送时并行构建 linux-amd64 与 windows-amd64。

仅 Go Node（无 TUI 二进制）：

```bash
BUILD_CLIENT=0 OUT_DIR=dist/pkg GOOS=linux GOARCH=amd64 scripts/ci/build_go_static.sh
```

含 Go Client 兜底 TUI（`BUILD_CLIENT=1`）：

```bash
BUILD_CLIENT=1 OUT_DIR=dist/pkg GOOS=linux GOARCH=amd64 scripts/ci/build_go_static.sh
```

**Release 包布局：**

```text
dist/dagents-local-assistant-linux-amd64/
  bin/dagents-node
  bin/dagents-client       # Go TUI：默认 bubbletea 全屏（tui）；--plain 为行模式 REPL
  bin/dagents-cli          # PyInstaller Textual TUI
  config.example.yaml
  .runtime/
  README.txt
```

legacy Go-only 包（`dagents-agent-client-*`）已由上述产物取代。

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

**Docker 特性导览**：[`cases/centos7-feature-tour/`](../../cases/centos7-feature-tour/)（CentOS 7 + `./scripts/verify.sh`）。RHEL 6 真机验收见 [releases/rhel6-acceptance-checklist.md](releases/rhel6-acceptance-checklist.md)。

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

- 当前构建、测试和发布：[docs/development.md](../development.md)
- 当前用户入口：[docs/user/operations.md](../user/operations.md)
