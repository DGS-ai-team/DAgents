const REGISTRY_API = "/v1/registry/agents";

const state = { page: 1, total: 0, pageSize: 50, agents: [], statsAgents: [] };

const els = {
  healthPill: document.getElementById("health-pill"),
  btnRefresh: document.getElementById("btn-refresh"),
  filterGroup: document.getElementById("filter-group"),
  filterTeam: document.getElementById("filter-team"),
  filterStatus: document.getElementById("filter-status"),
  filterQ: document.getElementById("filter-q"),
  filterPageSize: document.getElementById("filter-page-size"),
  listSummary: document.getElementById("list-summary"),
  roleHint: document.getElementById("role-hint"),
  agentRows: document.getElementById("agent-rows"),
  errorBanner: document.getElementById("error-banner"),
  btnPrev: document.getElementById("btn-prev"),
  btnNext: document.getElementById("btn-next"),
  pagerLabel: document.getElementById("pager-label"),
  drawer: document.getElementById("detail-drawer"),
  drawerBackdrop: document.getElementById("drawer-backdrop"),
  btnCloseDrawer: document.getElementById("btn-close-drawer"),
  drawerTitle: document.getElementById("drawer-title"),
  drawerSubtitle: document.getElementById("drawer-subtitle"),
  drawerBody: document.getElementById("drawer-body"),
  linkEndpoint: document.getElementById("link-endpoint"),
  statOnline: document.getElementById("stat-online"),
  statOffline: document.getElementById("stat-offline"),
  statTotal: document.getElementById("stat-total"),
  statPeers: document.getElementById("stat-peers"),
};

async function apiFetch(path, params = {}, options = {}) {
  const url = new URL(path, window.location.origin);
  Object.entries(params).forEach(([key, value]) => {
    if (value !== undefined && value !== null && value !== "") {
      url.searchParams.set(key, String(value));
    }
  });
  const method = options.method || "GET";
  const headers = { Accept: "application/json" };
  const init = { method, headers };
  if (options.body !== undefined) {
    headers["Content-Type"] = "application/json";
    init.body = JSON.stringify(options.body);
  }
  const resp = await fetch(url, init);
  let body = null;
  try {
    body = await resp.json();
  } catch {
    body = null;
  }
  if (!resp.ok) {
    const detail = body?.detail;
    const message = typeof detail === "string" ? detail : `HTTP ${resp.status}`;
    const err = new Error(message);
    err.status = resp.status;
    throw err;
  }
  return body;
}

function parseGroupInput(raw) {
  return String(raw || "")
    .split(/[,，\s]+/)
    .map((item) => item.trim())
    .filter(Boolean);
}

async function saveAgentGroups(agentId, raw) {
  const discovery_group = parseGroupInput(raw);
  if (!discovery_group.length) {
    throw new Error("至少填写一个 discovery_group");
  }
  return apiFetch(
    `/v1/registry/agents/${encodeURIComponent(agentId)}/groups`,
    {},
    { method: "PATCH", body: { discovery_group } },
  );
}

function escapeHtml(text) {
  return String(text)
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;");
}

function formatUnix(ts) {
  if (!ts) return "—";
  return new Date(ts * 1000).toLocaleString("zh-CN", { hour12: false });
}

function statusPill(status) {
  const cls =
    status === "online" ? "pill-online" : status === "offline" ? "pill-offline" : "pill-expired";
  return `<span class="pill ${cls}">${status}</span>`;
}

function boolPill(value, yes = "是", no = "否") {
  return `<span class="pill ${value ? "pill-yes" : "pill-no"}">${value ? yes : no}</span>`;
}

function riskPill(level) {
  const cls =
    level === "high" ? "pill-risk-high" : level === "low" ? "pill-risk-low" : "pill-risk-medium";
  return `<span class="pill ${cls}">${level || "medium"}</span>`;
}

function renderChips(items, limit = 3) {
  if (!items?.length) return '<span class="muted">—</span>';
  const visible = items.slice(0, limit);
  const extra = items.length > limit ? `<span class="chip">+${items.length - limit}</span>` : "";
  return `<span class="chips">${visible.map((x) => `<span class="chip">${escapeHtml(x)}</span>`).join("")}${extra}</span>`;
}

function updateStats(agents) {
  const online = agents.filter((a) => a.status === "online").length;
  const offline = agents.filter((a) => a.status === "offline").length;
  const peers = agents.filter((a) => a.expose_to_peers && a.status === "online").length;
  els.statOnline.textContent = String(online);
  els.statOffline.textContent = String(offline);
  els.statTotal.textContent = String(agents.length);
  els.statPeers.textContent = String(peers);
}

function renderTable(agents) {
  if (!agents.length) {
    els.agentRows.innerHTML = '<tr><td colspan="10" class="empty">无匹配 Node</td></tr>';
    return;
  }
  els.agentRows.innerHTML = agents
    .map(
      (agent, index) => `
      <tr tabindex="0" data-index="${index}">
        <td><strong>${escapeHtml(agent.name || agent.agent_id)}</strong></td>
        <td><code>${escapeHtml(agent.agent_id)}</code></td>
        <td>${escapeHtml(agent.team || "—")}</td>
        <td>${statusPill(agent.status)}</td>
        <td>${escapeHtml(agent.version || "—")}</td>
        <td>${formatUnix(agent.last_seen_unix)}</td>
        <td>${boolPill(agent.expose_to_peers)}</td>
        <td>${riskPill(agent.risk_level)}</td>
        <td>${renderChips(agent.tools)}</td>
        <td>${renderChips(agent.discovery_group, 2)}</td>
      </tr>`,
    )
    .join("");

  els.agentRows.querySelectorAll("tr[data-index]").forEach((row) => {
    row.addEventListener("click", () => openDrawer(state.agents[Number(row.dataset.index)]));
  });
}

function renderPager() {
  const totalPages = Math.max(1, Math.ceil(state.total / state.pageSize));
  els.pagerLabel.textContent = `第 ${state.page} / ${totalPages} 页（共 ${state.total} 条）`;
  els.btnPrev.disabled = state.page <= 1;
  els.btnNext.disabled = state.page >= totalPages;
}

function kv(label, value) {
  return `<div class="kv"><div class="kv-label">${escapeHtml(label)}</div><div class="kv-value">${value}</div></div>`;
}

async function loadStatsSnapshot(group) {
  try {
    const params = { status: "all", page: 1, page_size: 200 };
    if (group) params.discovery_group = group;
    const data = await apiFetch(REGISTRY_API, params);
    state.statsAgents = data.agents || [];
    updateStats(state.statsAgents);
  } catch {
    updateStats(state.agents);
  }
}

async function loadAuditForAgent(agentId) {
  try {
    const data = await apiFetch("/v1/admin/audit", { limit: 100 });
    return (data.events || []).filter((e) => e.target_agent_id === agentId).slice(0, 8);
  } catch (err) {
    return { error: err.message };
  }
}

async function openDrawer(agent) {
  els.drawerTitle.textContent = agent.name || agent.agent_id;
  els.drawerSubtitle.textContent = agent.agent_id;
  els.linkEndpoint.href = agent.base_url;
  els.linkEndpoint.textContent = agent.base_url;

  const groupValue = (agent.discovery_group || []).join(", ");

  els.drawerBody.innerHTML = `
    <div class="kv-grid">
      ${kv("status", statusPill(agent.status))}
      ${kv("expose_to_peers", boolPill(agent.expose_to_peers))}
      ${kv("team / owner", `${escapeHtml(agent.team || "—")} / ${escapeHtml(agent.owner || "—")}`)}
      ${kv("description", escapeHtml(agent.description || "—"))}
      ${kv("base_url", `<a href="${escapeHtml(agent.base_url)}" target="_blank" rel="noopener">${escapeHtml(agent.base_url)}</a>`)}
      ${kv("version", escapeHtml(agent.version || "—"))}
      ${kv("risk_level", riskPill(agent.risk_level))}
      ${kv("registered_at", formatUnix(agent.registered_at_unix))}
      ${kv("last_seen", formatUnix(agent.last_seen_unix))}
      ${kv("expires_at", formatUnix(agent.expires_at_unix))}
      ${kv("tools", renderChips(agent.tools, 20))}
      ${kv("skills", renderChips(agent.skills, 20))}
      ${kv("last_error", escapeHtml(agent.last_error_summary || "—"))}
      ${kv("recent_task", escapeHtml(agent.recent_task_summary || "—"))}
    </div>
    <div class="section-title">discovery_group（Manage 分配）</div>
    <div class="groups-editor">
      <label class="field field-grow">
        <span>分组（逗号分隔）</span>
        <input id="edit-groups" type="text" value="${escapeHtml(groupValue)}" placeholder="ops, staging" autocomplete="off" />
      </label>
      <button type="button" id="btn-save-groups" class="btn btn-primary">保存分组</button>
      <span id="groups-save-msg" class="muted"></span>
    </div>
    <div class="section-title">近期 Registry 审计</div>
    <div id="audit-panel" class="muted">加载中…</div>
  `;

  els.drawer.classList.remove("hidden");
  els.drawer.setAttribute("aria-hidden", "false");

  document.getElementById("btn-save-groups").addEventListener("click", async () => {
    const msg = document.getElementById("groups-save-msg");
    const input = document.getElementById("edit-groups");
    msg.textContent = "保存中…";
    try {
      const updated = await saveAgentGroups(agent.agent_id, input.value);
      agent.discovery_group = updated.discovery_group || [];
      const idx = state.agents.findIndex((item) => item.agent_id === agent.agent_id);
      if (idx >= 0) state.agents[idx] = { ...state.agents[idx], ...updated };
      msg.textContent = "已保存";
      await loadAgents();
    } catch (err) {
      msg.textContent = err.message;
    }
  });

  const panel = document.getElementById("audit-panel");
  const audit = await loadAuditForAgent(agent.agent_id);
  if (audit.error) {
    panel.textContent = audit.error;
    return;
  }
  if (!audit.length) {
    panel.textContent = "暂无记录。";
    return;
  }
  panel.innerHTML = `<ul class="audit-list">${audit
    .map(
      (e) => `<li class="audit-item"><strong>${escapeHtml(e.action)}</strong>${formatUnix(e.at_unix)} · ${escapeHtml(e.actor)}</li>`,
    )
    .join("")}</ul>`;
}

function closeDrawer() {
  els.drawer.classList.add("hidden");
  els.drawer.setAttribute("aria-hidden", "true");
}

function showError(message) {
  els.errorBanner.textContent = message;
  els.errorBanner.classList.remove("hidden");
}

function clearError() {
  els.errorBanner.classList.add("hidden");
}

async function refreshHealth() {
  try {
    const data = await apiFetch("/health");
    els.healthPill.textContent = `Manage · ${data.agents} nodes`;
    els.healthPill.className = "pill pill-online";
  } catch {
    els.healthPill.textContent = "Manage 不可达";
    els.healthPill.className = "pill pill-offline";
  }
}

async function loadAgents() {
  clearError();
  state.pageSize = Number(els.filterPageSize.value) || 50;
  const group = els.filterGroup.value.trim();
  const params = {
    team: els.filterTeam.value.trim(),
    status: els.filterStatus.value,
    q: els.filterQ.value.trim(),
    page: state.page,
    page_size: state.pageSize,
  };
  if (group) params.discovery_group = group;

  try {
    const data = await apiFetch(REGISTRY_API, params);
    state.agents = data.agents || [];
    state.total = data.total ?? state.agents.length;
    state.page = data.page || state.page;
    els.roleHint.textContent = group ? `分组：${group}` : "全部 Node";
    els.listSummary.textContent = `本页 ${state.agents.length} 条，合计 ${state.total} 条`;
    renderTable(state.agents);
    renderPager();
    await loadStatsSnapshot(group);
  } catch (err) {
    showError(err.message);
    els.agentRows.innerHTML = '<tr><td colspan="10" class="empty">加载失败</td></tr>';
  }
}

els.btnRefresh.addEventListener("click", () => {
  state.page = 1;
  loadAgents();
});
els.btnPrev.addEventListener("click", () => {
  if (state.page > 1) {
    state.page -= 1;
    loadAgents();
  }
});
els.btnNext.addEventListener("click", () => {
  const totalPages = Math.max(1, Math.ceil(state.total / state.pageSize));
  if (state.page < totalPages) {
    state.page += 1;
    loadAgents();
  }
});
els.btnCloseDrawer.addEventListener("click", closeDrawer);
els.drawerBackdrop.addEventListener("click", closeDrawer);

let debounceTimer;
[els.filterGroup, els.filterTeam, els.filterQ].forEach((el) => {
  el.addEventListener("input", () => {
    clearTimeout(debounceTimer);
    debounceTimer = setTimeout(() => {
      state.page = 1;
      loadAgents();
    }, 350);
  });
});
[els.filterStatus, els.filterPageSize].forEach((el) => {
  el.addEventListener("change", () => {
    state.page = 1;
    loadAgents();
  });
});

refreshHealth();
loadAgents();
