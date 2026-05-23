# `app/` 应用包

Python **命名空间包**（无 `__init__.py`，PEP 420），从仓库根将根目录加入 `sys.path` 后即可 `import app.*`。

| 子目录 | 职责 |
|--------|------|
| **`config/`** | 配置：**`Settings`**、**`get_settings()`**；**`load_env`**（`.env`） |
| **`context/`** | 会话上下文模型：**`MessageRecord`**、**`ConversationContext`**（历史 + runtime/summary 常驻字段） |
| **`core/main_agent/`** | 主 Agent：**`get_model`**、**`get_system_prompt`** 等 |
| **`harness/`** | **工具**、**queue**、**service**、**api**、**streaming**、**memory**/**ui**；Agent 组装在 **`core/main_agent/agent.py`** |

## 入口

- 对外 HTTP 服务：在仓库根执行 **`python run_agent_api.py`**（详见仓库根 **README.md**）。
- CLI、Register Center、本地联调等其它入口：见仓库根说明。

## 模块索引

详见同目录 **`REFERENCE.md`**；各子目录另有 **`README.md`** 说明文件用途。
