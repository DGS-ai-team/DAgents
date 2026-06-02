# `scripts/ci/`

CI 专用脚本（本地亦可手动在同类容器内调试）。

| 文件 | 说明 |
|------|------|
| **`build_go_static.sh`** | Go `dagents-node` 静态交叉编译；`BUILD_CLIENT=1` 时额外编 `dagents-client` |
| **`build_go_linux_static.sh`** | 兼容入口（等同 `GOOS=linux`） |
| **`build_dagents_cli.sh`** | PyInstaller 单文件 **`dagents-cli`**（Textual TUI） |
| **`build_linux_rocky8_pyenv.sh`** | **Release CI 默认**：Rocky Linux 8 容器（glibc **2.28**）内 pyenv + PyInstaller |
| **`build_linux_focal_pyenv.sh`** | Ubuntu 20.04 focal 容器（glibc 2.31；i386 或需较新链时手动用） |
| **`assemble_local_assistant_bundle.sh`** | 组装 `dagents-local-assistant-*` 目录并 tar.gz/zip |
| **`export_openapi_for_frontend.py`** | 导出 OpenAPI 并同步 DAgentsUI 类型 |

**Release 打包**：仓库根 `scripts/package_local_assistant.sh`；CI 见 `.github/workflows/build-and-release.yml`（`dagents-local-assistant-linux-amd64` + `windows-amd64`）。

**Go 测试**：`.github/workflows/go-ac.yml` 在仓库根执行 `go test ./node/... ./client/...`。

legacy **`packaging/linux/build-deb.sh`** / **`build-rpm.sh`**（Python backend 安装包）当前 Release **未使用**。
