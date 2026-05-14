# `scripts/ci/`

CI 专用脚本（本地亦可手动在同类容器内调试）。

| 文件 | 说明 |
|------|------|
| **`build_linux_focal_pyenv.sh`** | 在 **Ubuntu 20.04 (focal)** 容器内（**amd64**：`ubuntu:20.04`；**i386**：`i386/ubuntu:focal`）通过 **pyenv** 从源码安装 **`PYENV_PYTHON_VERSION`**（默认 **3.13.2**），再运行 PyInstaller。deadsnakes 已不再为 focal 提供 **3.13** deb，故 linux-x64 与 linux-x86 共用本脚本。 |

各平台 **Python 3.13 依赖离线包**（`pip download` + zip）见 GitHub Actions 工作流 **`manual-vendor-python-deps.yml`**（手动运行）。
