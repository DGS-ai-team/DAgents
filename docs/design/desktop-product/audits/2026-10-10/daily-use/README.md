# Desktop 日常使用闭环验收

日期：2026-10-10。对象：真实 Windows Tauri 窗口，当前开发前端；不是浏览器预览。

本轮覆盖输入栏复验、真实模型切换/回复、工具审批、文件预览/保存及资源页停止生成。整体 MVP 尚未宣告全部验收完成。

## 环境与隔离

- 主开发 Node `18765` 使用 Mock。本轮没有修改其 LLM 配置或发送新的测试消息。
- 复用已有的隔离测试运行目录，启动 Node `18766` 和独立 Tauri 实例；使用其已配置的真实 `mimo-v2.5-pro` / `mimo-v2.5`，没有导出凭据。
- 新建专用普通 Agent `agt-4b7e104706f577bc`，启用 `fs` 工具组；仅将这个 Agent 的 `read_file` 策略设为 `always`。
- 首次 fixture 创建请求误用了 `filesystem` 工具组；失败记录已归档。首次读文件时 fixture 不在 Agent 的 private workspace，故失败；随后通过 workspace API 在正确工作区写入无敏感内容的 `hello.txt`，复测成功。这两项是测试准备错误，不计为产品缺陷。
- 两个 Desktop 实例并存导致辅助 bridge 的固定端口 `18767` 冲突。实际消息/API、Rust SSE 与本次验收使用独立的 Node transport；本轮不据此宣告辅助 bridge 或目录选择器通过。
- 收尾：专用验收 Agent 已归档，测试文件已恢复；只停止隔离 Node/Desktop，主开发 Node/Desktop 继续运行，临时未发送草稿已清空。既有未提交变更保留，本轮没有直接提交或创建 PR。

## 逐步结果

| 步骤 | 结果 | 证据与结论 |
|---|---|---|
| 1. 空输入及软换行 | 通过 | 默认约 56px 紧凑输入框；连续中文软换行后正文展开，附件/模型/发送保持底部对齐。多次观察未反复折叠。[软换行](02-soft-wrap.jpg) |
| 2. Agent 草稿归属 | 通过 | 普通助手和自主助手切换时，各自草稿保留；空草稿恢复紧凑高度。测试未发送的主 Node 草稿已清空。[已清空](05-composer-cleared.jpg) |
| 3. 设置往返 | 通过 | 通用设置返回后，自主助手的草稿及多行高度恢复。[恢复后的草稿](04-draft-restored.jpg) |
| 4. 真实模型切换 | 通过 | 原生菜单选择 `mimo-v2.5` 后，输入栏显示更新；API Agent 配置 `defaults.llm.active` 同步为该 profile。[模型切换](06-real-model-switched.jpg) |
| 5. 真实对话 | 通过 | 原生输入、发送 `17×23`，收到 `391`；Node hydrate 历史含对应 user/assistant，回合 completed。[真实回复](07-real-reply.jpg) |
| 6. 只读工具审批 | 通过 | 原生窗口出现 `read_file` 审批卡；点击批准后工具 status=succeeded，模型返回 `DESKTOP_DAILY_APPROVAL_OK`，回合 completed。[审批卡](11-read-file-approval.jpg)、[最终历史](15-file-saved.jpg) |
| 7. 工具失败恢复 | 通过 | fixture 缺失时，失败工具气泡和真实模型的文件不存在说明可见；随后能继续发消息并完成成功读取。 |
| 8. 文件资源 | 通过 | 工作区入口显示 `hello.txt`；点击后展示内容。原生编辑并保存新增一行，API GET 精确确认内容及新 ETag，随后恢复 fixture 原内容。[预览](12-workspace-preview.jpg) |
| 9. 资源页消息与停止 | 通过 | 文件页悬浮输入栏可发送到同一 Agent，生成时显示状态及停止键。原生点击停止后 API `phase=cancelled`、`terminal=true`、`end_reason=cancelled_by_user`，界面恢复空闲。[生成中](13-resource-generating.jpg)、[停止后](14-resource-cancelled.jpg) |
| 10. 首 token 前反馈 | 修复后通过 | 最后一次从原生窗口发送新消息，立即观察到本次 user bubble、生成占位和停止键；后续流式正文替换占位。[等待态](10-generating-with-stop.jpg)、[新回合流式回复](16-immediate-user-and-waiting.jpg) |
| 11. 生成期间切换 Agent | 通过（包含代码回归） | A 仍在 model_generating 时切至 B，B 显示自己的模型且输入草稿后发送键可用；未发送 B 的草稿并已清空。[另一 Agent 可输入](17-other-agent-ready.jpg)。A 的请求失败、finally 与 B 状态隔离另由回归测试覆盖，不宣称人为制造了真实网络失败。 |
| 12. Node 断开与恢复 | 通过 | 只停止隔离 Node，Desktop 显示离线与等待状态；重启隔离 Node 后自动恢复在线、当前 Agent 及完整会话，API 返回 completed 和 15 条历史。[离线](18-node-offline.jpg)、[重连](19-node-reconnected.jpg) |

截图按采集顺序命名。`15-file-saved.jpg` 采集时开发热更新已返回对话，因此实际内容是完整成功工具历史与已取消消息，文件保存的结论依据原生操作及 API 内容校验。`09-request-waiting-before-fix.jpg` 是修复过程中间状态，不作为最终 UI 基线。

## 本轮发现与修复

编码由 Luna 完成，主审负责真实窗口验收和代码复核。

1. 上传附件期间切换 Agent，非图片文件的引用可能丢失：现在上传完成后写入原发送 Agent 的附件列表。
2. 旧 Agent 请求失败或同一 Agent 旧模型详情响应可能覆盖当前模型显示：增加归属和请求序号保护。
3. 模型首次输出前缺少反馈和停止入口：发送时展示临时用户消息及生成占位，持久化历史确认后去重；Node 进入可取消状态后使用同一按钮切换停止图标。
4. 发送/审批/取消请求的忙碌与错误按发起 Agent 隔离，切换后不影响另一 Agent；资源页输入的可访问标签同步改为“向当前智能体发送消息”。

验证：Desktop UI 测试 36/36 通过，Vite 生产构建成功；相关代码 whitespace 检查通过。构建保留既有动态导入与大分块警告。

观察到的次要问题：离线状态直接展示底层 health 请求错误，技术细节较多，可改成简短主提示并折叠诊断信息。它没有阻塞本轮自动重连。

## 后续验收清单

- [ ] 独立缩小/放大窗口的输入栏复验。本次拖拽没有实际改变窗口尺寸，不能记为通过。
- [ ] 附件上传进行中切换 Agent 的真实操作复验；本轮只完成代码修复。
- [ ] 拒绝审批、询问信息及记忆冲突的真实 UI 交互。
- [x] Node 断开/恢复。
- [ ] 发送失败的草稿恢复真实 UI 操作（已有纯逻辑回归测试）。
- [x] 终端新建、输入、关闭、Agent 切换及多 terminal 已通过原生复验：两终端同时连接；切 Agent 后直接关闭第一个 detached tab，第二个 tab 自动恢复 prompt；关闭最后一个后 API/UI 资源归零。交互 PTY 不再持有 workspace 写租约；CLI 回归确认两个 PTY 打开时 bash_run 仍能完成。最终截图和测试见 [Oct 11 资源与设置报告](../../2026-10-11/resources-settings/README.md#终端并发问题与修复)。
- [x] 浏览器真实会话与只读资源页已在 Oct 11 原生验证：双页面、页面标题/资源 tab 同步；不覆盖人工接管或地址栏导航。
- [x] 工作组真实 supervisor→worker→supervisor 回合、Auto 定时唤醒与 Dreaming 经验读取已在 Oct 12 隔离环境原生验证；同 Agent 侧栏导航回聊天及工作组历史恢复也通过。准确范围、失败过程截图、UI 42/42 与 build 结果见 [Oct 12 Auto 与工作组报告](../../2026-10-12/auto-workgroup/README.md)。
- [ ] 设置项完整保存、重新读取及运行生效；本轮仅验证 Oct 11 报告所述 Node 名称，不代表所有设置项。
- [ ] 安装/打包、辅助 bridge 及原生目录选择器单实例验收。

本轮截图不能替代完整可访问性、全部设置项、多窗口和发布包验收。
