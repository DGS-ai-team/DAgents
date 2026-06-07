#!/usr/bin/env bash
# 在 **Ubuntu 20.04 (focal)** 容器内（**amd64** 用 `ubuntu:20.04`，**i386** 用 `i386/ubuntu:focal`）：用 pyenv 从源码编译 CPython，再执行 PyInstaller。
#
# 说明：Release CI 已改用 **`build_linux_rocky8_pyenv.sh`**（glibc 2.28）。本脚本保留供 i386 或需 focal 链时手动构建。
#
# 背景：
# 1. **amd64**：deadsnakes PPA 已不再为 focal 提供 `python3.13` 等套件（`Unable to locate package python3.13`），
#    因此在 **glibc 2.31** 工具链下仍需 3.13 时，只能与 i386 一样走 **pyenv + 官方源码**。
# 2. **i386**：deadsnakes 基本不提供 i386 deb；pyenv 在目标架构上编译最稳妥。
#
# 约定：
# - 工作区挂载为 /src（与 GitHub Actions `docker -v` 一致）；
# - **CLI_PI_ARGS**（必填之一）：Textual TUI 单文件参数；**API_PI_ARGS** / **RC_PI_ARGS** 可选（legacy backend）；
# - **PYENV_PYTHON_VERSION**：可选，默认 **3.13.2**。
#
# 副作用：首次编译 CPython 耗时较长，建议在 workflow 上为该 step 设置足够 **timeout**。

set -euxo pipefail

PYENV_PYTHON_VERSION="${PYENV_PYTHON_VERSION:-3.13.2}"
export DEBIAN_FRONTEND=noninteractive

apt-get update
apt-get install -y --no-install-recommends \
  ca-certificates curl git \
  make build-essential \
  libssl-dev zlib1g-dev libbz2-dev libreadline-dev libsqlite3-dev \
  libffi-dev liblzma-dev libncurses5-dev libncursesw5-dev \
  wget xz-utils tk-dev \
  patch pkg-config \
  libgdbm-dev

export PYENV_ROOT="${PYENV_ROOT:-/opt/pyenv}"
export PATH="${PYENV_ROOT}/bin:${PATH}"

if [ ! -x "${PYENV_ROOT}/bin/pyenv" ]; then
  rm -rf "${PYENV_ROOT}"
  git clone --depth 1 https://github.com/pyenv/pyenv.git "${PYENV_ROOT}"
fi

eval "$(pyenv init -)"

# --enable-shared：部分扩展模块与 PyInstaller 打包更稳妥；不加也可，可按需去掉以缩短编译时间。
export PYTHON_CONFIGURE_OPTS="${PYTHON_CONFIGURE_OPTS:---enable-shared}"

# -s：已安装同版本则跳过，便于后续若挂载工具缓存复用。
pyenv install -s "${PYENV_PYTHON_VERSION}"
pyenv global "${PYENV_PYTHON_VERSION}"

cd /src
python -m pip install --upgrade pip
python -m pip install -r requirements.txt pyinstaller

if [[ -z "${API_PI_ARGS:-}" && -z "${RC_PI_ARGS:-}" && -z "${CLI_PI_ARGS:-}" ]]; then
  echo "[build_linux_focal_pyenv] at least one of API_PI_ARGS, RC_PI_ARGS, CLI_PI_ARGS is required" >&2
  exit 1
fi
if [[ -n "${API_PI_ARGS:-}" ]]; then
  eval python -m PyInstaller ${API_PI_ARGS}
fi
if [[ -n "${RC_PI_ARGS:-}" ]]; then
  eval python -m PyInstaller ${RC_PI_ARGS}
fi
if [[ -n "${CLI_PI_ARGS:-}" ]]; then
  eval python -m PyInstaller ${CLI_PI_ARGS}
fi
