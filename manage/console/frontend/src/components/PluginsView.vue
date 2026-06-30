<script setup>
import { onMounted, reactive, ref, watch } from "vue";
import { fetchPluginCatalog, publishPlugin, uploadPluginPackage } from "../api.js";
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
  plugin_id: "",
  version: "",
  name: "",
  platform: "any",
  risk_level: "low",
  file: null,
});

async function load() {
  loading.value = true;
  error.value = "";
  try {
    catalog.value = await fetchPluginCatalog();
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
  if (!form.plugin_id.trim() || !form.version.trim() || !form.name.trim() || !form.file) {
    emit("toast", { message: "plugin_id / version / name / 文件 必填", type: "error" });
    return;
  }
  uploading.value = true;
  try {
    const pkg = await uploadPluginPackage({
      pluginId: form.plugin_id.trim(),
      version: form.version.trim(),
      name: form.name.trim(),
      platform: form.platform,
      riskLevel: form.risk_level,
      file: form.file,
    });
    drafts.value = [pkg, ...drafts.value.filter(
      (d) => !(d.plugin_id === pkg.plugin_id && d.version === pkg.version),
    )];
    emit("toast", { message: `已上传 ${pkg.plugin_id}@${pkg.version}（草稿）`, type: "success" });
    form.plugin_id = "";
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
    await publishPlugin(pkg.plugin_id, pkg.version);
    emit("toast", { message: `已发布 ${pkg.plugin_id}@${pkg.version}`, type: "success" });
    drafts.value = drafts.value.filter(
      (d) => !(d.plugin_id === pkg.plugin_id && d.version === pkg.version),
    );
    await load();
  } catch (err) {
    emit("toast", { message: err.message, type: "error" });
  }
}

function downloadUrl(pkg) {
  return `/v1/plugins/catalog/${encodeURIComponent(pkg.plugin_id)}/versions/${encodeURIComponent(pkg.version)}/download`;
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
  <section class="panel-card">
    <div class="form-block">
      <h3 class="form-block__title">上传 Hook Plugin（.so）</h3>
      <p class="muted filters-note">
        Go in-process plugin；上传为 draft，发布后进入目录，供案例库关联
      </p>
      <div class="form-grid">
        <label>
          <span>plugin_id</span>
          <input v-model="form.plugin_id" placeholder="protect-loaded-skill" />
        </label>
        <label>
          <span>version</span>
          <input v-model="form.version" placeholder="1.0.0" />
        </label>
        <label>
          <span>平台</span>
          <select v-model="form.platform">
            <option value="any">any</option>
            <option value="linux-amd64">linux-amd64</option>
            <option value="windows-amd64">windows-amd64</option>
            <option value="darwin-arm64">darwin-arm64</option>
          </select>
        </label>
        <label>
          <span>风险等级</span>
          <select v-model="form.risk_level">
            <option value="low">low</option>
            <option value="medium">medium</option>
            <option value="high">high</option>
          </select>
        </label>
        <label class="form-grid__wide">
          <span>名称</span>
          <input v-model="form.name" placeholder="Protect Loaded Skill" />
        </label>
        <label class="form-grid__wide">
          <span>.so 文件</span>
          <input ref="fileInput" type="file" accept=".so" @change="onFile" />
        </label>
      </div>
      <div class="panel-actions">
        <button class="btn btn-primary" :disabled="uploading" @click="onUpload">
          {{ uploading ? "上传中…" : "上传" }}
        </button>
      </div>
    </div>

    <div v-if="drafts.length" class="table-block">
      <div class="panel-head">
        <h3 class="table-block__title">草稿（待发布）</h3>
        <span class="panel-meta">{{ drafts.length }} 条</span>
      </div>
      <div class="table-scroll">
        <table class="data-table">
          <thead>
            <tr><th>plugin_id</th><th>version</th><th>名称</th><th>平台</th><th>风险</th><th>操作</th></tr>
          </thead>
          <tbody>
            <tr v-for="d in drafts" :key="d.plugin_id + '@' + d.version">
              <td><strong>{{ d.plugin_id }}</strong></td>
              <td>{{ d.version }}</td>
              <td>{{ d.name }}</td>
              <td>{{ d.platform || "any" }}</td>
              <td><span class="pill" :class="riskPillClass(d.risk_level)">{{ d.risk_level }}</span></td>
              <td><button class="btn btn-primary btn-sm" @click="onPublish(d)">发布</button></td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <div class="table-block">
      <div class="panel-head">
        <h3 class="table-block__title">已发布目录</h3>
        <span class="panel-meta">{{ loading ? "加载中…" : `${catalog.length} 条` }}</span>
      </div>
      <p v-if="error" class="banner banner-error" role="alert">{{ error }}</p>
      <div class="table-scroll">
        <table class="data-table">
          <thead>
            <tr><th>plugin_id</th><th>version</th><th>名称</th><th>平台</th><th>风险</th><th>下载</th></tr>
          </thead>
          <tbody>
            <tr v-for="p in catalog" :key="p.plugin_id + '@' + p.version">
              <td><strong>{{ p.plugin_id }}</strong></td>
              <td>{{ p.version }}</td>
              <td>{{ p.name }}</td>
              <td>{{ p.platform || "any" }}</td>
              <td><span class="pill" :class="riskPillClass(p.risk_level)">{{ p.risk_level }}</span></td>
              <td><a class="btn btn-ghost btn-sm" :href="downloadUrl(p)">下载</a></td>
            </tr>
            <tr v-if="!loading && catalog.length === 0">
              <td colspan="6" class="empty">暂无已发布 Plugin</td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  </section>
</template>
