<script setup>
import { computed, onMounted, ref, watch } from "vue";
import { fetchSkillCatalog, publishSkill, uploadSkillPackage } from "../api.js";
import { riskLabel, riskPillClass } from "../utils.js";
import PackageUploadModal from "./PackageUploadModal.vue";

const props = defineProps({
  active: { type: Boolean, default: false },
});
const emit = defineEmits(["toast"]);

const catalog = ref([]);
const drafts = ref([]);
const loading = ref(false);
const error = ref("");
const uploading = ref(false);
const modalOpen = ref(false);
const query = ref("");

const filteredCatalog = computed(() => {
  const q = query.value.trim().toLowerCase();
  const list = [...(catalog.value || [])];
  if (!q) return list;
  return list.filter((p) => {
    const hay = [p.skill_id, p.version, p.name, p.risk_level].filter(Boolean).join(" ").toLowerCase();
    return hay.includes(q);
  });
});

const catalogMeta = computed(() => {
  if (loading.value) return "加载中…";
  if (query.value.trim()) return `显示 ${filteredCatalog.value.length}/${catalog.value.length}`;
  return `${catalog.value.length} 条`;
});

async function load() {
  loading.value = true;
  error.value = "";
  try {
    catalog.value = await fetchSkillCatalog();
  } catch (err) {
    error.value = err.message;
    emit("toast", { message: err.message, type: "error" });
  } finally {
    loading.value = false;
  }
}

async function onUploadSubmit(payload) {
  uploading.value = true;
  try {
    const pkg = await uploadSkillPackage({
      skillId: payload.packageId,
      version: payload.version,
      name: payload.name,
      riskLevel: payload.riskLevel,
      file: payload.file,
    });
    drafts.value = [
      pkg,
      ...drafts.value.filter((d) => !(d.skill_id === pkg.skill_id && d.version === pkg.version)),
    ];
    emit("toast", { message: `已上传 ${pkg.skill_id}@${pkg.version}（草稿）`, type: "success" });
    modalOpen.value = false;
  } catch (err) {
    emit("toast", { message: err.message, type: "error" });
  } finally {
    uploading.value = false;
  }
}

async function onPublish(pkg) {
  try {
    await publishSkill(pkg.skill_id, pkg.version);
    emit("toast", { message: `已发布 ${pkg.skill_id}@${pkg.version}`, type: "success" });
    drafts.value = drafts.value.filter(
      (d) => !(d.skill_id === pkg.skill_id && d.version === pkg.version),
    );
    await load();
  } catch (err) {
    emit("toast", { message: err.message, type: "error" });
  }
}

function downloadUrl(pkg) {
  return `/v1/skills/catalog/${encodeURIComponent(pkg.skill_id)}/versions/${encodeURIComponent(pkg.version)}/download`;
}

watch(
  () => props.active,
  (now) => {
    if (now) load();
  },
);
onMounted(() => {
  if (props.active) load();
});
defineExpose({ load });
</script>

<template>
  <section class="mkt-page">
    <div class="mkt-toolbar">
      <input
        v-model="query"
        type="search"
        class="mkt-search"
        placeholder="搜索已发布 Skill…"
        autocomplete="off"
      />
      <button type="button" class="btn btn-primary btn-sm" @click="modalOpen = true">
        新建
      </button>
    </div>

    <div v-if="drafts.length" class="panel mkt-panel">
      <div class="panel-head">
        <h3 class="table-block__title">待发布草稿</h3>
        <span class="panel-meta">{{ drafts.length }} 条 · 刷新页面后本地草稿会清空</span>
      </div>
      <div class="table-scroll">
        <table class="data-table">
          <thead>
            <tr>
              <th>包 ID</th>
              <th>版本</th>
              <th>名称</th>
              <th>风险</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="d in drafts" :key="d.skill_id + '@' + d.version">
              <td><strong>{{ d.skill_id }}</strong></td>
              <td>{{ d.version }}</td>
              <td>{{ d.name }}</td>
              <td>
                <span class="pill" :class="riskPillClass(d.risk_level)">{{ riskLabel(d.risk_level) }}</span>
              </td>
              <td>
                <button type="button" class="btn btn-primary btn-sm" @click="onPublish(d)">发布</button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <div class="panel mkt-panel mkt-panel--catalog">
      <div class="panel-head">
        <h3 class="table-block__title">已发布目录</h3>
        <span class="panel-meta">{{ catalogMeta }}</span>
      </div>
      <p v-if="error" class="banner banner-error" role="alert">{{ error }}</p>
      <div class="table-scroll">
        <table class="data-table">
          <thead>
            <tr>
              <th>包 ID</th>
              <th>版本</th>
              <th>名称</th>
              <th>风险</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="p in filteredCatalog" :key="p.skill_id + '@' + p.version">
              <td><strong>{{ p.skill_id }}</strong></td>
              <td>{{ p.version }}</td>
              <td>{{ p.name }}</td>
              <td>
                <span class="pill" :class="riskPillClass(p.risk_level)">{{ riskLabel(p.risk_level) }}</span>
              </td>
              <td>
                <a class="btn btn-ghost btn-sm" :href="downloadUrl(p)">下载</a>
              </td>
            </tr>
            <tr v-if="!loading && !filteredCatalog.length">
              <td colspan="5" class="empty">
                <div class="empty-state">
                  {{ catalog.length ? "无匹配项" : "暂无已发布 Skill，点击右上角「新建」上传" }}
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <PackageUploadModal
      :open="modalOpen"
      kind="skill"
      :uploading="uploading"
      @close="modalOpen = false"
      @submit="onUploadSubmit"
    />
  </section>
</template>
