# `scripts/ci/`

CI 专用脚本（本地亦可手动在同类容器内调试）。

| 文件 | 说明 |
|------|------|
| **`build_linux_i386_pyenv.sh`** | 在 **i386** Ubuntu 容器内通过 **pyenv** 编译安装指定版本 CPython，再运行 PyInstaller（供 `linux-x86` 矩阵使用） |
