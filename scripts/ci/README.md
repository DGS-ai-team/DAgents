# `scripts/ci/`

CI 专用脚本（本地亦可手动在同类容器内调试）。

| 文件 | 说明 |
|------|------|
| **`build_go_static.sh`** | Go `dagents-node` 静态交叉编译；`BUILD_CLIENT=1` 时额外编 `dagents-client`（probe/update） |
| **`build_go_linux_static.sh`** | 兼容入口（等同 `GOOS=linux`） |
| **`build_dagents_cli.sh`** | **已移除（Phase 4）**；原 PyInstaller `dagents-cli`（Textual TUI） |
| **`build_dagents_browser.sh`** | PyInstaller 单文件 **`dagents-browser`**（browser-use 薄服务） |
| **`build_linux_rocky8_pyenv.sh`** | **Release CI 默认**：Rocky Linux 8 容器（glibc **2.28**）内 pyenv + PyInstaller（`BROWSER_PI_ARGS`）；`SKIP_DNF=1` 配合预装镜像 |
| **`../packaging/ci/Dockerfile.rocky8-browser`** | Rocky8 + gcc-toolset-13 预装依赖（CI Buildx + GHA cache）；pyenv 目录另做 actions/cache |

**Release 打包**：仓库根 `scripts/package_local_assistant.sh`；CI 见 `.github/workflows/build-and-release.yml`：

- Linux 与 Windows（Go / Tauri Shell / browser）在单测后**并行**；
- Windows 三路产物合流后再 assemble + Inno；
- Manage 离线包**只依赖** `linux-amd64` 助手包（不再空等 Windows）。

**Go 测试**：`.github/workflows/go-ac.yml` 在仓库根执行 `go test ./node/... ./client/...`。

legacy **`packaging/linux/build-deb.sh`** / **`build-rpm.sh`**（Python backend 安装包）当前 Release **未使用**。
