#!/usr/bin/env bash
# 在 **Rocky Linux 8**（glibc **2.28**）容器内用 **gcc-toolset-13** + **pyenv** 源码编译 CPython，再执行 PyInstaller。
#
# 目的：
# 1. 默认 Linux x64 包在 **Ubuntu 20.04 (focal)** 上构建，产物往往依赖 **GLIBC_2.30+**（如 **libpthread**），在 **glibc 更旧** 但仍需运行 3.13 的主机（常见 **RHEL 8 / Rocky 8 / Alma 8**，glibc 2.28）上会启动失败。
# 2. 本脚本在 **glibc 2.28** 工具链上链接，降低对宿主 glibc 的上限要求（仍 **不低于 2.28**）。
#
# 硬边界：
# - **RHEL 6 / CentOS 6（glibc 2.12）无法运行 CPython 3.13**；若必须支持，只能换更低版本 Python 或容器化部署，勿期望本仓库 PyInstaller 产物兼容。
#
# 约定（与 **`build_linux_focal_pyenv.sh`** 一致）：
# - 工作区挂载为 **/src**；
# - **API_PI_ARGS** / **RC_PI_ARGS**：传给 **`python -m PyInstaller`** 的完整参数串；
# - **PYENV_PYTHON_VERSION**：可选，默认 **3.13.2**。
#
# 副作用：首次编译 CPython 耗时长；workflow step 需足够 **timeout**。

set -euxo pipefail

PYENV_PYTHON_VERSION="${PYENV_PYTHON_VERSION:-3.13.2}"

dnf -y install \
  ca-certificates curl git \
  make patch pkg-config which \
  gcc zlib-devel bzip2-devel readline-devel sqlite-devel openssl-devel xz xz-devel \
  libffi-devel gdbm-devel \
  gcc-toolset-13 gcc-toolset-13-gcc gcc-toolset-13-gcc-c++

# Rocky 8 上启用较新 GCC，满足 CPython 3.13 源码构建要求；运行时仍只依赖系统 glibc 2.28。
# shellcheck source=/dev/null
source /opt/rh/gcc-toolset-13/enable

export PYENV_ROOT="${PYENV_ROOT:-/opt/pyenv}"
export PATH="${PYENV_ROOT}/bin:${PATH}"

if [ ! -x "${PYENV_ROOT}/bin/pyenv" ]; then
  rm -rf "${PYENV_ROOT}"
  git clone --depth 1 https://github.com/pyenv/pyenv.git "${PYENV_ROOT}"
fi

eval "$(pyenv init -)"

export PYTHON_CONFIGURE_OPTS="${PYTHON_CONFIGURE_OPTS:---enable-shared}"

pyenv install -s "${PYENV_PYTHON_VERSION}"
pyenv global "${PYENV_PYTHON_VERSION}"

cd /src
python -m pip install --upgrade pip
python -m pip install -r requirements.txt pyinstaller

eval python -m PyInstaller ${API_PI_ARGS}
eval python -m PyInstaller ${RC_PI_ARGS}
