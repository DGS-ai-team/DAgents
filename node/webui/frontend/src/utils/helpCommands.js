/** Web UI slash 命令帮助（对齐 TUI FormatHelpPanelBody，仅含 Web 已实现的命令）。 */
export const HELP_SHORTCUTS = [
  { keys: "Enter", desc: "发送消息" },
  { keys: "Ctrl+Enter", desc: "输入框内换行" },
  { keys: "Esc", desc: "关闭弹窗面板" },
];

export const HELP_SECTIONS = [
  {
    title: "Session",
    items: [
      { cmd: "/status", desc: "Agent、LLM 与当前 session 状态" },
      { cmd: "/sessions", desc: "列出 session（/ls 同义）" },
      { cmd: "/switch <id>", desc: "切换到指定 session" },
      { cmd: "/new", desc: "新建 session" },
      { cmd: "/clear", desc: "清空对话上下文与 transcript" },
      { cmd: "/cancel", desc: "中断在途 turn（或点「取消」）" },
    ],
  },
  {
    title: "上下文与 Skills",
    items: [
      { cmd: "/context", desc: "只读 context 视图（消息、token、skills）" },
      { cmd: "/skill", desc: "已加载 / 可用 skills 列表" },
      { cmd: "/skill load NAME", desc: "加载 skill 到当前 session" },
      { cmd: "/skill unload NAME", desc: "从 session 卸载 skill" },
      { cmd: "/children", desc: "子 Agent 列表（/child 同义）" },
      { cmd: "/compress", desc: "手动触发阻塞压缩" },
    ],
  },
  {
    title: "策略与触发器",
    items: [
      { cmd: "/policy", desc: "工具 / Shell 策略" },
      { cmd: "/triggers", desc: "查看已配置触发器" },
    ],
  },
];
