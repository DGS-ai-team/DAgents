import { reactive, ref } from "vue";
import * as api from "../api/node.js";

const emptyForm = () => ({
  node: { listen_host: "", listen_port: 0, local_endpoint: "" },
  llm: { active: "default", profiles: [], provider: "", base_url: "", model: "", api_key_env: "", mock: false, max_tool_loops: 16 },
  manage: {},
  features: {},
  compression: {},
  runtime: {},
  agent: {},
  child_agents: {},
  browser: {},
  tools: { enabled_groups: [] },
  hooks: {},
});

/** 共享 setup/config 加载与保存（各设置面板按需 PATCH 子块）。 */
export function useSetupConfig() {
  const loading = ref(false);
  const saving = ref(false);
  const error = ref("");
  const statusMessage = ref("");
  const configPath = ref("");
  const configWritable = ref(false);
  const form = reactive(emptyForm());

  function fillForm(data) {
    if (!data) return;
    configPath.value = data.config_path || "";
    configWritable.value = Boolean(data.config_writable);
    Object.assign(form.node, data.node || {});
    Object.assign(form.llm, data.llm || {});
    form.llm.profiles = Array.isArray(data.llm?.profiles)
      ? data.llm.profiles.map((p) => ({ ...p }))
      : [];
    if (!form.llm.active && form.llm.profiles.length) {
      form.llm.active = form.llm.profiles[0].id;
    }
    Object.assign(form.manage, data.manage || {});
    Object.assign(form.features, data.features || {});
    Object.assign(form.compression, data.compression || {});
    Object.assign(form.runtime, data.runtime || {});
    Object.assign(form.agent, data.agent || {});
    Object.assign(form.child_agents, data.child_agents || {});
    Object.assign(form.browser, data.browser || {});
    form.tools.enabled_groups = Array.isArray(data.tools?.enabled_groups)
      ? [...data.tools.enabled_groups]
      : [];
    Object.assign(form.tools, data.tools || {});
    Object.assign(form.hooks, data.hooks || {});
  }

  async function load() {
    loading.value = true;
    error.value = "";
    statusMessage.value = "";
    try {
      fillForm(await api.getSetupConfig());
    } catch (e) {
      error.value = e.message;
    } finally {
      loading.value = false;
    }
  }

  async function save(patch, { successHint } = {}) {
    if (!configWritable.value) {
      error.value = "当前环境无法写入 config.yaml";
      return false;
    }
    saving.value = true;
    error.value = "";
    statusMessage.value = "";
    try {
      const data = await api.patchSetupConfig(patch);
      fillForm(data);
      statusMessage.value =
        successHint ||
        (data.restart_required
          ? "已保存到 config.yaml。部分项需重启 Node（或 Shell 重启 Node）后生效。"
          : "已保存。");
      return true;
    } catch (e) {
      error.value = e.message;
      return false;
    } finally {
      saving.value = false;
    }
  }

  return {
    loading,
    saving,
    error,
    statusMessage,
    configPath,
    configWritable,
    form,
    load,
    save,
    fillForm,
  };
}
