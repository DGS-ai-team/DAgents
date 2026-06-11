const TOKEN_KEY = "dagents_rc_token";
const API_PREFIX = "/v1";

const state = {
  page: 1,
  total: 0,
  pageSize: 50,
  isAdmin: null,
  agents: [],
  selected: null,
};

const els = {
  healthPill: document.getElementById("health-pill"),
  btnSettings: document.getElementById("btn-settings"),
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
  settingsDialog: document.getElementById("settings-dialog"),
  settingsForm: document.getElementById("settings-form"),
  inputToken: document.getElementById("input-token"),
  btnClearToken: document.getElementById("btn-clear-token"),
  drawer: document.getElementById("detail-drawer"),
  drawerBackdrop: document.getElementById("drawer-backdrop"),
  btnCloseDrawer: document.getElementById("btn-close-drawer"),
  drawerTitle: document.getElementById("drawer-title"),
  drawerSubtitle: document.getElementById("drawer-subtitle"),
  drawerBody: document.getElementById("drawer-body"),
  linkEndpoint: document.getElementById("link-endpoint"),
};

function getToken() {
  return sessionStorage.getItem(TOKEN_KEY) || "";
}

function setToken(value) {
  const trimmed = (value || "").trim();
  if (trimmed) {
    sessionStorage.setItem(TOKEN_KEY, trimmed);
  } else {
    sessionStorage.removeItem(TOKEN_KEY);
  }
}

function initTokenFromURL() {
  const params = new URLSearchParams(window.location.search);
  const token = params.get("token");
  if (token) {
    setToken(token);
    params.delete("token");
    const next = `${window.location.pathname}${params.toString() ? `?${params}` : ""}`;
    window.history.replaceState({}, "", next);
  }
}

function authHeaders() {
  const token = getToken();
  return token ? { "x-dagents-a2a-token": token } : {};
}

async function apiFetch(path, params = {}) {
  const url = new URL(path, window.location.origin);
  Object.entries(params).forEach(([key, value]) => {
    if (value !== undefined && value !== null && value !== "") {
      url.searchParams.set(key, String(value));
    }
  });
  const resp = await fetch(url, { headers: { ...authHeaders(), Accept: "application/json" } });
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

function showError(message) {
  els.errorBanner.textContent = message;
  els.errorBanner.classList.remove("hidden");
}

function clearError() {
  els.errorBanner.classList.add("hidden");
  els.errorBanner.textContent = "";
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

function riskPill(level) {
  const cls =
    level === "high" ? "pill-risk-high" : level === "low" ? "pill-risk-low" : "pill-risk-medium";
  return `<span class="pill ${cls}">${level}</span>`;
}

function renderChips(items, limit = 3) {
  if (!items?.length) return '<span class="muted">—</span>';
  const visible = items.slice(0, limit);
  const extra = items.length > limit ? `<span class="chip">+${items.length - limit}</span>` : "";
  return `<span class="chips">${visible.map((x) => `<span class="chip" title="${escapeHtml(x)}">${escapeHtml(x)}</span>`).join("")}${extra}</span>`;
}

function escapeHtml(text) {
  return String(text)
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;");
}

function mergedCapabilities(agent) {
  const seen = new Set();
  const out = [];
  for (const item of [...(agent.capabilities || []), ...(agent.capabilities_hint || [])]) {
    if (!item || seen.has(item)) continue;
    seen.add(item);
    out.push(item);
  }
  return out;
}

function renderTable(agents) {
  if (!agents.length) {
    els.agentRows.innerHTML = '<tr><td colspan="10" class="empty">无匹配 Agent</td></tr>';
    return;
  }
  els.agentRows.innerHTML = agents
    .map(
      (agent, index) => `
      <tr tabindex="0" data-index="${index}" aria-label="查看 ${escapeHtml(agent.agent_id)}">
        <td><strong>${escapeHtml(agent.name || agent.agent_id)}</strong></td>
        <td><code>${escapeHtml(agent.agent_id)}</code></td>
        <td>${escapeHtml(agent.team || "—")}</td>
        <td>${statusPill(agent.status)}</td>
        <td>${escapeHtml(agent.version || "—")}</td>
        <td>${formatUnix(agent.last_seen_unix)}</td>
        <td>${riskPill(agent.risk_level || "medium")}</td>
        <td>${renderChips(mergedCapabilities(agent))}</td>
        <td>${renderChips(agent.tools)}</td>
        <td>${renderChips(agent.skills)}</td>
      </tr>`,
    )
    .join("");

  els.agentRows.querySelectorAll("tr[data-index]").forEach((row) => {
    row.addEventListener("click", () => openDrawer(state.agents[Number(row.dataset.index)]));
    row.addEventListener("keydown", (event) => {
      if (event.key === "Enter" || event.key === " ") {
        event.preventDefault();
        openDrawer(state.agents[Number(row.dataset.index)]);
      }
    });
  });
}

function renderPager() {
  const totalPages = Math.max(1, Math.ceil(state.total / state.pageSize));
  els.pagerLabel.textContent = `第 ${state.page} / ${totalPages} 页（共 ${state.total} 条）`;
  els.btnPrev.disabled = state.page <= 1;
  els.btnNext.disabled = state.page >= totalPages;
}

function kv(label, value, mono = false) {
  return `<div class="kv"><div class="kv-label">${escapeHtml(label)}</div><div class="kv-value${mono ? " mono" : ""}">${value}</div></div>`;
}

async function loadA2AForAgent(agentId) {
  try {
    const data = await apiFetch(`${API_PREFIX}/admin/a2a/recent`, { limit: 100 });
    return (data.entries || []).filter(
      (entry) => entry.target_agent_id === agentId || entry.operation === "broadcast",
    );
  } catch (err) {
    if (err.status === 403) {
      return { forbidden: true };
    }
    return { error: err.message };
  }
}

async function openDrawer(agent) {
  state.selected = agent;
  els.drawerTitle.textContent = agent.name || agent.agent_id;
  els.drawerSubtitle.textContent = agent.agent_id;
  els.linkEndpoint.href = agent.base_url;
  els.linkEndpoint.textContent = agent.base_url;

  const groups = (agent.discovery_group || []).map((g) => `<span class="chip">${escapeHtml(g)}</span>`).join(" ");
  const scopes = (agent.allowed_scopes || []).map((s) => `<span class="chip">${escapeHtml(s)}</span>`).join(" ") || "—";

  els.drawerBody.innerHTML = `
    <div class="kv-grid">
      ${kv("status", statusPill(agent.status))}
      ${kv("team", escapeHtml(agent.team || "—"))}
      ${kv("owner", escapeHtml(agent.owner || "—"))}
      ${kv("description", escapeHtml(agent.description || "—"))}
      ${kv("discovery_group", groups)}
      ${kv("base_url", `<a href="${escapeHtml(agent.base_url)}" target="_blank" rel="noopener">${escapeHtml(agent.base_url)}</a>`)}
      ${kv("version", escapeHtml(agent.version || "—"))}
      ${kv("auth_method", escapeHtml(agent.auth_method || "—"))}
      ${kv("risk_level", riskPill(agent.risk_level || "medium"))}
      ${kv("allowed_scopes", scopes)}
      ${kv("registered_at", formatUnix(agent.registered_at_unix))}
      ${kv("last_seen", formatUnix(agent.last_seen_unix))}
      ${kv("expires_at", formatUnix(agent.expires_at_unix))}
      ${kv("capabilities", renderChips(mergedCapabilities(agent), 20))}
      ${kv("tools", renderChips(agent.tools, 20))}
      ${kv("skills", renderChips(agent.skills, 20))}
      ${kv("last_error", escapeHtml(agent.last_error_summary || "—"))}
      ${kv("recent_task", escapeHtml(agent.recent_task_summary || "—"))}
    </div>
    <div class="section-title">近期 A2A（RC 侧）</div>
    <div id="a2a-panel" class="muted">加载中…</div>
  `;

  els.drawer.classList.remove("hidden");
  els.drawer.setAttribute("aria-hidden", "false");

  const a2aPanel = document.getElementById("a2a-panel");
  const a2a = await loadA2AForAgent(agent.agent_id);
  if (a2a.forbidden) {
    a2aPanel.innerHTML = "需要 admin token 才能查看 A2A 摘要。";
    return;
  }
  if (a2a.error) {
    a2aPanel.textContent = a2a.error;
    return;
  }
  if (!a2a.length) {
    a2aPanel.textContent = "暂无记录。";
    return;
  }
  a2aPanel.innerHTML = `<ul class="a2a-list">${a2a
    .slice(0, 8)
    .map(
      (entry) => `<li class="a2a-item">
        <strong>${escapeHtml(entry.operation)} · ${escapeHtml(entry.final_state)}</strong>
        trace: <span class="mono">${escapeHtml(entry.trace_id)}</span><br />
        ${entry.latency_ms} ms · ${formatUnix(entry.finished_at_unix)}
        ${entry.error_summary ? `<br />${escapeHtml(entry.error_summary)}` : ""}
      </li>`,
    )
    .join("")}</ul>`;
}

function closeDrawer() {
  els.drawer.classList.add("hidden");
  els.drawer.setAttribute("aria-hidden", "true");
  state.selected = null;
}

async function refreshHealth() {
  try {
    const data = await apiFetch("/health");
    els.healthPill.textContent = `RC ok · ${data.agents} agents`;
    els.healthPill.className = "pill pill-online";
  } catch {
    els.healthPill.textContent = "RC 不可达";
    els.healthPill.className = "pill pill-offline";
  }
}

async function loadAgents() {
  clearError();
  state.pageSize = Number(els.filterPageSize.value) || 50;
  const params = {
    team: els.filterTeam.value.trim(),
    status: els.filterStatus.value,
    q: els.filterQ.value.trim(),
    page: state.page,
    page_size: state.pageSize,
  };
  const group = els.filterGroup.value.trim();
  if (group) {
    params.discovery_group = group;
  }

  try {
    const data = await apiFetch(`${API_PREFIX}/agents`, params);
    state.agents = data.agents || [];
    state.total = data.total ?? state.agents.length;
    state.page = data.page || state.page;
    state.isAdmin = !group || state.isAdmin === true;
    els.roleHint.textContent = group
      ? `筛选分组：${group}`
      : "admin 全局视图（未指定 discovery_group）";
    els.listSummary.textContent = `显示 ${state.agents.length} 条，合计 ${state.total} 条`;
    renderTable(state.agents);
    renderPager();
  } catch (err) {
    if (err.status === 422 && !group) {
      state.isAdmin = false;
      els.roleHint.textContent = "member token：请填写 discovery_group";
    }
    showError(err.message);
    els.agentRows.innerHTML = '<tr><td colspan="10" class="empty">加载失败</td></tr>';
  }
}

function openSettings() {
  els.inputToken.value = getToken();
  els.settingsDialog.showModal();
}

els.btnSettings.addEventListener("click", openSettings);
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
els.settingsForm.addEventListener("submit", (event) => {
  event.preventDefault();
  setToken(els.inputToken.value);
  els.settingsDialog.close();
  state.page = 1;
  loadAgents();
});
els.btnClearToken.addEventListener("click", () => {
  els.inputToken.value = "";
  setToken("");
});
els.btnCloseDrawer.addEventListener("click", closeDrawer);
els.drawerBackdrop.addEventListener("click", closeDrawer);
document.addEventListener("keydown", (event) => {
  if (event.key === "Escape" && !els.drawer.classList.contains("hidden")) {
    closeDrawer();
  }
});

let debounceTimer;
[els.filterGroup, els.filterTeam, els.filterQ].forEach((el) => {
  el.addEventListener(
    "input",
    () => {
      clearTimeout(debounceTimer);
      debounceTimer = setTimeout(() => {
        state.page = 1;
        loadAgents();
      }, 350);
    },
  );
});
[els.filterStatus, els.filterPageSize].forEach((el) => {
  el.addEventListener("change", () => {
    state.page = 1;
    loadAgents();
  });
});

initTokenFromURL();
refreshHealth();
if (!getToken()) {
  openSettings();
} else {
  loadAgents();
}
