# 系统提示侧车种子（`packaging/prompt_context/`）

本目录为 **UTF-8 Markdown 模板**，供首次运行或离线包解压后 **复制/同步** 到运行根下的 **`.runtime/prompt_context/`**。

**运行时读取路径**（与仓库解耦）：**`<resolve_runtime_root()>/.runtime/prompt_context/`**，由 **`app/core/main_agent/prompt.py`** 的 **`get_system_prompt`** 读取；目录默认 **被 `.gitignore` 忽略**，请勿依赖把本地 `.runtime` 提交进版本库。

| 文件 | 用途 |
|------|------|
| **`soul.md`** | 智能体设定（人格、职责、语气等），非空时拼入系统提示 **「以下是你的设定」** 节。 |
| **`user.md`** | 用户偏好，非空时拼入 **「以下是用户信息与偏好」** 节。 |
| **`custom.md`** | 用户自定义补充；非空时拼在 **`.runtime` 约定** 与 JSONL 说明之后、**`session_id`** 段之前，标题为 **「以下是用户侧追加的临时/专项指令」**。 |

首次启动时，若 **`.runtime/prompt_context/`** 下缺少对应文件且仓库内存在 **`packaging/prompt_context/`**，**`prompt.py`** 会从种子 **拷贝缺失的 `.md`**（不覆盖已有文件）。
