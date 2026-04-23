# `frontend/src/` 说明

前端源码目录（React + TypeScript，已可运行）。

## 当前结构

- `main.tsx`：React 挂载入口。
- `App.tsx`：应用根组件（当前渲染 `ChatWorkbench`）。
- `styles.css`：全局设计令牌与样式系统。
- `pages/ChatWorkbench.tsx`：工作台页面（主对话 + 运行状态 + 子线程）。
- `components/MainChatPanel.tsx`：主聊天流与输入区。
- `components/ApprovalToolBubble.tsx`：工具调用审批/执行状态气泡（内联于聊天流）。
- `components/RuntimeStatusPanel.tsx`：运行状态与 tokens。
- `components/SubAgentThreadTabs.tsx`：子 Agent 线程切换。
- `components/SubAgentThreadView.tsx`：子线程实时输出展示。
- `components/ui.tsx`：通用 UI 小组件（状态 pill、risk badge 等）。
- `api/`：API 契约与调用封装（`types.ts` 自动生成，`client.ts` 手写封装）。
- `ui-contracts.ts`：前端 UI 类型契约与事件类型。

## 契约范围（当前）

- 主聊天消息流（`assistant/reasoning/tool`）。
- 工具调用审批（每个工具独立按钮）与执行中/已返回状态。
- 多 Agent 子线程展示。
- SSE 事件统一类型（用于后续接入真实后端流）。

## 可优化点（已记录，暂不实现）

- `ToolExecutionBubble` 在 `display_type = "image"` 时，目前优先直接把结果文本当作图片 URL 使用。
- 可进一步增强为：当结果为 markdown 图片语法（如 `![](https://...)`）时，先提取 URL 再渲染图片，提升兼容性与容错性。
