# 离线安装说明（可选：与 `wheels/` 一并拷贝到目标环境）

本说明适用于：**已将项目源码与预先下载的依赖包目录 `wheels/`** 一并拷贝到 **无法访问 PyPI** 的机器时使用。

`wheels/` 须在 **可联网** 且环境尽量与目标机一致的前提下生成：

- **Python**：次版本需一致（例如目标用 3.13，下载时也应用 3.13）。
- **操作系统 / CPU 架构**：须一致或为兼容组合（例如在 Linux x86_64 上下载的 wheel 不能直接用于 Windows）。

## 在联网机上准备 `wheels/`（示例）

```bash
cd /path/to/DAgents   # 本仓库根目录
python3.13 -m venv .venv
source .venv/bin/activate   # Windows: .venv\Scripts\activate
pip install --upgrade pip wheel
mkdir -p wheels
pip download -r requirements.lock -d wheels
```

可将 `packaging/OFFLINE_INSTALL.md` 复制到发布目录，便于内网用户查阅。

## 在离线机上安装并启动

```bash
cd /path/to/DAgents
python3.13 -m venv .venv
source .venv/bin/activate
pip install --no-index --find-links=wheels -r requirements.lock
cp .env.example .env
python run_agent_api.py
```

Manage 控制面：`python3 run_manage.py`（或 Docker，见 `packaging/manage/README.md`）。

## 使用其它 Python 版本

不能用为 3.13 下载的 wheel 去装 3.12 环境。请换用目标解释器在联网机 **重新** 执行 `pip download` 生成新的 `wheels/`，再整包拷贝。

## 校验

```bash
pip check
```
