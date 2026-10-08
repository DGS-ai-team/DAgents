import { useMemo, useState } from "react";
import "./prototype.css";

type Resource = { id: string; kind: "terminal" | "browser" | "file"; title: string; detail?: string };
type Message = { role: "user" | "assistant"; text: string; time: string };

const icon = (name: string, className = "") => <span className={`icon ${className}`} style={{ maskImage: `url(/assets/icons/${name}.svg)`, WebkitMaskImage: `url(/assets/icons/${name}.svg)` }} aria-hidden="true" />;
const initialMessages: Message[] = [
  { role: "user", text: "帮我构建一下当前项目。", time: "10:41" },
  { role: "assistant", text: "我会先检查项目脚本，再运行构建。", time: "10:41" },
  { role: "assistant", text: "构建需要安装缺少的依赖。安装前我会先请求审批。", time: "10:42" },
];
const agents = ["通用助手", "代码助手", "产品顾问", "数据分析师"];

export function Prototype() {
  const [activeAgent, setActiveAgent] = useState("通用助手");
  const [resources, setResources] = useState<Resource[]>([
    { id: "terminal-1", kind: "terminal", title: "终端 1 · bash", detail: "bash · Node dev-machine" },
    { id: "browser-docs", kind: "browser", title: "浏览器 · 文档", detail: "https://vite.dev/guide/" },
    { id: "file-readme", kind: "file", title: "README.md", detail: "项目说明文件" },
  ]);
  const [activeTab, setActiveTab] = useState("terminal-1");
  const [showAdd, setShowAdd] = useState(false);
  const [menu, setMenu] = useState("");
  const [showSessions, setShowSessions] = useState(false);
  const [session, setSession] = useState("bash · Node dev-machine");
  const [approval, setApproval] = useState<"pending" | "allowed" | "denied" | "none">("pending");
  const [model, setModel] = useState("deepseek-chat");
  const [showModels, setShowModels] = useState(false);
  const [draft, setDraft] = useState("");
  const [conversations, setConversations] = useState<Record<string, Message[]>>({ "通用助手": initialMessages });
  const messages = conversations[activeAgent] ?? [];
  const [terminalInput, setTerminalInput] = useState("");
  const [notice, setNotice] = useState("");
  const [browserDraft, setBrowserDraft] = useState("");
  const [resourceMenuId, setResourceMenuId] = useState<string | null>(null);
  const [terminalLines, setTerminalLines] = useState([
    { text: "user@dev-machine:~/projects$ npm run build", type: "prompt" },
    { text: "", type: "normal" },
    { text: "> my-app@0.1.0 build", type: "normal" },
    { text: "> vite build", type: "normal" },
    { text: "", type: "normal" },
    { text: "vite v5.0.8 building for production...", type: "normal" },
    { text: "× error during build:", type: "error" },
    { text: "Error: Cannot find package '@vitejs/plugin-vue'", type: "error" },
    { text: "    at resolve (node:internal/modules/…)", type: "muted" },
    { text: "    at …", type: "muted" },
    { text: "", type: "normal" },
    { text: "npm ERR! code 1", type: "muted" },
    { text: "npm ERR! path /home/user/project", type: "muted" },
    { text: "npm ERR! command failed", type: "muted" },
    { text: "", type: "normal" },
  ]);

  const activeResource = useMemo(() => resources.find((r) => r.id === activeTab), [resources, activeTab]);
  const switchAgent = (agent: string) => {
    if (agent === activeAgent) return;
    setActiveAgent(agent);
    setActiveTab("chat");
    setResources([]);
    setApproval("none");
    setResourceMenuId(null);
  };
  const addResource = (kind: Resource["kind"]) => {
    const n = resources.filter((r) => r.kind === kind).length + 1;
    const entry: Resource = kind === "terminal"
      ? { id: `terminal-${Date.now()}`, kind, title: `终端 ${n} · bash`, detail: "bash · Node dev-machine" }
      : kind === "browser"
        ? { id: `browser-${Date.now()}`, kind, title: "浏览器 · 新标签", detail: "输入网址以访问页面" }
        : { id: `file-${Date.now()}`, kind, title: "新建文件", detail: "工作区文件" };
    setResources((items) => [...items, entry]); setActiveTab(entry.id); setShowAdd(false);
    if (kind === "browser") setBrowserDraft("");
  };
  const closeResource = (id: string) => {
    const next = resources.filter((r) => r.id !== id); setResources(next);
    if (activeTab === id) setActiveTab(next.at(-1)?.id ?? "chat");
  };
  const sendMessage = () => {
    if (!draft.trim()) return;
    const now = new Date().toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit", hour12: false });
    setConversations((current) => ({ ...current, [activeAgent]: [...(current[activeAgent] ?? []), { role: "user", text: draft.trim(), time: now }, { role: "assistant", text: `收到，我会以${model}继续处理。`, time: now }] })); setDraft("");
  };
  const runTerminal = () => {
    if (!terminalInput.trim()) return;
    const command = terminalInput.trim();
    const reply = command === "pwd" ? "/home/user/projects" : command === "ls" ? "README.md  package.json  src/  node_modules/" : command === "whoami" ? "user" : `bash: ${command.split(" ")[0]}: command simulated`;
    setTerminalLines((lines) => [...lines, { text: `user@dev-machine:~/projects$ ${command}`, type: "prompt" }, { text: reply, type: "normal" }]); setTerminalInput("");
  };
  const decideApproval = (decision: "allowed" | "denied") => {
    setApproval(decision);
    setTerminalLines((lines) => [...lines, { text: "", type: "normal" }, { text: decision === "allowed" ? "已批准一次：npm i -D @vitejs/plugin-vue" : "已拒绝安装依赖", type: decision === "allowed" ? "success" : "error" }, ...(decision === "allowed" ? [{ text: "added 12 packages · 2.4s", type: "normal" }] : [])]);
  };
  const menuItems: Record<string, string[]> = { 文件: ["新建对话", "新建终端", "打开文件…", "设置"], 编辑: ["撤销", "重做", "查找", "偏好设置"], 视图: ["显示对话", "显示终端", "显示浏览器", "重置布局"], 运行: ["继续运行", "停止当前任务", "重新运行上次任务"], 工具: ["终端", "浏览器", "工作区文件", "审批记录"], 帮助: ["快捷键", "文档", "关于 DAgents"] };
  const menuAction = (item: string) => { setMenu(""); if (item.includes("终端")) addResource("terminal"); else if (item.includes("浏览器")) addResource("browser"); else if (item.includes("文件")) addResource("file"); else if (item.includes("对话")) setActiveTab("chat"); else if (item.includes("审批")) setActiveTab(resources.find((r) => r.kind === "terminal")?.id ?? "chat"); else if (item.includes("布局")) { setActiveTab(resources.find((r) => r.kind === "terminal")?.id ?? "chat"); showNotice("已恢复默认布局"); } else if (item.includes("继续") || item.includes("重新运行")) { showNotice("任务已继续运行"); } else showNotice(`${item} · 原型演示`); };
  const showNotice = (text: string) => { setNotice(text); window.setTimeout(() => setNotice(""), 2600); };
  const citeActiveResource = () => {
    if (!activeResource) return;
    const time = new Date().toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit", hour12: false });
    const detail = activeResource.kind === "browser" ? `浏览器演示页面：${activeResource.detail}` : `工作区文件演示内容：${activeResource.title}`;
    setConversations((current) => ({ ...current, [activeAgent]: [...(current[activeAgent] ?? []), { role: "assistant", text: `已将${detail}添加到对话上下文（演示内容）。`, time }] }));
    setResourceMenuId(null);
    setActiveTab("chat");
  };
  const navigateBrowser = () => {
    if (!activeResource || activeResource.kind !== "browser") return;
    const entered = browserDraft.trim();
    if (!entered) { showNotice("请输入网址后再前往"); return; }
    const destination = /^https?:\/\//i.test(entered) ? entered : `https://${entered}`;
    setResources((items) => items.map((item) => item.id === activeResource.id ? { ...item, detail: destination } : item));
    setBrowserDraft(destination);
    showNotice("已打开模拟页面：演示内容，未连接真实网站");
  };

  return <main className="desktop-shell" onClick={() => { if (showAdd) setShowAdd(false); if (showModels) setShowModels(false); }}>
    <header className="menubar" onClick={(e) => e.stopPropagation()}>
      <div className="menu-items">{Object.keys(menuItems).map((name) => <div className="menu-wrap" key={name}><button className={`menu-button ${menu === name ? "open" : ""}`} onClick={() => setMenu(menu === name ? "" : name)}>{name}</button>{menu === name && <div className="menu-popover">{menuItems[name].map((item) => <button key={item} onClick={() => menuAction(item)}>{item}</button>)}</div>}</div>)}</div>
      <div className="window-controls"><button aria-label="最小化">−</button><button aria-label="最大化">□</button><button aria-label="关闭">×</button></div>
    </header>
    <div className="app-layout">
      <aside className="sidebar">
        <div className="side-scroll">
          <div className="side-heading">{icon("bot")}<span>智能体</span><span className="count">4</span></div>
          <div className="agent-list">{agents.map((agent, i) => <button key={agent} className={`agent-row ${activeAgent === agent ? "selected" : ""}`} onClick={() => switchAgent(agent)}><span className="agent-icon">{icon(["bot", "scroll-text", "brain", "list-checks"][i])}</span><span>{agent}</span>{i === 0 && <span className="agent-presence">now</span>}</button>)}</div>
          <div className="side-heading section-heading">{icon("users-round")}<span>工作组</span><span className="count">2</span></div>
          <button className={`agent-row ${activeAgent === "前端团队" ? "selected" : ""}`} onClick={() => switchAgent("前端团队")}><span className="agent-icon">{icon("users-round")}</span><span>前端团队</span></button><button className={`agent-row ${activeAgent === "后端团队" ? "selected" : ""}`} onClick={() => switchAgent("后端团队")}><span className="agent-icon">{icon("users-round")}</span><span>后端团队</span></button>
          <div className="side-heading section-heading auto-heading">{icon("sparkles")}<span>自主智能体</span><span className="count">2</span></div>
          <button className={`agent-row auto-row ${activeAgent === "Auto Dev" ? "selected" : ""}`} onClick={() => switchAgent("Auto Dev")}>{icon("sparkles")}<span>Auto Dev</span></button><button className={`agent-row auto-row ${activeAgent === "Auto Ops" ? "selected" : ""}`} onClick={() => switchAgent("Auto Ops")}>{icon("sparkles")}<span>Auto Ops</span></button>
        </div>
        <div className="sidebar-bottom"><div className="node-status"><span>DAgents</span><i /></div><div className="settings-wrap"><button className="settings-button" aria-label="设置" onClick={() => setMenu(menu === "设置" ? "" : "设置")}>{icon("settings")}</button>{menu === "设置" && <div className="settings-popover"><b>设置 · 演示</b><button onClick={() => { setMenu(""); showNotice("当前外观：浅色主题（演示）"); }}>外观：浅色</button><button onClick={() => { setMenu(""); showNotice("连接状态：模拟在线 · 未连接 Node"); }}>连接状态</button><button onClick={() => { setMenu(""); showNotice("演示偏好设置已保存"); }}>保存偏好设置</button></div>}</div></div>
      </aside>
      <section className="workspace">
          <div className="tabs-bar">
          <button className={`tab chat-tab ${activeTab === "chat" ? "active" : ""}`} onClick={() => setActiveTab("chat")}>{icon("message-circle")}<span>对话</span></button>
          {resources.map((resource) => <div className={`tab resource-tab ${activeTab === resource.id ? "active" : ""}`} key={resource.id}>
            <button className="tab-main" onClick={() => setActiveTab(resource.id)}>{icon(resource.kind === "terminal" ? "square-terminal" : resource.kind === "browser" ? "network" : "file")}<span>{resource.title}</span></button>
            <button className="tab-close" aria-label={`关闭${resource.title}`} onClick={() => closeResource(resource.id)}>{icon("circle-x")}</button>
          </div>)}
          <div className="add-tab-wrap"><button className="add-tab" aria-label="新增资源" onClick={(e) => { e.stopPropagation(); setShowAdd(!showAdd); }}>{icon("circle-plus")}</button>{showAdd && <div className="add-popover" onClick={(e) => e.stopPropagation()}><button onClick={() => addResource("terminal")}>{icon("square-terminal")}新建终端</button><button onClick={() => addResource("browser")}>{icon("network")}新建浏览器</button><button onClick={() => addResource("file")}>{icon("file")}打开文件</button></div>}</div>
        </div>
        {activeTab === "chat" ? <section className="chat-view"><div className="history-header"><div><b>{activeAgent}</b><span>对话记录 · 演示内容</span></div><button className="history-action" onClick={() => { setConversations((current) => ({ ...current, [activeAgent]: [] })); showNotice(`已为${activeAgent}新建演示对话`); }}>{icon("refresh-cw")}新建对话</button></div><div className="conversation">{messages.length > 0 ? <><div className="history-day">今天 · 10:41</div>{messages.map((message, i) => <article key={i} className={`message ${message.role}`}><div className="message-content"><p>{message.text}</p><span>{message.time}</span></div></article>)}</> : <div className="empty-conversation">这是 {activeAgent} 的独立演示对话，发送消息开始。</div>}</div><MessageComposer draft={draft} setDraft={setDraft} onSend={sendMessage} model={model} setModel={setModel} showModels={showModels} setShowModels={setShowModels} /></section> : activeResource?.kind === "terminal" ? <>
          <div className="resource-toolbar"><div className="session-wrap"><button className="session-select" onClick={() => setShowSessions(!showSessions)}><i className="live-dot"/><span>{session}</span>{icon("chevron-down")}</button>{showSessions && <div className="session-popover">{["bash · Node dev-machine", "zsh · Node dev-machine", "PowerShell · 本地"].map((item) => <button key={item} onClick={() => { setSession(item); setShowSessions(false); }}>{item}{session === item && icon("circle-check", "session-check")}</button>)}</div>}</div><button className="more-button" aria-label="终端选项" onClick={() => setMenu(menu === "terminal" ? "" : "terminal")}>{icon("ellipsis")}</button>{menu === "terminal" && <div className="terminal-menu"><button onClick={() => { setTerminalLines([]); setMenu(""); }}>清空终端</button><button onClick={() => { setTerminalLines((l) => [...l, { text: "终端会话已重新连接", type: "success" }]); setMenu(""); }}>重新连接</button></div>}</div>
          <div className="terminal-area"><div className="terminal-output">{terminalLines.map((line, i) => <div key={i} className={`terminal-line ${line.type}`}>{line.text || " "}</div>)}<form className="terminal-command" onSubmit={(e) => { e.preventDefault(); runTerminal(); }}><span>user@dev-machine:~/projects$</span><input value={terminalInput} onChange={(e) => setTerminalInput(e.target.value)} aria-label="终端命令" autoComplete="off" /><button aria-label="执行命令">{icon("play")}</button></form></div></div>
          <BottomPanel approval={approval} onDecide={decideApproval} onDetails={() => showNotice(approval === "allowed" ? "操作记录：依赖安装已完成" : "操作记录：安装请求已拒绝")} onOpenChat={() => setActiveTab("chat")} activeAgent={activeAgent} onSend={sendMessage} draft={draft} setDraft={setDraft} model={model} setModel={setModel} showModels={showModels} setShowModels={setShowModels} />
        </> : <>
          <div className="resource-toolbar"><div className="resource-label">{icon(activeResource?.kind === "browser" ? "network" : "file")}<span>{activeResource?.detail}</span></div><div className="resource-options-wrap"><button className="more-button" aria-label="资源选项" aria-expanded={resourceMenuId === activeResource?.id} onClick={() => setResourceMenuId(resourceMenuId === activeResource?.id ? null : activeResource?.id ?? null)}>{icon("ellipsis")}</button>{resourceMenuId === activeResource?.id && activeResource && <div className="resource-options-popover"><b>资源选项</b><button onClick={() => { showNotice(`资源信息：${activeResource.title} · ${activeResource.detail}（演示内容）`); setResourceMenuId(null); }}>查看资源信息</button><button onClick={citeActiveResource}>在对话中引用</button><button className="close-resource-option" onClick={() => { setResourceMenuId(null); closeResource(activeResource.id); }}>关闭此资源</button></div>}</div></div>
          <div className="resource-content">{activeResource?.kind === "browser" ? <div className="browser-mock"><form className="address-bar" onSubmit={(e) => { e.preventDefault(); navigateBrowser(); }}><span className="address-icon">{icon("search")}</span><input value={activeResource.detail.startsWith("http") ? activeResource.detail : browserDraft} onChange={(e) => setBrowserDraft(e.target.value)} aria-label="浏览器网址" placeholder="输入网址，例如 example.com" /><button type="submit">前往</button></form><div className="browser-page"><span className="demo-badge">演示内容 · 未连接真实网站</span><h1>{activeResource.detail.startsWith("http") ? "模拟页面预览" : "输入网址开始预览"}</h1><p>{activeResource.detail.startsWith("http") ? "已加载本地演示页面" : "输入任意网址后点击“前往”，查看模拟导航结果。"}</p>{activeResource.detail.startsWith("http") && <div className="demo-url">{activeResource.detail}</div>}<button onClick={citeActiveResource} disabled={!activeResource.detail.startsWith("http")}>在对话中引用此演示页面</button></div></div> : <div className="file-mock"><div className="file-title">{icon("file")}<h2>{activeResource?.title}</h2><span>演示内容</span></div><div className="file-meta">此文件为原型中的只读示例。操作不会修改工作区文件。</div><pre>{"# my-app\n\n项目工作区文件（演示内容）\n\n- package.json\n- src/\n- README.md"}</pre><button className="file-cite-button" onClick={citeActiveResource}>在对话中引用文件</button></div>}</div>
          <BottomPanel approval={approval} onDecide={decideApproval} onDetails={() => showNotice(approval === "allowed" ? "操作记录：依赖安装已完成" : "操作记录：安装请求已拒绝")} onOpenChat={() => setActiveTab("chat")} activeAgent={activeAgent} onSend={sendMessage} draft={draft} setDraft={setDraft} model={model} setModel={setModel} showModels={showModels} setShowModels={setShowModels} />
        </>}
      </section>
    </div>{notice && <div className="notice-toast" role="status">{notice}</div>}
  </main>;
}

type ComposerProps = { draft: string; setDraft: (v: string) => void; onSend: () => void; model: string; setModel: (v: string) => void; showModels: boolean; setShowModels: (v: boolean) => void };
function MessageComposer({ draft, setDraft, onSend, model, setModel, showModels, setShowModels }: ComposerProps) {
  return <div className="composer-zone"><form className="message-composer" onSubmit={(e) => { e.preventDefault(); onSend(); }}><button type="button" className="attach-button" aria-label="添加附件">{icon("upload")}</button><input placeholder="发送消息…" value={draft} onChange={(e) => setDraft(e.target.value)} aria-label="发送消息"/><div className="model-wrap"><button type="button" className="model-select" onClick={() => setShowModels(!showModels)}>{model}{icon("chevron-down")}</button>{showModels && <div className="model-popover">{["deepseek-chat", "deepseek-reasoner", "gpt-4o"].map((m) => <button key={m} type="button" onClick={() => { setModel(m); setShowModels(false); }}>{m}{model === m && <span>✓</span>}</button>)}</div>}</div><button className="send-button" aria-label="发送消息">{icon("play")}</button></form></div>;
}
type BottomProps = ComposerProps & { approval: "pending" | "allowed" | "denied" | "none"; onDecide: (value: "allowed" | "denied") => void; onDetails: () => void; onOpenChat: () => void; activeAgent: string };
function BottomPanel({ approval, onDecide, onDetails, onOpenChat, activeAgent, ...composerProps }: BottomProps) {
  return <div className="bottom-panel"><div className={`approval-row ${approval}`}><div className="approval-info">{icon(approval === "pending" ? "triangle-alert" : approval === "allowed" ? "circle-check" : approval === "denied" ? "circle-x" : "info")}<b>{approval === "pending" ? "待审批" : approval === "allowed" ? "已允许一次" : approval === "denied" ? "已拒绝" : "暂无待审批"}</b><span>·</span><span>{approval === "pending" ? "安装依赖" : approval === "allowed" ? "操作已继续" : approval === "denied" ? "操作已停止" : "资源和审批按 Agent 隔离"}</span>{approval !== "none" && <code>npm i -D @vitejs/plugin-vue</code>}</div>{approval === "pending" ? <div className="approval-actions"><button className="allow-button" onClick={() => onDecide("allowed")}>允许一次</button><button className="deny-button" onClick={() => onDecide("denied")}>拒绝</button></div> : approval !== "none" ? <button className="undo-button" onClick={onDetails}>查看详情</button> : null}</div><div className="activity-row"><button className="activity-link" onClick={onOpenChat}>{icon("message-circle")}<b>消息</b><span>·</span><span>{approval === "pending" ? "已定位构建失败原因" : approval === "allowed" ? "依赖安装完成，等待继续" : approval === "denied" ? "已记录审批结果" : `${activeAgent} 的独立演示对话`}</span></button><span className="activity-divider"/><button className="activity-link tools-link" onClick={() => approval === "none" ? onOpenChat() : onDetails}>{icon("wrench")}<b>工具</b><span>·</span><span>{approval === "none" ? "仅显示当前 Agent 资源" : approval === "pending" ? "等待审批后继续" : approval === "allowed" ? "依赖安装已完成" : "请求已拒绝，任务已暂停"}</span></button></div><MessageComposer {...composerProps} />
  </div>;
}
