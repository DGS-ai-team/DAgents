#!/usr/bin/env bash
# 在 **Ubuntu 20.04 (focal)** amd64 容器内安装 Python 3.13（deadsnakes）并执行 PyInstaller。
#
# 背景：在 GitHub `ubuntu-22.04` 宿主编译出的 onefile 常绑定 **较新 glibc**（如 GLIBC_2.34+），
# 在旧发行版（glibc < 2.30）上会报 libpthread / libpython 版本不满足。
# 在 focal（glibc 2.31）工具链下打包，可向下兼容到 **约 Ubuntu 20.04 / glibc 2.31** 一类环境；
# 若宿主仍低于该版本，只能升级系统或使用源码运行，而非更换单个 .so。
#
# 约定：仓库挂载为 /src；API_PI_ARGS / RC_PI_ARGS 与 workflow matrix 一致。

set -euxo pipefail

export DEBIAN_FRONTEND=noninteractive

apt-get update
apt-get install -y --no-install-recommends \
  ca-certificates curl gnupg software-properties-common \
  build-essential zlib1g-dev libffi-dev libssl-dev \
  libbz2-dev libreadline-dev libsqlite3-dev liblzma-dev

add-apt-repository -y ppa:deadsnakes/ppa
apt-get update
apt-get install -y --no-install-recommends \
  python3.13 python3.13-dev python3.13-venv

python3.13 -m ensurepip --upgrade || \
  curl -sS https://bootstrap.pypa.io/get-pip.py | python3.13

cd /src
python3.13 -m pip install --upgrade pip
python3.13 -m pip install -r requirements.txt pyinstaller

eval python3.13 -m PyInstaller ${API_PI_ARGS}
eval python3.13 -m PyInstaller ${RC_PI_ARGS}
