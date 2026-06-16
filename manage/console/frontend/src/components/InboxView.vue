<script setup>
import { computed, ref, watch } from "vue";
import { formatUnix, taskStatusPillClass, truncate } from "../utils.js";

const props = defineProps({
  tasks: { type: Array, default: () => [] },
  loading: { type: Boolean, default: false },
  error: { type: String, default: "" },
  page: { type: Number, required: true },
  total: { type: Number, required: true },
  pageSize: { type: Number, required: true },
  filters: { type: Object, required: true },
  summary: { type: String, default: "—" },
});

const emit = defineEmits(["filter-change", "page-prev", "page-next"]);

const localFilters = ref({ ...props.filters });
let debounceTimer;

watch(
  () => props.filters,
  (v) => {
    localFilters.value = { ...v };
  },
  { deep: true },
);

const totalPages = computed(() => Math.max(1, Math.ceil(props.total / props.pageSize)));
const pagerLabel = computed(
  () => `第 ${props.page} / ${totalPages.value} 页（共 ${props.total} 条）`,
);

function emitFilters(immediate = false) {
  clearTimeout(debounceTimer);
  const payload = { ...localFilters.value };
  if (immediate) {
    emit("filter-change", payload);
    return;
  }
  debounceTimer = setTimeout(() => emit("filter-change", payload), 350);
}
</script>

<template>
  <div>
    <section class="panel filters-panel">
      <div class="panel-head">
        <h2 class="panel-title">Task 筛选</h2>
        <span class="panel-meta">{{ summary }}</span>
      </div>
      <div class="filters-grid filters-grid-inbox">
        <label class="field">
          <span>to_agent_id</span>
          <input
            v-model="localFilters.to"
            type="text"
            placeholder="收件 Node"
            autocomplete="off"
            @input="emitFilters()"
          />
        </label>
        <label class="field">
          <span>from_agent_id</span>
          <input
            v-model="localFilters.from"
            type="text"
            placeholder="发件 Node"
            autocomplete="off"
            @input="emitFilters()"
          />
        </label>
        <label class="field">
          <span>status</span>
          <select v-model="localFilters.status" @change="emitFilters(true)">
            <option value="">全部</option>
            <option value="queued">queued</option>
            <option value="delivered">delivered</option>
            <option value="processing">processing</option>
            <option value="awaiting_caller">awaiting_caller</option>
            <option value="caller_notified">caller_notified</option>
            <option value="caller_responded">caller_responded</option>
            <option value="completed">completed</option>
            <option value="failed">failed</option>
            <option value="expired">expired</option>
          </select>
        </label>
        <label class="field field-narrow">
          <span>每页</span>
          <select
            v-model.number="localFilters.pageSize"
            @change="emitFilters(true)"
          >
            <option :value="25">25</option>
            <option :value="50">50</option>
            <option :value="100">100</option>
          </select>
        </label>
      </div>
      <p class="filters-note muted">只读观测列表，不会触发 deliver。</p>
    </section>

    <section class="panel table-panel">
      <div v-if="error" class="banner banner-error" role="alert">{{ error }}</div>
      <div class="table-scroll">
        <table class="data-table">
          <thead>
            <tr>
              <th>task_id</th>
              <th>from → to</th>
              <th>kind</th>
              <th>status</th>
              <th>content</th>
              <th>created</th>
              <th>expires</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="loading">
              <td colspan="7" class="empty">
                <div class="empty-state">
                  <span class="spinner" aria-hidden="true"></span>
                  加载中…
                </div>
              </td>
            </tr>
            <tr v-else-if="!tasks.length">
              <td colspan="7" class="empty">
                <div class="empty-state">无匹配 Task</div>
              </td>
            </tr>
            <tr v-for="task in tasks" v-else :key="task.task_id">
              <td><code class="mono">{{ task.task_id }}</code></td>
              <td>
                <code class="mono">{{ task.from_agent_id }}</code>
                →
                <code class="mono">{{ task.to_agent_id }}</code>
              </td>
              <td>{{ task.kind }}</td>
              <td>
                <span class="pill" :class="taskStatusPillClass(task.status)">
                  {{ task.status || "—" }}
                </span>
              </td>
              <td class="cell-wrap">{{ truncate(task.content, 160) }}</td>
              <td class="mono">{{ formatUnix(task.created_at_unix) }}</td>
              <td class="mono">{{ formatUnix(task.expires_at_unix) }}</td>
            </tr>
          </tbody>
        </table>
      </div>
      <footer class="pager">
        <button
          type="button"
          class="btn btn-ghost"
          :disabled="page <= 1"
          @click="emit('page-prev')"
        >
          上一页
        </button>
        <span class="pager-label">{{ pagerLabel }}</span>
        <button
          type="button"
          class="btn btn-ghost"
          :disabled="page >= totalPages"
          @click="emit('page-next')"
        >
          下一页
        </button>
      </footer>
    </section>
  </div>
</template>
