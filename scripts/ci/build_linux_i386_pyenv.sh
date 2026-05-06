#!/usr/bin/env bash
# 在 i386 Linux 容器（如 i386/ubuntu:focal）内：用 pyenv 从源码编译 32 位 CPython，再执行 PyInstaller。
#
# 背景：deadsnakes PPA 基本不提供 i386 deb；library/ubuntu 亦无可靠 linux/386 根镜像与新版 Python 的组合，
# 因此在 CI 中用 pyenv + 官方源码在目标架构上编译最稳妥。
#
# 约定：
# - 工作区挂载为 /src（与 GitHub Actions docker -v 一致）；
# - API_PI_ARGS / RC_PI_ARGS：传给 `python -m PyInstaller` 的完整参数串（与 workflow matrix 一致）；
# - PYENV_PYTHON_VERSION：可选，默认 3.13.2。
#
# 副作用：首次编译 CPython 耗时较长，建议在 workflow 上为该 step 设置足够 timeout。

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

eval python -m PyInstaller ${API_PI_ARGS}
eval python -m PyInstaller ${RC_PI_ARGS}
