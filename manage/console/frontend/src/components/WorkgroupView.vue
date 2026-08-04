<script setup>
import { computed, onMounted, reactive, ref, watch } from "vue";
import {
  fetchWorkgroupLLMConfigs,
  fetchWorkgroups,
  fetchWorkgroupTimeline,
  postWorkgroupMessage,
} from "../api.js";

const props = defineProps({
  active: { type: Boolean, default: false },
});
const emit = defineEmits(["toast"]);

const loading = ref(false);
const loadingTimeline = ref(false);
const sending = ref(false);
const error = ref("");

const workgroups = ref([]);
const selectedId = ref("");
const timeline = ref([]);
const llmConfigs = ref([]);

const form = reactive({
  fromNodeId: "console",
  text: "",
  disableTools: false,
});

const selectedWorkgroup = computed(
  () => workgroups.value.find((item) => item.workgroup_id === selectedId.value) || null,
);

async function loadWorkgroups() {
  loading.value = true;
  error.value = "";
  try {
    workgroups.value = await fetchWorkgroups();
    if (!selectedId.value && workgroups.value.length) {
      selectedId.value = workgroups.value[0].workgroup_id;
    }
  } catch (err) {
    error.value = err.message || "加载工作组失败";
    emit("toast", { message: error.value, type: "error" });
  } finally {
    loading.value = false;
  }
}

async function loadTimelineAndLLM() {
  if (!selectedId.value) {
    timeline.value = [];
    llmConfigs.value = [];
    return;
  }
  loadingTimeline.value = true;
  try {
    const [tl, llm] = await Promise.all([
      fetchWorkgroupTimeline(selectedId.value),
      fetchWorkgroupLLMConfigs(selectedId.value),
    ]);
    timeline.value = tl || [];
    llmConfigs.value = llm || [];
  } catch (err) {
    emit("toast", { message: err.message || "加载工作组详情失败", type: "error" });
  } finally {
    loadingTimeline.value = false;
  }
}

async function sendMessage() {
  const text = form.text.trim();
  const fromNodeId = form.fromNodeId.trim();
  if (!selectedId.value || !text || !fromNodeId) return;
  sending.value = true;
  try {
    await postWorkgroupMessage(selectedId.value, {
      text,
      from_node_id: fromNodeId,
      disable_tools: form.disableTools,
    });
    form.text = "";
    await loadTimelineAndLLM();
    emit("toast", { message: "消息已发送", type: "success" });
  } catch (err) {
    emit("toast", { message: err.message || "发送失败", type: "error" });
  } finally {
    sending.value = false;
  }
}

watch(
  () => selectedId.value,
  () => {
    if (props.active) loadTimelineAndLLM();
  },
);

watch(
  () => props.active,
  (active) => {
    if (active) {
      loadWorkgroups().then(loadTimelineAndLLM);
    }
  },
);

onMounted(async () => {
  if (props.active) {
    await loadWorkgroups();
    await loadTimelineAndLLM();
  }
});
</script>

<template>
  <section class="workgroup-page">
    <div class="panel split-layout">
      <aside class="split-sidebar">
        <div class="panel-head">
          <h2 class="panel-title">工作组</h2>
          <button type="button" class="btn btn-ghost" :disabled="loading" @click="loadWorkgroups">
            刷新
          </button>
        </div>
        <p v-if="error" class="state state-error">{{ error }}</p>
        <p v-else-if="loading" class="state">加载中…</p>
        <ul v-else class="workgroup-list">
          <li v-for="item in workgroups" :key="item.workgroup_id">
            <button
              type="button"
              class="workgroup-list-item"
              :class="{ active: selectedId === item.workgroup_id }"
              @click="selectedId = item.workgroup_id"
            >
              <strong>{{ item.display_name }}</strong>
              <span>{{ item.workgroup_id }}</span>
              <small>{{ item.status }}</small>
            </button>
          </li>
        </ul>
      </aside>

      <div class="split-content">
        <div v-if="!selectedWorkgroup" class="state">请选择一个工作组</div>
        <template v-else>
          <div class="panel-head">
            <h2 class="panel-title">{{ selectedWorkgroup.display_name }}</h2>
            <span class="panel-meta">{{ selectedWorkgroup.workgroup_id }}</span>
          </div>
          <div class="workgroup-meta-row">
            <span class="pill">{{ selectedWorkgroup.status }}</span>
            <span class="muted">
              LLM: {{ selectedWorkgroup.llm_profile_id }}@{{ selectedWorkgroup.llm_profile_revision }}
            </span>
          </div>
          <div class="panel subsection">
            <div class="panel-head">
              <h3 class="panel-title">可用 LLM 配置</h3>
            </div>
            <div class="tags">
              <span v-if="!llmConfigs.length" class="muted">无可见配置</span>
              <span v-for="cfg in llmConfigs" :key="cfg.id" class="tag">
                {{ cfg.name }} · {{ cfg.provider }} / {{ cfg.model }}
              </span>
            </div>
          </div>

          <div class="panel subsection">
            <div class="panel-head">
              <h3 class="panel-title">工作组对话</h3>
              <span class="panel-meta">{{ timeline.length }} 条</span>
            </div>
            <div v-if="loadingTimeline" class="state">加载对话中…</div>
            <div v-else class="timeline-list">
              <article v-for="event in timeline" :key="event.event_id" class="timeline-item">
                <header>
                  <strong>{{ event.actor_id }}</strong>
                  <span class="muted">{{ event.type }}</span>
                </header>
                <p>{{ event.text || "（空）" }}</p>
              </article>
            </div>
            <form class="workgroup-chat-form" @submit.prevent="sendMessage">
              <div class="form-grid">
                <label class="field">
                  <span>from_node_id</span>
                  <input v-model="form.fromNodeId" type="text" placeholder="console" />
                </label>
                <label class="field field-checkbox">
                  <input v-model="form.disableTools" type="checkbox" />
                  <span>禁用 supervisor 工具（纯对话）</span>
                </label>
              </div>
              <label class="field">
                <span>消息内容</span>
                <textarea
                  v-model="form.text"
                  rows="3"
                  placeholder="输入消息并发送到工作组 supervisor"
                />
              </label>
              <div class="form-actions">
                <button
                  type="submit"
                  class="btn btn-primary"
                  :disabled="sending || !form.text.trim() || !form.fromNodeId.trim()"
                >
                  {{ sending ? "发送中…" : "发送消息" }}
                </button>
              </div>
            </form>
          </div>
        </template>
      </div>
    </div>
  </section>
</template>
