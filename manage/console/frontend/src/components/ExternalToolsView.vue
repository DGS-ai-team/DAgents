<script setup>
import { onMounted, reactive, ref, watch } from "vue";
import {
  fetchExternalToolCatalog,
  publishExternalTool,
  uploadExternalToolPackage,
} from "../api.js";
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
  tool_id: "",
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
    catalog.value = await fetchExternalToolCatalog();
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
  if (!form.tool_id.trim() || !form.version.trim() || !form.name.trim() || !form.file) {
    emit("toast", { message: "tool_id / version / name / 文件 必填", type: "error" });
    return;
  }
  uploading.value = true;
  try {
    const pkg = await uploadExternalToolPackage({
      toolId: form.tool_id.trim(),
      version: form.version.trim(),
      name: form.name.trim(),
      platform: form.platform,
      riskLevel: form.risk_level,
      file: form.file,
    });
    drafts.value = [pkg, ...drafts.value.filter(
      (d) => !(d.tool_id === pkg.tool_id && d.version === pkg.version),
    )];
    emit("toast", { message: `已上传 ${pkg.tool_id}@${pkg.version}（草稿）`, type: "success" });
    form.tool_id = "";
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
    await publishExternalTool(pkg.tool_id, pkg.version);
    emit("toast", { message: `已发布 ${pkg.tool_id}@${pkg.version}`, type: "success" });
    drafts.value = drafts.value.filter(
      (d) => !(d.tool_id === pkg.tool_id && d.version === pkg.version),
    );
    await load();
  } catch (err) {
    emit("toast", { message: err.message, type: "error" });
  }
}

function downloadUrl(pkg) {
  return `/v1/externaltools/catalog/${encodeURIComponent(pkg.tool_id)}/versions/${encodeURIComponent(pkg.version)}/download`;
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
      <h3 class="form-block__title">上传外置 CLI / 二进制</h3>
      <p class="muted filters-note">
        上传为 draft，发布后进入目录；Node 侧安装至 <code>.runtime/externaltools/</code>
      </p>
      <div class="form-grid">
        <label>
          <span>tool_id</span>
          <input v-model="form.tool_id" placeholder="officecli" />
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
          <input v-model="form.name" placeholder="OfficeCLI" />
        </label>
        <label class="form-grid__wide">
          <span>可执行文件 / zip</span>
          <input ref="fileInput" type="file" @change="onFile" />
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
            <tr><th>tool_id</th><th>version</th><th>名称</th><th>平台</th><th>风险</th><th>操作</th></tr>
          </thead>
          <tbody>
            <tr v-for="d in drafts" :key="d.tool_id + '@' + d.version">
              <td><strong>{{ d.tool_id }}</strong></td>
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
            <tr><th>tool_id</th><th>version</th><th>名称</th><th>平台</th><th>风险</th><th>owner/team</th><th>下载</th></tr>
          </thead>
          <tbody>
            <tr v-for="p in catalog" :key="p.tool_id + '@' + p.version">
              <td><strong>{{ p.tool_id }}</strong></td>
              <td>{{ p.version }}</td>
              <td>{{ p.name }}</td>
              <td>{{ p.platform || "any" }}</td>
              <td><span class="pill" :class="riskPillClass(p.risk_level)">{{ p.risk_level }}</span></td>
              <td class="muted">{{ p.owner || "—" }} / {{ p.team || "—" }}</td>
              <td><a class="btn btn-ghost btn-sm" :href="downloadUrl(p)">下载</a></td>
            </tr>
            <tr v-if="!loading && catalog.length === 0">
              <td colspan="7" class="empty">暂无已发布外置工具</td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  </section>
</template>
