<script setup>
import { onMounted, reactive, ref, watch } from "vue";
import { fetchSkillCatalog, publishSkill, uploadSkillPackage } from "../api.js";
import { riskPillClass } from "../utils.js";

const props = defineProps({
  active: { type: Boolean, default: false },
});
const emit = defineEmits(["toast"]);

const catalog = ref([]);
const drafts = ref([]);
const loading = ref(false);
const error = ref("");
const uploading = ref(false);
const fileInput = ref(null);

const form = reactive({
  skill_id: "",
  version: "",
  name: "",
  risk_level: "low",
  file: null,
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

function onFile(e) {
  form.file = e.target.files?.[0] || null;
}

async function onUpload() {
  if (!form.skill_id.trim() || !form.version.trim() || !form.name.trim() || !form.file) {
    emit("toast", { message: "skill_id / version / name / 文件 必填", type: "error" });
    return;
  }
  uploading.value = true;
  try {
    const pkg = await uploadSkillPackage({
      skillId: form.skill_id.trim(),
      version: form.version.trim(),
      name: form.name.trim(),
      riskLevel: form.risk_level,
      file: form.file,
    });
    drafts.value = [pkg, ...drafts.value.filter(
      (d) => !(d.skill_id === pkg.skill_id && d.version === pkg.version),
    )];
    emit("toast", { message: `已上传 ${pkg.skill_id}@${pkg.version}（草稿）`, type: "success" });
    form.skill_id = "";
    form.version = "";
    form.name = "";
    form.file = null;
    if (fileInput.value) fileInput.value.value = "";
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
  <section class="panel">
    <div class="panel-head">
      <h2 class="panel-title">上传 Skill 包</h2>
      <span class="panel-meta">上传为 draft，发布后进入目录</span>
    </div>
    <div class="filters-grid">
      <label class="field">
        <span>skill_id</span>
        <input v-model="form.skill_id" placeholder="service-restart" />
      </label>
      <label class="field field-narrow">
        <span>version</span>
        <input v-model="form.version" placeholder="1.0.0" />
      </label>
      <label class="field field-grow">
        <span>名称</span>
        <input v-model="form.name" placeholder="Service Restart" />
      </label>
      <label class="field field-narrow">
        <span>风险等级</span>
        <select v-model="form.risk_level">
          <option value="low">low</option>
          <option value="medium">medium</option>
          <option value="high">high</option>
        </select>
      </label>
      <label class="field field-grow">
        <span>zip 文件</span>
        <input ref="fileInput" type="file" accept=".zip" @change="onFile" />
      </label>
      <div class="field field-narrow">
        <span>&nbsp;</span>
        <button class="btn btn-primary" :disabled="uploading" @click="onUpload">
          {{ uploading ? "上传中…" : "上传" }}
        </button>
      </div>
    </div>
  </section>

  <section v-if="drafts.length" class="table-panel">
    <div class="panel-head">
      <h2 class="panel-title">草稿（待发布）</h2>
      <span class="panel-meta">{{ drafts.length }} 条</span>
    </div>
    <div class="table-scroll">
      <table class="data-table">
        <thead>
          <tr><th>skill_id</th><th>version</th><th>名称</th><th>风险</th><th>操作</th></tr>
        </thead>
        <tbody>
          <tr v-for="d in drafts" :key="d.skill_id + '@' + d.version">
            <td><strong>{{ d.skill_id }}</strong></td>
            <td>{{ d.version }}</td>
            <td>{{ d.name }}</td>
            <td><span class="pill" :class="riskPillClass(d.risk_level)">{{ d.risk_level }}</span></td>
            <td><button class="btn btn-primary" @click="onPublish(d)">发布</button></td>
          </tr>
        </tbody>
      </table>
    </div>
  </section>

  <section class="table-panel">
    <div class="panel-head">
      <h2 class="panel-title">已发布目录</h2>
      <span class="panel-meta">{{ loading ? "加载中…" : `${catalog.length} 条` }}</span>
    </div>
    <p v-if="error" class="banner-error">{{ error }}</p>
    <div class="table-scroll">
      <table class="data-table">
        <thead>
          <tr><th>skill_id</th><th>version</th><th>名称</th><th>风险</th><th>owner/team</th><th>下载</th></tr>
        </thead>
        <tbody>
          <tr v-for="p in catalog" :key="p.skill_id + '@' + p.version">
            <td><strong>{{ p.skill_id }}</strong></td>
            <td>{{ p.version }}</td>
            <td>{{ p.name }}</td>
            <td><span class="pill" :class="riskPillClass(p.risk_level)">{{ p.risk_level }}</span></td>
            <td class="muted">{{ p.owner || "—" }} / {{ p.team || "—" }}</td>
            <td><a class="btn btn-ghost" :href="downloadUrl(p)">下载 zip</a></td>
          </tr>
          <tr v-if="!loading && catalog.length === 0">
            <td colspan="6" class="empty">暂无已发布 Skill</td>
          </tr>
        </tbody>
      </table>
    </div>
  </section>
</template>
