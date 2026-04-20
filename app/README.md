# `app/` 应用包

Python **命名空间包**（无 `__init__.py`，PEP 420），从仓库根将根目录加入 `sys.path` 后即可 `import app.*`。

| 子目录 | 职责 |
|--------|------|
| **`config/`** | 配置：**`Settings`**、**`get_settings()`**；**`load_env`**（`.env`） |
| **`context/`** | 会话上下文模型：**`MessageRecord`**、**`ConversationContext`**（历史 + runtime/summary 常驻字段） |
| **`core/main_agent/`** | 主 Agent：**`get_model`**、**`get_system_prompt`** 等 |
| **`harness/`** | **工具**、**queue**、**service**、**api**、**streaming**、**memory**/**cli**/**ui**；Agent 组装在 **`core/main_agent/agent.py`** |

## 入口

- 推荐：**仓库根 `run_agent.py`** → 调用 **`app.harness.cli.main.main(project_root)`**。
- 模块方式：`PYTHONPATH=<仓库根> python -m app.harness.cli.main`（等价，需保证 `PYTHONPATH`）。

## 模块索引

详见同目录 **`REFERENCE.md`**；各子目录另有 **`README.md`** 说明文件用途。
