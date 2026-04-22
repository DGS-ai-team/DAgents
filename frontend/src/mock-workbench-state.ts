import type { ApprovalTask, ChatWorkbenchState, RuntimeState, SubAgentThread } from "./ui-contracts";

const now = Date.now();

const runtime: RuntimeState = {
  status: "running",
  model: "gpt-4.1",
  usage: {
    inputTokens: 1024,
    outputTokens: 256,
    totalTokens: 1280,
  },
};

const approvals: ApprovalTask[] = [
  {
    id: "approval-1",
    sessionId: "s-demo",
    requestId: "r-demo",
    createdAt: now - 500,
    payload: {
      message: "检测到工具调用，请确认参数后选择批准或拒绝。",
      args: {
        tool_calls: [
          {
            id: "call_bash_001",
            name: "bash_run",
            arguments: { command: "ls -la /workspace" },
            riskLevel: "medium",
          },
          {
            id: "call_fetch_002",
            name: "http_fetch",
            arguments: {
              url: "https://example.com/api/data",
              method: "GET",
            },
            riskLevel: "low",
          },
        ],
      },
    },
    handled: false,
  },
  {
    id: "approval-2",
    sessionId: "s-demo",
    requestId: "r-demo",
    createdAt: now - 5000,
    payload: {
      message: "高风险写操作，需确认。",
      args: {
        tool_calls: [
          {
            id: "call_write_001",
            name: "fs_write",
            arguments: { path: "/workspace/report.md", size: 2048 },
            riskLevel: "high",
          },
        ],
      },
    },
    handled: true,
    handledAt: now - 4800,
    decision: "approve_all",
  },
];

const threads: SubAgentThread[] = [
  {
    id: "sub-1",
    parentRequestId: "r-demo",
    sessionId: "s-demo",
    createdAt: now,
    agentId: "agent-research",
    title: "Research Agent",
    status: "running",
    chunks: [
      { id: "c1", content: "开始检索依赖信息...", kind: "delta", ts: now - 4000 },
      { id: "c2", content: "调用 http_fetch 工具抓取文档。", kind: "tool", ts: now - 2500 },
      { id: "c3", content: "已找到 3 条候选结果。", kind: "summary", ts: now - 1000 },
    ],
    startedAt: now - 5000,
  },
  {
    id: "sub-2",
    parentRequestId: "r-demo",
    sessionId: "s-demo",
    createdAt: now - 20000,
    agentId: "agent-coder",
    title: "Coder Agent",
    status: "success",
    chunks: [
      { id: "c4", content: "已生成 React 组件骨架。", kind: "summary", ts: now - 10000 },
    ],
    startedAt: now - 20000,
    endedAt: now - 10000,
  },
];

export const mockWorkbenchState: ChatWorkbenchState = {
  currentSessionId: "s-demo",
  runtimeBySession: {
    "s-demo": runtime,
  },
  messagesBySession: {
    "s-demo": [
      {
        id: "m1",
        sessionId: "s-demo",
        createdAt: now - 8000,
        role: "user",
        content: "帮我分析这个项目的前端技术方案，并给出最小可运行骨架。",
      },
      {
        id: "m2",
        sessionId: "s-demo",
        createdAt: now - 7000,
        role: "reasoning",
        content: "目标：React + Vite，最小可运行。需要检查现有目录结构与依赖。",
      },
      {
        id: "m3",
        sessionId: "s-demo",
        createdAt: now - 6000,
        role: "assistant",
        content:
          "建议采用 React + Vite + TypeScript；后续可用 Tauri 打包为桌面端，兼顾包体与内存占用。",
      },
      {
        id: "m4",
        sessionId: "s-demo",
        createdAt: now - 4500,
        role: "tool",
        content: "bash_run: ls -la\n-> drwxr-xr-x  5 user  group  160B frontend",
      },
      {
        id: "m5",
        sessionId: "s-demo",
        createdAt: now - 3000,
        role: "assistant",
        content: "已初始化 frontend 目录，`ChatWorkbench` 页面骨架可以运行。",
      },
    ],
  },
  approvalsBySession: {
    "s-demo": approvals,
  },
  subThreadsBySession: {
    "s-demo": threads,
  },
  activeSubThreadBySession: {
    "s-demo": "sub-1",
  },
};
