# app — Python 包参考

## `cli/`

见 [`cli/REFERENCE.md`](cli/REFERENCE.md)。

## `config/env.py`

| 符号 | 说明 |
|------|------|
| `resolve_runtime_root()` | 源码树或 PyInstaller 下的仓库/安装根目录 |
| `load_env(project_root)` | 加载 `{root}/.env`（不覆盖已有环境变量） |
