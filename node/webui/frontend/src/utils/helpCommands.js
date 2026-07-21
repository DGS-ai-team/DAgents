/** Web UI slash 命令帮助（仅保留前端暂无直接入口的命令）。 */
export const COMPOSER_DRAFT_KEY = "dagents.ui.composerDraft";

export const HELP_SHORTCUTS = [
  { keys: "Enter", desc: "发送消息" },
  { keys: "Shift+Enter", desc: "输入框内换行" },
  { keys: "Esc", desc: "关闭弹窗面板" },
];

export const HELP_SECTIONS = [
  {
    title: "对话控制",
    items: [
      { cmd: "/clear", desc: "清空当前 Agent 的对话上下文与 transcript" },
      { cmd: "/compress", desc: "手动触发一次上下文压缩" },
    ],
  },
  {
    title: "Thinking",
    items: [{ cmd: "/thinking on|off", desc: "开启/关闭 thinking 模式（也可用状态栏按钮）" }],
  },
  {
    title: "Manage 上传",
    items: [
      { cmd: "/upload skill PATH ID VER [NAME] [--publish]", desc: "上传 skill zip 至 Manage" },
      { cmd: "/upload externaltool PATH ID VER [NAME] [--platform] [--publish]", desc: "上传外置 CLI" },
      { cmd: "/upload plugin PATH ID VER [NAME] [--platform] [--publish]", desc: "上传 Hook plugin (.so)" },
    ],
  },
];
