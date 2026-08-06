<script setup>
import { computed, ref, watch } from "vue";
import AgentTable from "./AgentTable.vue";

const props = defineProps({
  agents: { type: Array, default: () => [] },
  loading: { type: Boolean, default: false },
  error: { type: String, default: "" },
  page: { type: Number, required: true },
  total: { type: Number, required: true },
  pageSize: { type: Number, required: true },
  filters: {
    type: Object,
    required: true,
  },
  roleHint: { type: String, default: "" },
  listSummary: { type: String, default: "—" },
});

const emit = defineEmits(["open", "filter-change", "page-prev", "page-next"]);

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
  <div class="list-page">
    <section class="panel filters-panel">
      <div class="panel-head">
        <h2 class="panel-title">筛选与搜索</h2>
        <span class="panel-meta">{{ listSummary }}</span>
      </div>
      <div class="filters-grid">
        <label class="field">
          <span>discovery_group</span>
          <input
            v-model="localFilters.group"
            type="text"
            placeholder="留空 = 全部"
            autocomplete="off"
            @input="emitFilters()"
          />
        </label>
        <label class="field">
          <span>team</span>
          <input
            v-model="localFilters.team"
            type="text"
            placeholder="精确匹配"
            autocomplete="off"
            @input="emitFilters()"
          />
        </label>
        <label class="field">
          <span>status</span>
          <select v-model="localFilters.status" @change="emitFilters(true)">
            <option value="all">全部</option>
            <option value="online">online</option>
            <option value="offline">offline</option>
          </select>
        </label>
        <label class="field field-grow">
          <span>搜索</span>
          <input
            v-model="localFilters.q"
            type="search"
            placeholder="名称 / node_id / 描述"
            autocomplete="off"
            @input="emitFilters()"
          />
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
      <div class="filters-meta">
        <span class="muted">{{ roleHint }}</span>
      </div>
    </section>

    <AgentTable
      :agents="agents"
      :loading="loading"
      :error="error"
      @open="emit('open', $event)"
    >
      <template #pager>
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
      </template>
    </AgentTable>
  </div>
</template>
