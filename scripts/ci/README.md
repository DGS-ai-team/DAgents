# `scripts/ci/`

CI 专用脚本（本地亦可手动在同类容器内调试）。

| 文件 | 说明 |
|------|------|
| **`build_go_static.sh`** | Go `dagents-node` 静态交叉编译；`BUILD_CLIENT=1` 时额外编 `dagents-client`（probe/update） |
| **`build_go_linux_static.sh`** | 兼容入口（等同 `GOOS=linux`） |
| **`build_dagents_cli.sh`** | **已移除（Phase 4）**；原 PyInstaller `dagents-cli`（Textual TUI） |
| **`build_dagents_browser.sh`** | PyInstaller 单文件 **`dagents-browser`**（browser-use 薄服务） |
| **`build_linux_rocky8_pyenv.sh`** | **Release CI 默认**：Rocky Linux 8 容器（glibc **2.28**）内 pyenv + PyInstaller（`BROWSER_PI_ARGS`） |
| **`build_linux_focal_pyenv.sh`** | Ubuntu 20.04 focal 容器（glibc 2.31；i386 或需较新链时手动用） |
| **`assemble_local_assistant_bundle.sh`** | 组装 `dagents-local-assistant-*` 目录并 tar.gz/zip |
| **`build_windows_installer.sh`** | Windows：staging `bundle/` + Inno Setup 生成 `.exe` 安装包 |
| **`build_windows_setup_bootstrapper.sh`** | Windows：在 Inno 之上打 Tauri Setup 向导（`dagents-setup-windows-amd64-*.exe`，与 Inno 并存） |
| **`build_manage_docker.sh`** | Manage Docker 镜像构建与 tar.gz 导出（本地/CI 中间产物，不单独进 Release） |
| **`assemble_manage_bundle.sh`** | 组装 **`dagents-manage-bundle-*`** 离线包（镜像 + compose + 导入/重启脚本；Release 附件） |

**Release 打包**：仓库根 `scripts/package_local_assistant.sh`；CI 见 `.github/workflows/build-and-release.yml`（`dagents-local-assistant-linux-amd64` + `windows-amd64`）。

**Go 测试**：`.github/workflows/go-ac.yml` 在仓库根执行 `go test ./node/... ./client/...`。

legacy **`packaging/linux/build-deb.sh`** / **`build-rpm.sh`**（Python backend 安装包）当前 Release **未使用**。
