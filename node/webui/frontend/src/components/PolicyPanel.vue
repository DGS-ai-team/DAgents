<script setup>
import { computed, onMounted, ref } from "vue";
import * as api from "../api/node.js";
import {
  POLICY_MODES,
  PROTECTED_POLICY_TOOL,
  applyLocalShellUpdate,
  applyLocalToolUpdate,
  canSetPolicyMode,
  entryMode,
  filterPolicyShellEntries,
  filterPolicyTools,
  normalizeShellCommand,
  removeLocalShellEntry,
} from "../utils/policyEditor.js";
import { formatPolicyMode, policyModeClass } from "../utils/panelFormat.js";

const emit = defineEmits(["close"]);

const SHELL_ORDER = ["bash", "cmd", "powershell"];

const loading = ref(false);
const busyKey = ref("");
const error = ref("");
const statusMessage = ref("");
const showRaw = ref(false);
const data = ref(null);
const tab = ref("tools");
const shellTab = ref("bash");
const filterText = ref("");
const newShellCommand = ref("");
const newShellMode = ref("never");

const shellTypes = computed(() => {
  const shell = data.value?.shell;
  if (!shell || typeof shell !== "object") return [];
  const keys = Object.keys(shell);
  return SHELL_ORDER.filter((k) => keys.includes(k)).concat(keys.filter((k) => !SHELL_ORDER.includes(k)));
});

const filteredTools = computed(() => filterPolicyTools(data.value?.tools, filterText.value));

const shellEntries = computed(() => {
  const shell = data.value?.shell;
  if (!shell || typeof shell !== "object") return [];
  const rows = shell[shellTab.value];
  return filterPolicyShellEntries(rows, filterText.value);
});

function rowBusy(kind, key) {
  return busyKey.value === `${kind}:${key}`;
}

async function load() {
  loading.value = true;
  error.value = "";
  statusMessage.value = "";
  try {
    data.value = await api.getPolicy();
    const def = data.value?.platform?.default_shell || "bash";
    shellTab.value = shellTypes.value.includes(def) ? def : shellTypes.value[0] || "bash";
  } catch (e) {
    error.value = e.message;
    data.value = null;
  } finally {
    loading.value = false;
  }
}

async function updateToolMode(name, mode) {
  if (!name || rowBusy("tool", name)) return;
  if (!canSetPolicyMode(name, mode)) {
    error.value = `${PROTECTED_POLICY_TOOL} 不能设为禁止`;
    return;
  }
  const row = filteredTools.value.find((r) => r.name === name);
  if (String(mode) === String(entryMode(row))) return;
  busyKey.value = `tool:${name}`;
  error.value = "";
  statusMessage.value = "";
  try {
    await api.updateToolPolicy([{ name, mode }]);
    applyLocalToolUpdate(data.value, name, mode);
    statusMessage.value = `已更新 ${name} → ${formatPolicyMode(mode)}`;
  } catch (e) {
    error.value = e.message;
  } finally {
    busyKey.value = "";
  }
}

async function updateShellMode(command, mode) {
  if (!command || rowBusy("shell", command)) return;
  const row = shellEntries.value.find((r) => r.command === command);
  if (String(mode) === String(entryMode(row))) return;
  busyKey.value = `shell:${command}`;
  error.value = "";
  statusMessage.value = "";
  try {
    await api.updateShellPolicy(shellTab.value, [{ command, mode }]);
    applyLocalShellUpdate(data.value, shellTab.value, command, mode);
    statusMessage.value = `已更新 ${command} → ${formatPolicyMode(mode)}`;
  } catch (e) {
    error.value = e.message;
  } finally {
    busyKey.value = "";
  }
}

async function deleteShellCommand(command) {
  const cmd = normalizeShellCommand(command);
  if (!cmd || rowBusy("shell", cmd)) return;
  busyKey.value = `shell:${cmd}`;
  error.value = "";
  statusMessage.value = "";
  try {
    await api.updateShellPolicy(shellTab.value, [], [cmd]);
    removeLocalShellEntry(data.value, shellTab.value, cmd);
    statusMessage.value = `已删除 ${cmd}（未列出命令默认需审批）`;
  } catch (e) {
    error.value = e.message;
  } finally {
    busyKey.value = "";
  }
}

async function addShellCommand() {
  const cmd = normalizeShellCommand(newShellCommand.value);
  if (!cmd || busyKey.value) return;
  busyKey.value = `shell:${cmd}`;
  error.value = "";
  statusMessage.value = "";
  try {
    await api.updateShellPolicy(shellTab.value, [{ command: cmd, mode: newShellMode.value }]);
    applyLocalShellUpdate(data.value, shellTab.value, cmd, newShellMode.value);
    statusMessage.value = `已添加 ${cmd} → ${formatPolicyMode(newShellMode.value)}`;
    newShellCommand.value = "";
  } catch (e) {
    error.value = e.message;
  } finally {
    busyKey.value = "";
  }
}

function isProtectedTool(name) {
  return String(name || "") === PROTECTED_POLICY_TOOL;
}

onMounted(load);
</script>

<template>
  <section class="panel panel-overlay__card command-panel policy-panel">
    <header class="panel__header command-panel__header">
      <div>
        <div class="panel__title">Policy</div>
        <div class="command-panel__subtitle">
          {{ data?.platform?.goos || "—" }} · 默认 shell {{ data?.platform?.default_shell || "—" }}
        </div>
      </div>
      <div class="command-panel__header-actions">
        <button type="button" class="btn btn--ghost btn--sm" @click="showRaw = !showRaw">
          {{ showRaw ? "友好视图" : "JSON" }}
        </button>
        <button type="button" class="btn btn--ghost btn--sm" :disabled="loading || !!busyKey" @click="load">
          刷新
        </button>
        <button type="button" class="btn btn--ghost btn--sm" data-panel-close @click="emit('close')">关闭</button>
      </div>
    </header>

    <div class="panel__body command-panel__body">
      <div v-if="loading && !data" class="command-panel__loading">加载中…</div>
      <div v-else-if="error" class="command-panel__error">{{ error }}</div>
      <pre v-else-if="showRaw && data" class="command-panel__raw">{{ JSON.stringify(data, null, 2) }}</pre>
      <template v-else-if="data">
        <div class="policy-panel__toolbar">
          <div class="command-tabs policy-panel__tabs">
            <button
              type="button"
              class="command-tab"
              :class="{ 'command-tab--active': tab === 'tools' }"
              @click="tab = 'tools'"
            >
              工具
            </button>
            <button
              type="button"
              class="command-tab"
              :class="{ 'command-tab--active': tab === 'shell' }"
              @click="tab = 'shell'"
            >
              Shell
            </button>
          </div>
          <input
            v-model="filterText"
            type="search"
            class="policy-panel__filter"
            :placeholder="tab === 'tools' ? '过滤工具名…' : '过滤命令…'"
          />
        </div>

        <p v-if="tab === 'shell'" class="policy-panel__hint">
          仅列出已显式配置的命令；未出现在列表中的命令默认<strong>需审批</strong>。
        </p>

        <p v-if="statusMessage" class="policy-panel__status">{{ statusMessage }}</p>

        <section v-if="tab === 'tools'" class="command-section">
          <h3 class="command-section__title">工具策略 ({{ filteredTools.length }})</h3>
          <div v-if="filteredTools.length" class="command-table-wrap">
            <table class="command-table">
              <thead>
                <tr>
                  <th>工具</th>
                  <th>当前</th>
                  <th>修改</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="row in filteredTools" :key="row.name">
                  <td class="command-table__mono">
                    {{ row.name }}
                    <span v-if="isProtectedTool(row.name)" class="policy-panel__protected">受保护</span>
                  </td>
                  <td>
                    <span class="decision-pill" :class="policyModeClass(entryMode(row))">
                      {{ formatPolicyMode(entryMode(row)) }}
                    </span>
                  </td>
                  <td>
                    <div class="policy-decision-group">
                      <button
                        v-for="opt in POLICY_MODES"
                        :key="opt.value"
                        type="button"
                        class="policy-decision-btn"
                        :class="[
                          `policy-decision-btn--${opt.value}`,
                          { 'policy-decision-btn--active': entryMode(row) === opt.value },
                        ]"
                        :disabled="
                          rowBusy('tool', row.name) ||
                          (isProtectedTool(row.name) && opt.value === 'deny')
                        "
                        :title="isProtectedTool(row.name) && opt.value === 'deny' ? '受保护工具不可设为禁止' : ''"
                        @click="updateToolMode(row.name, opt.value)"
                      >
                        {{ opt.label }}
                      </button>
                    </div>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
          <p v-else class="command-panel__empty">无匹配工具</p>
        </section>

        <section v-else class="command-section">
          <div class="command-section__head">
            <h3 class="command-section__title">Shell 命令策略 ({{ shellEntries.length }})</h3>
            <div v-if="shellTypes.length" class="command-tabs">
              <button
                v-for="st in shellTypes"
                :key="st"
                type="button"
                class="command-tab"
                :class="{ 'command-tab--active': shellTab === st }"
                @click="shellTab = st"
              >
                {{ st }}
              </button>
            </div>
          </div>
          <form class="policy-panel__add-shell" @submit.prevent="addShellCommand">
            <input
              v-model="newShellCommand"
              type="text"
              class="policy-panel__filter"
              placeholder="添加命令（取首个单词，如 docker / git）"
            />
            <select v-model="newShellMode" class="policy-panel__decision-select">
              <option v-for="opt in POLICY_MODES" :key="opt.value" :value="opt.value">
                {{ opt.label }}
              </option>
            </select>
            <button type="submit" class="btn btn--primary btn--sm" :disabled="!newShellCommand.trim() || !!busyKey">
              添加
            </button>
          </form>
          <div v-if="shellEntries.length" class="command-table-wrap">
            <table class="command-table">
              <thead>
                <tr>
                  <th>命令</th>
                  <th>当前</th>
                  <th>修改</th>
                  <th />
                </tr>
              </thead>
              <tbody>
                <tr v-for="row in shellEntries" :key="row.command">
                  <td class="command-table__mono">{{ row.command }}</td>
                  <td>
                    <span class="decision-pill" :class="policyModeClass(entryMode(row))">
                      {{ formatPolicyMode(entryMode(row)) }}
                    </span>
                  </td>
                  <td>
                    <div class="policy-decision-group">
                      <button
                        v-for="opt in POLICY_MODES"
                        :key="opt.value"
                        type="button"
                        class="policy-decision-btn"
                        :class="[
                          `policy-decision-btn--${opt.value}`,
                          { 'policy-decision-btn--active': entryMode(row) === opt.value },
                        ]"
                        :disabled="rowBusy('shell', row.command)"
                        @click="updateShellMode(row.command, opt.value)"
                      >
                        {{ opt.label }}
                      </button>
                    </div>
                  </td>
                  <td>
                    <button
                      type="button"
                      class="btn btn--ghost btn--sm btn--danger"
                      :disabled="rowBusy('shell', row.command)"
                      @click="deleteShellCommand(row.command)"
                    >
                      删除
                    </button>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
          <p v-else class="command-panel__empty">暂无显式 shell 规则，可在上方添加</p>
        </section>

        <p class="command-panel__foot">
          点击档位即时保存 · {{ data.policy_dir || "policy_dir" }}
        </p>
      </template>
    </div>
  </section>
</template>
