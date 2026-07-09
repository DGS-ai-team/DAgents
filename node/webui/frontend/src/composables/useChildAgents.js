import { computed, ref, unref, watch } from "vue";
import * as api from "../api/node.js";
import {
  childAgentItems,
  formatChildAgentStatus,
  isChildAgentActive,
  sortChildAgentItems,
} from "../utils/childAgent.js";

export { formatChildAgentStatus, isChildAgentActive };

export function useChildAgents(sessionIdSource) {
  const loading = ref(false);
  const error = ref("");
  const data = ref(null);
  const cancellingId = ref("");
  const statusMessage = ref("");

  const sessionId = computed(() => String(unref(sessionIdSource) || "").trim());

  const items = computed(() => sortChildAgentItems(childAgentItems(data.value)));

  async function load() {
    const sid = sessionId.value;
    if (!sid) {
      data.value = null;
      error.value = "";
      return;
    }
    loading.value = true;
    error.value = "";
    statusMessage.value = "";
    try {
      data.value = await api.listChildAgents(sid);
    } catch (e) {
      error.value = e.message;
      data.value = null;
    } finally {
      loading.value = false;
    }
  }

  async function cancelChild(childSessionId) {
    const sid = sessionId.value;
    const childId = String(childSessionId || "").trim();
    if (!sid || !childId || cancellingId.value === childId) return false;
    if (!window.confirm("确定取消该子 Agent？")) return false;
    cancellingId.value = childId;
    statusMessage.value = "";
    error.value = "";
    try {
      await api.cancelChildAgent(sid, childId, "用户取消");
      statusMessage.value = "已发送取消请求";
      await load();
      return true;
    } catch (e) {
      error.value = e.message;
      return false;
    } finally {
      cancellingId.value = "";
    }
  }

  watch(sessionId, load, { immediate: true });

  return {
    loading,
    error,
    data,
    items,
    cancellingId,
    statusMessage,
    load,
    cancelChild,
  };
}
