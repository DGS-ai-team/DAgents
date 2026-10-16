# 普通对话步骤上限与 Bash 失败状态回归

日期：2026-10-16。用户确认上限问题发生在普通智能体手动对话，另一问题通常出现在 Bash 工具失败后。Luna 分别负责后端与前端，主线程审查事件契约、显示优先级和回归证据。

## 原因与修复

1. 普通 Agent 的构造函数把快照中的 `defaults.llm.max_steps` 注入预算，却没有开启 `ReserveFinalSummary`。达到上限后无法进入已有的保留摘要路径，只记录预算耗尽并停止。现为 Agent 预留一次无工具收尾；仍由 Coordinator 检查次数及其他预算，不能多执行一轮工具。如果其他预算不允许收尾，则在预算终态持久化之后发送唯一的 `turn_finished`，携带实际 `budget_reason` 和对应回合身份。
2. 前端工具生命周期注解遗漏了 `failed`；另外 Bash 控制按钮和工具组优先采用旧的 running/active 缓存，即使已有失败结果仍可显示执行中。现按结果、无结果时的权威执行终态、旧运行缓存的优先级处理。Bash 失败后停止转圈和终止控制，真正的 `running`、`queued` 仍保留。旧 `settled` hint 也不能覆盖新失败结果。
3. 显式 `turn_finished` 在回合身份匹配后可收束前端状态；不拿普通错误事件猜测整个回合已结束，也不覆盖更完整的终态生命周期投影。

## 验证证据

- 通过真实 Agent builder、Registry、Session Manager 和临时 SQLite 构造 `max_steps=1` 的普通人类回合：一次读文件后仅再请求一次摘要，摘要请求的工具列表为空；持久化记录包含 `final_summary` 步骤与回合完成。
- 预算不允许继续时，验证终态 `turn_state` 先于唯一 `turn_finished`，回合 ID/generation 匹配，数据库保留预算耗尽事实。另验证摘要不能绕过已耗尽的输出 token 预算。
- 实际本地 shell 执行 `exit 1`，分别覆盖同批 `read_file` 成功、最后 `bash_run` 失败，以及跨模型步骤最后 Bash 失败。结果带 `status=failed` 与真实 `exit_code=1`；失败结果、批次结算和回合完成均持久化，hydrate 保留失败状态，同步 shell 的 Running 计数和 ID 列表归零。测试使用合成模型控制调用顺序，不代表真实模型联网验收。
- 挂载真实 ToolGroupRow/ToolSummaryRow，覆盖前一项读文件成功、最后 Bash 失败且运行缓存滞后的情形，以及结果正文尚未到但权威执行状态已失败的情形。均不再显示执行中或终止按钮；失败文本保留。另验证 queued 不被旧 settled hint 误关、旧回合结束事件不能关闭新回合。
- Go 1.26.9 下 Node 全模块测试、vet 与 staticcheck 通过。Node Vue 77 个文件、425 项测试和共享图标 6 项通过，Desktop UI 51 项通过，Node Vue 与 Desktop 构建通过。

本轮未重启用户的 Node/Desktop，也未在原生窗口手工重测；上述证据分别来自真实本地工具执行、生命周期持久化、组件 DOM 与构建。最终远端检查以 PR 最新 head 为准。
