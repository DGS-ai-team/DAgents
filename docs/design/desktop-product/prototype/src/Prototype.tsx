import { useEffect, useMemo, useRef, useState } from "react";
import desktopIconManifest from "@shared-icons/desktop/manifest.json";
const desktopIconUrls = {
  ...import.meta.glob("@shared-icons/desktop/**/*.svg", { eager: true, query: "?url", import: "default" }),
  ...import.meta.glob("@shared-icons/lucide/**/*.svg", { eager: true, query: "?url", import: "default" }),
} as Record<string, string>;
const desktopIconFiles = Object.fromEntries((desktopIconManifest.icons as Array<{ name: string; file: string }>).map(({ name, file }) => {
  const key = Object.keys(desktopIconUrls).find((candidate) => candidate.endsWith(`/shared/icons/${file}`));
  return [name, key ? desktopIconUrls[key] : ""];
}));
import "./prototype.css";

type Kind = "terminal" | "browser" | "file";
type Resource = { id: string; kind: Kind; title: string; detail: string };
type Message = { role: "user" | "assistant"; text: string; time: string };
type Approval = "pending" | "allowed" | "denied" | "none";
const icon = (name: string, cls = "") => {
  const source = `url("${desktopIconFiles[name] ?? ""}")`;
  return <span className={`icon ${cls}`} style={{ maskImage: source, WebkitMaskImage: source }} aria-hidden="true" />;
};
const agents = ["通用助手", "代码助手", "产品顾问", "数据分析师"];
const groups = [{ id: "agents", label: "智能体", glyph: "nav-agents", items: agents }, { id: "teams", label: "工作组", glyph: "nav-workgroups", items: ["前端团队", "后端团队"] }, { id: "auto", label: "自主智能体", glyph: "nav-auto", items: ["Auto Dev", "Auto Ops"] }];
const initial: Message[] = [{ role: "user", text: "帮我构建一下当前项目。", time: "10:41" }, { role: "assistant", text: "我会先检查项目脚本，再运行构建。", time: "10:41" }, { role: "assistant", text: "构建需要安装缺少的依赖。安装前我会先请求审批。", time: "10:42" }];
const seed: Resource[] = [{ id: "terminal-1", kind: "terminal", title: "bash", detail: "bash · Node dev-machine" }, { id: "browser-docs", kind: "browser", title: "Vite 文档", detail: "https://vite.dev/guide/" }, { id: "file-readme", kind: "file", title: "README.md", detail: "项目说明文件" }];
const seedTerminalLines = ["user@dev-machine:~/projects$ npm run build", "> my-app@0.1.0 build", "> vite build", "vite v5.0.8 building for production...", "Error during build:", "Error: Cannot find package '@vitejs/plugin-vue'", "    at resolve (node:internal/modules/…)", "npm ERR! code 1"];
const topMenus = ["DAgents", "文件", "编辑", "视图", "运行", "工具", "帮助"];
type MenuItem = { label: string; action: string; shortcut?: string; hint?: string };
const menuGroups: Record<string, MenuItem[][]> = {
  DAgents: [[{ label: "设置", action: "settings" }], [{ label: "Node 连接概况", action: "node-status", hint: "模拟在线" }, { label: "外观", action: "appearance", hint: "浅色" }], [{ label: "关于 DAgents", action: "about" }]],
  文件: [[{ label: "新建对话", action: "new-chat", shortcut: "Ctrl+N" }], [{ label: "打开文件", action: "open-file", shortcut: "Ctrl+O" }, { label: "打开浏览器", action: "open-browser", shortcut: "Ctrl+Shift+B" }, { label: "新建终端", action: "new-terminal", shortcut: "Ctrl+Shift+T" }], [{ label: "关闭当前资源", action: "close-resource", shortcut: "Ctrl+W" }]],
  编辑: [[{ label: "撤销", action: "undo", shortcut: "Ctrl+Z" }, { label: "重做", action: "redo", shortcut: "Ctrl+Shift+Z" }], [{ label: "全选当前输入", action: "select-all", hint: "输入时 Ctrl+A" }, { label: "清空当前输入", action: "clear-input" }]],
  视图: [[{ label: "收起侧栏", action: "toggle-sidebar", shortcut: "Ctrl+B" }]],
  运行: [[{ label: "当前 Agent 状态", action: "agent-status" }, { label: "发送已写草稿", action: "send-draft", shortcut: "Ctrl+Enter" }], [{ label: "查看待审批", action: "review-approval" }, { label: "打开终端", action: "open-terminal" }]],
  工具: [[{ label: "新建终端", action: "new-terminal" }, { label: "打开浏览器", action: "open-browser" }, { label: "打开文件", action: "open-file" }], [{ label: "Node 连接演示", action: "node-status", hint: "模拟" }]],
  帮助: [[{ label: "快捷键说明", action: "shortcuts" }], [{ label: "关于原型", action: "about-prototype" }]],
};

export function Prototype() {
  const [activeAgent, setActiveAgent] = useState("通用助手");
  const [resourcesByAgent, setResourcesByAgent] = useState<Record<string, Resource[]>>({ "通用助手": seed });
  const resources = resourcesByAgent[activeAgent] ?? [];
  const [activeTab, setActiveTab] = useState("chat");
  const [collapsed, setCollapsed] = useState(false);
  const [sideGroup, setSideGroup] = useState("");
  const [showAdd, setShowAdd] = useState(false);
  const [menu, setMenu] = useState("");
  const [statusOpen, setStatusOpen] = useState(false);
  const [approvalByAgent, setApprovalByAgent] = useState<Record<string, Approval>>({ "通用助手": "pending" });
  const approval = approvalByAgent[activeAgent] ?? "none";
  const [conversations, setConversations] = useState<Record<string, Message[]>>({ "通用助手": initial });
  const messages = conversations[activeAgent] ?? [];
  const [generatingByAgent, setGeneratingByAgent] = useState<Record<string, boolean>>({});
  const isGenerating = generatingByAgent[activeAgent] ?? false;
  const generationTimers = useRef<Record<string, number>>({});
  const [draftsByAgent, setDraftsByAgent] = useState<Record<string, string>>({});
  const draft = draftsByAgent[activeAgent] ?? "";
  const setDraft = (value: string) => setDraftsByAgent((d) => ({ ...d, [activeAgent]: value }));
  const [model, setModel] = useState("deepseek-chat");
  const [showModels, setShowModels] = useState(false);
  const [terminalInputsByResource, setTerminalInputsByResource] = useState<Record<string, string>>({});
  const [terminalLinesByResource, setTerminalLinesByResource] = useState<Record<string, string[]>>({ "terminal-1": seedTerminalLines });
  const [browserDraft, setBrowserDraft] = useState("");
  const [notice, setNotice] = useState("");
  const activeResource = useMemo(() => resources.find((r) => r.id === activeTab), [resources, activeTab]);
  const terminalInput = activeResource?.kind === "terminal" ? terminalInputsByResource[activeResource.id] ?? "" : "";
  const setTerminalInput = (value: string) => { if (activeResource?.kind === "terminal") setTerminalInputsByResource((all) => ({ ...all, [activeResource.id]: value })); };
  const terminalLines = activeResource?.kind === "terminal" ? terminalLinesByResource[activeResource.id] ?? [] : [];
  const updateTerminalLines = (update: string[] | ((current: string[]) => string[])) => {
    const target = activeResource?.kind === "terminal" ? activeResource : resources.find((resource) => resource.kind === "terminal");
    if (!target) return;
    setTerminalLinesByResource((all) => ({ ...all, [target.id]: typeof update === "function" ? update(all[target.id] ?? []) : update }));
  };
  useEffect(() => {
    const onEscape = (event: KeyboardEvent) => {
      if (event.key !== "Escape") return;
      setMenu(""); setShowAdd(false); setShowModels(false); setSideGroup(""); setStatusOpen(false);
    };
    window.addEventListener("keydown", onEscape);
    return () => window.removeEventListener("keydown", onEscape);
  }, []);
  const updateResources = (fn: (items: Resource[]) => Resource[]) => setResourcesByAgent((all) => ({ ...all, [activeAgent]: fn(all[activeAgent] ?? []) }));
  const switchAgent = (name: string) => { setActiveAgent(name); setActiveTab("chat"); setSideGroup(""); setStatusOpen(false); };
  const showNotice = (s: string) => { setNotice(s); window.setTimeout(() => setNotice(""), 4200); };
  const addResource = (kind: Kind) => {
    const n = resources.filter((r) => r.kind === kind).length + 1;
    const id = `${kind}-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;
    const r: Resource = kind === "terminal" ? { id, kind, title: n === 1 ? "bash" : `bash ${n}`, detail: `bash · Node dev-machine · 会话 ${n}` } : kind === "browser" ? { id, kind, title: "新标签", detail: "输入网址以访问页面" } : { id, kind, title: "README.md", detail: "工作区文件 · 只读" };
    updateResources((items) => [...items, r]); setActiveTab(r.id); setShowAdd(false); setBrowserDraft("");
  };
  const closeResource = (id: string) => { const i = resources.findIndex((r) => r.id === id); const next = resources.filter((r) => r.id !== id); updateResources(() => next); if (activeTab === id) setActiveTab(next[Math.min(i, next.length - 1)]?.id ?? "chat"); };
  const sendMessage = () => {
    if (!draft.trim()) return;
    const agent = activeAgent;
    const prompt = draft.trim();
    const selectedModel = model;
    const time = new Date().toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit", hour12: false });
    setConversations((c) => ({ ...c, [agent]: [...(c[agent] ?? []), { role: "user", text: prompt, time }] }));
    setGeneratingByAgent((g) => ({ ...g, [agent]: true }));
    setDraft("");
    const previousTimer = generationTimers.current[agent];
    if (previousTimer) window.clearTimeout(previousTimer);
    generationTimers.current[agent] = window.setTimeout(() => {
      const replyTime = new Date().toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit", hour12: false });
      setConversations((c) => ({ ...c, [agent]: [...(c[agent] ?? []), { role: "assistant", text: `收到，我会以${selectedModel}继续处理。`, time: replyTime }] }));
      setGeneratingByAgent((g) => ({ ...g, [agent]: false }));
      delete generationTimers.current[agent];
    }, 1800);
  };
  const startNewConversation = () => {
    const timer = generationTimers.current[activeAgent];
    if (timer) window.clearTimeout(timer);
    delete generationTimers.current[activeAgent];
    setConversations((all) => ({ ...all, [activeAgent]: [] }));
    setApprovalByAgent((all) => ({ ...all, [activeAgent]: "none" }));
    setGeneratingByAgent((all) => ({ ...all, [activeAgent]: false }));
    setActiveTab("chat"); setStatusOpen(false);
    showNotice(`已为${activeAgent}新建演示对话`);
  };
  const decideApproval = (decision: "allowed" | "denied") => { setApprovalByAgent((a) => ({ ...a, [activeAgent]: decision })); updateTerminalLines((l) => [...l, decision === "allowed" ? "已批准一次：npm i -D @vitejs/plugin-vue" : "已拒绝安装依赖", ...(decision === "allowed" ? ["审批已通过，等待 Agent 继续执行"] : [])]); };
  const resourceIcon = (r: Resource) => r.kind === "terminal" ? "terminal" : r.kind === "browser" ? "browser" : "file";
  const cite = () => { if (!activeResource) return; const time = new Date().toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit", hour12: false }); setConversations((c) => ({ ...c, [activeAgent]: [...(c[activeAgent] ?? []), { role: "assistant", text: `已将${activeResource.kind === "browser" ? `浏览器演示页面：${activeResource.detail}` : `工作区文件：${activeResource.title}`}添加到对话上下文（演示内容）。`, time }] })); setActiveTab("chat"); };
  const navigate = (e: React.FormEvent) => { e.preventDefault(); if (!activeResource || !browserDraft.trim()) return; const url = /^https?:\/\//i.test(browserDraft) ? browserDraft : `https://${browserDraft}`; const pageTitle = new URL(url).hostname.replace(/^www\./, ""); updateResources((items) => items.map((r) => r.id === activeResource.id ? { ...r, title: pageTitle, detail: url } : r)); setBrowserDraft(url); showNotice("已打开模拟页面：演示内容，未连接真实网站"); };
  const toggleMenu = (name: string, event?: React.MouseEvent) => {
    event?.stopPropagation();
    setShowAdd(false);
    setShowModels(false);
    setMenu((current) => current === name ? "" : name);
  };
  const openExistingOrNew = (kind: Kind) => {
    const existing = resources.find((resource) => resource.kind === kind);
    if (existing) { setActiveTab(existing.id); setShowAdd(false); }
    else addResource(kind);
  };
  const runMenuAction = (action: string) => {
    setMenu(""); setShowAdd(false);
    if (action.startsWith("resource:")) { setActiveTab(action.slice("resource:".length)); return; }
    switch (action) {
      case "new-chat": startNewConversation(); break;
      case "new-terminal": addResource("terminal"); break;
      case "open-terminal": openExistingOrNew("terminal"); break;
      case "open-browser": openExistingOrNew("browser"); break;
      case "open-file": openExistingOrNew("file"); break;
      case "close-resource": if (activeResource) closeResource(activeResource.id); break;
      case "settings": showNotice("设置（演示）：浅色外观与 Node 连接选项"); break;
      case "node-status": showNotice("Node 连接状态：模拟在线 · 未连接真实 Node"); break;
      case "appearance": showNotice("当前外观：浅色主题（演示）"); break;
      case "about": showNotice("DAgents Desktop · 原型演示"); break;
      case "about-prototype": showNotice("此页面为 DAgents Desktop 可操作交互原型"); break;
      case "shortcuts": showNotice("原型快捷键：Ctrl+N 新建对话 · Ctrl+O 文件 · Ctrl+Shift+B 浏览器 · Ctrl+Shift+T 终端 · Ctrl+B 侧栏 · Ctrl+W 关闭资源 · Ctrl+Enter 发送草稿"); break;
      case "toggle-sidebar": setCollapsed((value) => !value); setSideGroup(""); break;
      case "back-chat": setActiveTab("chat"); setStatusOpen(false); break;
      case "agent-status": showNotice(`${activeAgent}：${isGenerating ? "生成中" : approval === "pending" ? "待审批" : "空闲"}（演示状态）`); break;
      case "send-draft": sendMessage(); break;
      case "review-approval": setActiveTab("chat"); setStatusOpen(false); showNotice(activeTab === "chat" ? "待审批卡已显示在当前对话中" : "已返回对话查看待审批操作"); break;
      case "select-all": {
        const selector = activeTab === "chat" ? ".message-composer input" : activeResource?.kind === "terminal" ? ".terminal-command input" : ".resource-composer textarea";
        const input = document.querySelector<HTMLInputElement | HTMLTextAreaElement>(selector);
        input?.focus(); input?.select();
        break;
      }
      case "clear-input": {
        if (activeTab !== "chat" && activeResource?.kind === "terminal") setTerminalInput("");
        else setDraft("");
        break;
      }
      default: break;
    }
  };
  const getMenuGroups = (name: string) => {
    const groupsForMenu = (menuGroups[name] ?? []).map((group) => group.map((item) => {
      if (item.action === "toggle-sidebar") return { ...item, label: collapsed ? "展开侧栏" : "收起侧栏", hint: collapsed ? "当前已收起" : "当前已展开" };
      return item;
    }));
    if (name === "视图") groupsForMenu.push([{ label: "对话", action: "back-chat", hint: "固定页签" }, ...resources.map((resource) => ({ label: resource.title, action: `resource:${resource.id}`, hint: resource.kind }))]);
    return groupsForMenu;
  };
  const renderTopMenu = (name: string) => <div className="top-menu-popover" role="group" aria-label={`${name}菜单`} onClick={(event) => event.stopPropagation()}>
    {getMenuGroups(name).map((group, groupIndex) => <div className="top-menu-group" key={`${name}-${groupIndex}`}>
      {groupIndex > 0 && <div className="top-menu-separator" />}
      {group.map((item) => {
        const disabled = item.action === "undo" || item.action === "redo" || (item.action === "close-resource" && !activeResource) || (item.action === "send-draft" && !draft.trim()) || (item.action === "review-approval" && approval !== "pending") || (item.action === "back-chat" && activeTab === "chat");
        return <button type="button" className="top-menu-item" key={`${item.action}-${item.label}`} disabled={disabled} onClick={() => runMenuAction(item.action)}><span>{item.label}</span><span className="top-menu-meta">{item.shortcut ?? item.hint ?? ""}</span></button>;
      })}
    </div>)}
  </div>;
  useEffect(() => {
    const onShortcut = (event: KeyboardEvent) => {
      if (!(event.ctrlKey || event.metaKey)) return;
      const key = event.key.toLowerCase();
      if (key === "n") { event.preventDefault(); runMenuAction("new-chat"); }
      else if (key === "b" && event.shiftKey) { event.preventDefault(); runMenuAction("open-browser"); }
      else if (key === "b") { event.preventDefault(); runMenuAction("toggle-sidebar"); }
      else if (key === "t" && event.shiftKey) { event.preventDefault(); runMenuAction("new-terminal"); }
      else if (key === "o") { event.preventDefault(); runMenuAction("open-file"); }
      else if (key === "w" && activeResource) { event.preventDefault(); runMenuAction("close-resource"); }
      else if (key === "enter" && draft.trim()) { event.preventDefault(); runMenuAction("send-draft"); }
    };
    window.addEventListener("keydown", onShortcut);
    return () => window.removeEventListener("keydown", onShortcut);
  }, [activeResource, draft, runMenuAction]);

  return <main className="desktop-shell" onClick={(event) => { if (showAdd) setShowAdd(false); if (showModels && !(event.target as HTMLElement).closest(".model-wrap")) setShowModels(false); if (menu) setMenu(""); }}>
    <header className="menubar" onClick={(event) => event.stopPropagation()}><div className="menu-items"><button className={`header-sidebar-toggle ${collapsed ? "is-collapsed" : ""}`} aria-label={collapsed ? "展开侧栏" : "收起侧栏"} title={collapsed ? "展开侧栏" : "收起侧栏"} onClick={() => { setCollapsed(!collapsed); setSideGroup(""); setShowAdd(false); setMenu(""); }}>{icon(collapsed ? "panel-left-open" : "panel-left-close")}</button>{topMenus.map((name) => <div className="top-menu-wrap" key={name}><button type="button" className={`menu-button ${menu === name ? "open" : ""}`} aria-haspopup="true" aria-expanded={menu === name} onClick={(event) => toggleMenu(name, event)}>{name}</button>{menu === name && renderTopMenu(name)}</div>)}</div><div className="window-controls"><button type="button" aria-label="最小化">−</button><button type="button" aria-label="最大化">□</button><button type="button" aria-label="关闭">×</button></div></header>
    <div className={`app-layout ${collapsed ? "sidebar-collapsed" : ""}`}>
      <aside className="sidebar">
        <div className="side-scroll">{groups.map((g) => <section className="side-section" key={g.id}>
          {collapsed ? <><button className={`collapsed-group ${sideGroup === g.id ? "selected" : ""}`} aria-label={`${g.label}，${g.items.length}项`} title={g.label} onClick={() => setSideGroup(sideGroup === g.id ? "" : g.id)}>{icon(g.glyph)}</button>{sideGroup === g.id && <div className="sidebar-popover"><b>{g.label}<small>{g.items.length}</small></b>{g.items.map((item, i) => <button key={item} className={activeAgent === item ? "selected" : ""} onClick={() => switchAgent(item)}>{icon(g.id === "agents" ? "agent" : g.id === "auto" ? "auto" : "workgroup")}<span>{item}</span>{activeAgent === item && <i>当前</i>}</button>)}</div>}</> : <><div className="side-heading">{icon(g.glyph)}<span>{g.label}</span><span className="count">{g.items.length}</span></div><div className="agent-list">{g.items.map((item, i) => <button key={item} className={`agent-row ${activeAgent === item ? "selected" : ""}`} onClick={() => switchAgent(item)}><span className="agent-icon">{icon(g.id === "agents" ? "agent" : g.id === "auto" ? "auto" : "workgroup")}</span><span>{item}</span></button>)}</div></>}
        </section>)}</div>
        <div className="sidebar-bottom"><div className="node-status" title="Node 连接状态：模拟在线" aria-label="Node 连接状态：模拟在线"><span>Node · 模拟在线</span><i /></div></div>
      </aside>
      {collapsed && sideGroup && <button className="popover-dismiss" aria-label="关闭对象列表" onClick={() => setSideGroup("")} />}
      <section className="workspace">
        <div className="workspace-nav">
          <button type="button" className={`conversation-tab ${activeTab === "chat" ? "active" : ""}`} onClick={() => { setActiveTab("chat"); setStatusOpen(false); }} title={activeAgent} aria-label={`对话：${activeAgent}`}>
            {icon("agent")}<b>{activeAgent}</b>
          </button>
          <div className="resource-tabs">{resources.map((r) => <div className={`resource-tab ${activeTab === r.id ? "active" : ""}`} key={r.id}>
            <button type="button" className="resource-tab-main" title={r.title} aria-label={r.title} onClick={() => { setActiveTab(r.id); setShowAdd(false); setMenu(""); }}>{icon(resourceIcon(r))}<span>{r.title}</span></button>
            <button type="button" className="tab-close" aria-label={`关闭${r.title}`} title={`关闭${r.title}`} onClick={() => closeResource(r.id)}>{icon("circle-x")}</button>
          </div>)}</div>
          <div className="nav-actions">
            <div className="resource-entry-wrap"><button type="button" className="resource-entry" aria-expanded={menu === "resource-switcher"} onClick={(event) => toggleMenu("resource-switcher", event)}>{icon("folder-open")}资源 · {resources.length}{icon("chevron-down")}</button>{menu === "resource-switcher" && <div className="resource-switcher-menu"><b>切换资源</b>{resources.length ? resources.map((r) => <button type="button" key={r.id} className={activeTab === r.id ? "selected" : ""} onClick={() => { setActiveTab(r.id); setMenu(""); }}>{icon(resourceIcon(r))}<span>{r.title}</span>{activeTab === r.id && <i>当前</i>}</button>) : <span className="empty-resource-menu">暂无已打开资源</span>}</div>}</div>
            {activeResource?.kind === "terminal" && <div className="resource-tool-menu"><button type="button" className="more-button" aria-label="终端选项" title="终端选项" aria-expanded={menu === "terminal"} onClick={(event) => toggleMenu("terminal", event)}>{icon("more")}</button>{menu === "terminal" && <div className="terminal-menu"><button type="button" onClick={() => { updateTerminalLines([]); setMenu(""); }}>清空终端</button><button type="button" onClick={() => { updateTerminalLines((lines) => [...lines, "终端会话已重新连接（演示）"]); setMenu(""); }}>重新连接</button></div>}</div>}
            <div className="add-tab-wrap"><button type="button" className="add-tab" aria-label="新增资源" title="新增资源" aria-expanded={showAdd} onClick={(event) => { event.stopPropagation(); setMenu(""); setShowModels(false); setShowAdd(!showAdd); }}>{icon("create")}</button>{showAdd && <div className="add-popover"><b>新建资源</b><button type="button" onClick={() => addResource("terminal")}>{icon("terminal")}新建终端</button><button type="button" onClick={() => openExistingOrNew("browser")}>{icon("browser")}打开浏览器</button><button type="button" onClick={() => openExistingOrNew("file")}>{icon("file")}打开文件</button></div>}</div>
          </div>
        </div>
        {activeTab === "chat" ? <section className="chat-view"><div className="conversation">{messages.length ? <><div className="history-day">今天 · 10:41</div>{messages.map((m, i) => <article key={i} className={`message ${m.role}`}><div className="message-content"><p>{m.text}</p></div><time className="message-time">{m.time}</time></article>)}{approval !== "none" && <ApprovalCard approval={approval} onDecide={decideApproval}/>}</> : <div className={`empty-conversation ${activeAgent.startsWith("Auto") ? "auto-empty" : ""}`}>{activeAgent.startsWith("Auto") ? <><b>{activeAgent} 暂无任务记录</b><span>在下方发送任务开始 · 当前为交互原型</span></> : <>这是 {activeAgent} 的独立演示对话，发送消息开始。</>}</div>}</div><MessageComposer draft={draft} setDraft={setDraft} onSend={sendMessage} model={model} setModel={setModel} showModels={showModels} setShowModels={setShowModels}/></section> : activeResource?.kind === "terminal" ? <><div className={`terminal-area ${statusOpen ? "status-drawer-open" : ""}`}><div className="terminal-demo-label">本地终端演示 · 命令仅生成模拟输出</div><div className="terminal-output">{terminalLines.map((l, i) => <div key={i} className="terminal-line">{l}</div>)}<form className="terminal-command" onSubmit={(e) => { e.preventDefault(); if (!terminalInput.trim()) return; const c = terminalInput.trim(); updateTerminalLines((l) => [...l, `user@dev-machine:~/projects$ ${c}`, c === "pwd" ? "/home/user/projects" : c === "ls" ? "README.md  package.json  src/" : `bash: ${c.split(" ")[0]}: command simulated`]); setTerminalInput(""); }}><span>user@dev-machine:~/projects$</span><input value={terminalInput} onChange={(e) => setTerminalInput(e.target.value)} aria-label="终端命令" autoComplete="off"/><button aria-label="执行命令">{icon("play", "terminal-run-icon")}</button></form></div></div></> : activeResource ? <><div className="resource-toolbar unified-toolbar">{activeResource.kind === "browser" ? <form className="address-bar" onSubmit={navigate}><span className="address-icon">{icon("browser")}</span><input value={activeResource.detail.startsWith("http") ? activeResource.detail : browserDraft} onChange={(e) => setBrowserDraft(e.target.value)} aria-label="浏览器网址" placeholder="输入网址，例如 example.com"/><button type="submit">前往</button></form> : <div className="file-toolbar-title">{icon("file")}<b>{activeResource.title}</b><span>只读 · 演示内容</span></div>}<button type="button" className="more-button" aria-label="资源选项" title="资源选项" aria-expanded={menu === "resource"} onClick={(event) => toggleMenu("resource", event)}>{icon("more")}</button>{menu === "resource" && <div className="resource-options-popover" onClick={(event) => event.stopPropagation()}><button type="button" onClick={cite}>在对话中引用</button><button type="button" onClick={() => { setMenu(""); closeResource(activeResource.id); }}>关闭此资源</button></div>}</div><div className={`resource-content ${activeResource.kind}-resource-content`}>{activeResource.kind === "browser" ? <div className="browser-page"><span className="demo-badge">演示内容 · 未连接真实网站</span><h1>{activeResource.detail.startsWith("http") ? "模拟页面预览" : "输入网址开始预览"}</h1><p>{activeResource.detail.startsWith("http") ? "已加载本地演示页面" : "输入任意网址后点击“前往”，查看模拟导航结果。"}</p>{activeResource.detail.startsWith("http") && <div className="demo-url">{activeResource.detail}</div>}<button onClick={cite} disabled={!activeResource.detail.startsWith("http")}>在对话中引用此演示页面</button></div> : <div className="file-mock"><div className="file-meta">此文件为原型中的只读示例。操作不会修改工作区文件。</div><pre>{"# my-app\n\n项目工作区文件（演示内容）\n\n- package.json\n- src/\n- README.md"}</pre><button className="file-cite-button" onClick={cite}>在对话中引用文件</button></div>}</div></> : null}
                {activeTab !== "chat" && <aside className={`agent-dock ${statusOpen ? "expanded" : ""}`}>
          <div className="dock-topline"><button className="dock-agent" onClick={() => setStatusOpen(!statusOpen)} aria-expanded={statusOpen}><span className={`status-dot ${approval === "pending" ? "pending" : isGenerating ? "generating" : ""}`}/><b>{activeAgent}</b><span className={`status-label ${approval === "pending" ? "pending" : isGenerating ? "generating" : approval}`}>{approval === "pending" ? "待审批 1" : isGenerating ? "生成中" : approval === "allowed" ? "已允许一次" : approval === "denied" ? "已拒绝" : "空闲"}</span>{icon(statusOpen ? "chevron-down" : "chevron-up", "dock-chevron")}</button></div>
          <form className={`resource-composer ${draft.includes("\n") ? "multi-line" : ""}`} onSubmit={(e) => { e.preventDefault(); sendMessage(); }}><textarea value={draft} rows={1} onChange={(e) => setDraft(e.target.value)} onKeyDown={(e) => { if (e.key === "Enter" && !e.shiftKey) { e.preventDefault(); sendMessage(); } }} aria-label="给当前 Agent 发送消息" placeholder={`发消息给 ${activeAgent}…`} style={{ height: Math.min(56, 32 + (draft.match(/\n/g)?.length ?? 0) * 18) }}/><button aria-label="发送消息" title="发送消息">{icon("send")}</button></form>
          {statusOpen && <div className="dock-expanded">
            {approval === "pending" && <div className="dock-approval"><h3>待审批操作</h3><ApprovalCard approval={approval} onDecide={decideApproval}/></div>}
            <div className="dock-details"><div className="dock-section"><h3>最近消息</h3><div className="recent-message-list">{messages.slice(-2).map((m, i) => <div className={`recent-message ${m.role}`} key={`${m.time}-${i}`}><span>{m.role === "user" ? "你" : "Agent"}</span><p>{m.text}</p><time>{m.time}</time></div>)}{isGenerating && <div className="recent-message assistant generating"><span>Agent</span><p>正在生成回复…</p></div>}</div></div><div className="dock-section"><h3>最近工具</h3><p className="tool-activity">{approval === "pending" ? "terminal · npm run build · 等待审批" : isGenerating ? `模型 · ${model} 正在生成回复` : approval === "allowed" ? "terminal · 已授权，等待 Agent 继续" : approval === "denied" ? "审批已拒绝 · 执行已停止" : "terminal · 当前会话空闲"}</p></div>{approval !== "none" && approval !== "pending" && <div className="dock-section"><h3>审批与操作</h3><ApprovalCard approval={approval} onDecide={decideApproval}/></div>}</div>
            <button className="view-chat" onClick={() => { setActiveTab("chat"); setStatusOpen(false); }}>返回完整对话 {icon("message")}</button>
          </div>}
        </aside>}
      </section>
    </div>{notice && <div className="notice-toast" role="status">{notice}</div>}
  </main>;
}

function ApprovalCard({ approval, onDecide }: { approval: Approval; onDecide: (v: "allowed" | "denied") => void }) {
  return <div className={`approval-card ${approval}`}><div className="approval-heading">{icon(approval === "pending" ? "pending" : approval === "allowed" ? "success" : "error")}<b>{approval === "pending" ? "需要审批" : approval === "allowed" ? "已允许一次" : "已拒绝"}</b><span>· 安装依赖</span></div><code>npm i -D @vitejs/plugin-vue</code><p>Node：dev-machine　影响范围：项目依赖</p>{approval === "pending" && <div className="approval-actions"><button className="allow-button" onClick={() => onDecide("allowed")}>允许一次</button><button className="deny-button" onClick={() => onDecide("denied")}>拒绝</button></div>}</div>;
}
type ComposerProps = { draft: string; setDraft: (v: string) => void; onSend: () => void; model: string; setModel: (v: string) => void; showModels: boolean; setShowModels: (v: boolean) => void };
function MessageComposer({ draft, setDraft, onSend, model, setModel, showModels, setShowModels }: ComposerProps) {
  return <div className="composer-zone"><form className="message-composer" onSubmit={(e) => { e.preventDefault(); onSend(); }}><button type="button" className="attach-button" aria-label="添加附件" title="添加附件">{icon("attachment")}</button><input placeholder="发送消息…" value={draft} onChange={(e) => setDraft(e.target.value)} aria-label="发送消息"/><div className="model-wrap"><button type="button" className="model-select" onClick={() => setShowModels(!showModels)}>{model}{icon("chevron-down")}</button>{showModels && <div className="model-popover">{["deepseek-chat", "deepseek-reasoner", "gpt-4o"].map((m) => <button key={m} type="button" onClick={() => { setModel(m); setShowModels(false); }}>{m}{model === m && icon("circle-check")}</button>)}</div>}</div><button className="send-button" aria-label="发送消息" title="发送消息">{icon("send")}</button></form></div>;
}
