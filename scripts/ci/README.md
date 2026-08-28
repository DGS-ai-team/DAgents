# `scripts/ci/`

CI 专用脚本（本地亦可手动在同类容器内调试）。

| 文件 | 说明 |
|------|------|
| **`build_go_static.sh`** | Go `dagents-node` 静态交叉编译；`BUILD_CLIENT=1` 时额外编 `dagents-client`（probe/update） |
| **`run_staticcheck.sh`** | 使用固定版本的 Go Staticcheck 检查全部 Go module |
| **`build_go_linux_static.sh`** | 兼容入口（等同 `GOOS=linux`） |
| **`build_dagents_browser.sh`** | PyInstaller 单文件 **`dagents-browser`**（browser-use 薄服务） |
| **`build_linux_rocky8_pyenv.sh`** | **Release CI 默认**：Rocky Linux 8 容器（glibc **2.28**）内 pyenv + PyInstaller（`BROWSER_PI_ARGS`）；`SKIP_DNF=1` 配合预装镜像 |
| **`../packaging/ci/Dockerfile.rocky8-browser`** | Rocky8 + gcc-toolset-13 预装依赖（CI Buildx + GHA cache）；pyenv 目录另做 actions/cache |
| **`../packaging/ci/Dockerfile.tauri-linux`** | Ubuntu 24.04 + Rust stable + Tauri Linux 系统依赖；发布到 GHCR 后供 `tauri-shell.yml` 直接复用 |
| **`build_linux_focal_pyenv.sh`** | Ubuntu 20.04 focal 容器（glibc 2.31；i386 或需较新链时手动用） |
| **`assemble_local_assistant_bundle.sh`** | 组装 `dagents-local-assistant-*` 目录并 tar.gz/zip |
| **`build_windows_installer.sh`** | Windows：staging `bundle/` + Inno Setup 生成 `.exe` 安装包 |
| **`build_manage_docker.sh`** | Manage Docker 镜像构建与 tar.gz 导出（本地/CI 中间产物，不单独进 Release） |
| **`assemble_manage_bundle.sh`** | 组装 **`dagents-manage-bundle-*`** 离线包（镜像 + compose + 导入/重启脚本；Release 附件） |
| **`build_dagents_shell_tauri.sh`** / **`build_dagents_shell.sh`** | Windows Shell（Tauri 推荐 + Go legacy） |

**Release 打包**：仓库根 `scripts/package_local_assistant.sh`；CI 见 `.github/workflows/build-and-release.yml`：

- Linux 与 Windows（Go / Tauri Shell / browser）在单测后**并行**；
- Tauri Linux 检查优先使用 GHCR 预构建镜像，不在正常 CI 中重复安装 apt 依赖；镜像由 `tauri-linux-image.yml` 在 `dev/main` 发布并通过 BuildKit GHA cache 加速构建。镜像首次发布前仅允许一次 bootstrap apt 兜底。
- Windows 三路产物合流后再 assemble + Inno；
- Manage 离线包**只依赖** `linux-amd64` 助手包（不再空等 Windows）。

**统一本地门禁**：仓库根运行 `scripts/verify.sh`；Windows 运行 `scripts/verify.ps1`。CI 的 Go 门禁覆盖全部模块，并同时执行 `gofmt`、`go vet`、单测和构建。

Python 运行时依赖使用根目录 `requirements.lock`；浏览器打包依赖使用 `browser-service/requirements.lock`。源文件格式由 `.editorconfig` 和 `.gitattributes` 统一。
