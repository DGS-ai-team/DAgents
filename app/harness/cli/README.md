# `app/harness/cli/`

| 文件 | 说明 |
|------|------|
| **`main.py`** | 方案二 HTTP CLI；**TTY** 下 **`patch_stdout` + `PromptSession`**；普通行仅入队、不打断当前 turn；**`/yes`** / **`/no`** 审批；**`/cancel`** 取消；**`/cancel 正文`** 入队并 cancel；详见 **`REFERENCE.md`** |

仓库根 **`run_agent.py`** 调用 **`main(仓库根)`**。
