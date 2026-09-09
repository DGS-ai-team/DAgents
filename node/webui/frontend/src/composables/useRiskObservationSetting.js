import { getCurrentInstance, onBeforeUnmount, ref, watch } from "vue";
import * as api from "../api/node.js";
import { buildRiskObservationPatch } from "../utils/agentTemplateForm.js";

export function useRiskObservationSetting({ agentId, agentMeta, draft }) {
  const saving = ref(false), error = ref(""), confirmed = ref(true);
  let token = 0;
  const current = (id, t) => id === agentId.value && t === token;
  const snapshotOf = (value) => {
    if (typeof value !== "string") return value || {};
    try { return JSON.parse(value) || {}; } catch { return {}; }
  };
  async function save(enabled) {
    const id = agentId.value, t = ++token;
    draft.riskObservationEnabled = Boolean(enabled);
    saving.value = true; error.value = ""; confirmed.value = false;
    try {
      const updated = await api.patchAgent(id, buildRiskObservationPatch(agentMeta.value, enabled));
      if (!current(id, t)) return;
      agentMeta.value = updated;
      await api.reloadAgentRuntime(id);
      if (current(id, t)) confirmed.value = true;
    } catch (cause) {
      if (!current(id, t)) return;
      error.value = cause?.message || "风险观察设置保存失败";
      const previous = Boolean(snapshotOf(agentMeta.value?.config_snapshot)?.defaults?.hooks?.risk_observation_enabled);
      try {
        const server = await api.getAgent(id);
        if (current(id, t)) {
          const snap = typeof server?.config_snapshot === "string" ? (() => { try { return JSON.parse(server.config_snapshot) || {}; } catch { return {}; } })() : (server?.config_snapshot || {});
          draft.riskObservationEnabled = Boolean(snap?.defaults?.hooks?.risk_observation_enabled);
          agentMeta.value = server; confirmed.value = true;
        }
      } catch {
        if (current(id, t)) draft.riskObservationEnabled = previous;
      }
    } finally { if (current(id, t)) saving.value = false; }
  }
  watch(agentId, () => { token += 1; saving.value = false; error.value = ""; confirmed.value = true; });
  if (getCurrentInstance()) onBeforeUnmount(() => { token += 1; saving.value = false; });
  return { saving, error, confirmed, save };
}
