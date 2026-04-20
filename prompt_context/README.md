# `prompt_context/`

位于**仓库根目录**、与 **`app/`** 同级。与 **`get_system_prompt`**（**`app/core/main_agent/prompt.py`**）配套的 Markdown 侧车（UTF-8）。修改后再次调用 **`get_system_prompt`** 时会按文件 **mtime** 重新读入（进程内缓存）。

| 文件 | 用途 |
|------|------|
| **`soul.md`** | 智能体设定：人格、职责、语气、边界等 |
| **`user.md`** | 用户偏好：称呼、输出风格、领域习惯等 |
| **`custom.md`** | 用户自定义补充，**拼在整条 system prompt 最后**（在运行环境段之后）；默认可为空，有正文时才追加一节 |

拼接顺序见 **`get_system_prompt`** 的 docstring。
