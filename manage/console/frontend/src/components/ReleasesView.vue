<script setup>
import { onMounted, reactive, ref, watch } from "vue";
import {
  deleteReleasePackage,
  fetchReleasePackages,
  promoteReleasePackage,
  publishReleasePackage,
  uploadReleasePackage,
} from "../api.js";

const props = defineProps({
  active: { type: Boolean, default: false },
});
const emit = defineEmits(["toast"]);

const packages = ref([]);
const loading = ref(false);
const error = ref("");
const uploading = ref(false);
const fileInput = ref(null);

const form = reactive({
  version: "",
  platform: "linux-amd64",
  channel: "stable",
  releaseNotes: "",
  publish: false,
  setLatest: false,
  file: null,
});

const drafts = () => packages.value.filter((p) => p.status === "draft");
const published = () => packages.value.filter((p) => p.status === "published");

async function load() {
  loading.value = true;
  error.value = "";
  try {
    packages.value = await fetchReleasePackages();
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

function formatSize(n) {
  const v = Number(n) || 0;
  if (v >= 1024 * 1024) return `${(v / (1024 * 1024)).toFixed(1)} MB`;
  if (v >= 1024) return `${Math.round(v / 1024)} KB`;
  return `${v} B`;
}

function downloadUrl(pkg) {
  return `/v1/releases/packages/${encodeURIComponent(pkg.artifact)}/${encodeURIComponent(pkg.channel)}/${encodeURIComponent(pkg.platform)}/${encodeURIComponent(pkg.version)}/download`;
}

async function onUpload() {
  if (!form.version.trim() || !form.platform.trim() || !form.file) {
    emit("toast", { message: "version / platform / 文件 必填", type: "error" });
    return;
  }
  uploading.value = true;
  try {
    const pkg = await uploadReleasePackage({
      version: form.version.trim(),
      platform: form.platform.trim(),
      channel: form.channel.trim() || "stable",
      releaseNotes: form.releaseNotes.trim(),
      publish: form.publish,
      setLatest: form.setLatest,
      file: form.file,
    });
    emit("toast", {
      message: form.publish
        ? `已上传并发布 ${pkg.version} (${pkg.platform})`
        : `已上传 ${pkg.version} 草稿`,
      type: "success",
    });
    form.version = "";
    form.releaseNotes = "";
    form.file = null;
    if (fileInput.value) fileInput.value.value = "";
    await load();
  } catch (err) {
    emit("toast", { message: err.message, type: "error" });
  } finally {
    uploading.value = false;
  }
}

async function onPublish(pkg, setLatest = false) {
  try {
    await publishReleasePackage(pkg, { setLatest });
    emit("toast", { message: `已发布 ${pkg.version}`, type: "success" });
    await load();
  } catch (err) {
    emit("toast", { message: err.message, type: "error" });
  }
}

async function onPromote(pkg) {
  try {
    await promoteReleasePackage(pkg);
    emit("toast", { message: `已设为 latest: ${pkg.version}`, type: "success" });
    await load();
  } catch (err) {
    emit("toast", { message: err.message, type: "error" });
  }
}

async function onDelete(pkg) {
  if (!window.confirm(`删除 ${pkg.version} (${pkg.platform})？`)) return;
  try {
    await deleteReleasePackage(pkg);
    emit("toast", { message: "已删除", type: "success" });
    await load();
  } catch (err) {
    emit("toast", { message: err.message, type: "error" });
  }
}

watch(
  () => props.active,
  (on) => {
    if (on) load();
  },
  { immediate: true },
);

onMounted(() => {
  if (props.active) load();
});
</script>

<template>
  <section class="panel-card">
    <div class="form-block">
      <h3 class="form-block__title">上传安装包</h3>
      <p class="muted filters-note">默认草稿；发布后可设为 latest 供 Node 检查更新</p>
      <div class="form-grid upload-grid">
        <label>
          <span>version</span>
          <input v-model="form.version" placeholder="0.5.2" />
        </label>
        <label>
          <span>platform</span>
          <select v-model="form.platform">
            <option value="linux-amd64">linux-amd64</option>
            <option value="linux-arm64">linux-arm64</option>
            <option value="windows-amd64">windows-amd64</option>
          </select>
        </label>
        <label>
          <span>channel</span>
          <input v-model="form.channel" placeholder="stable" />
        </label>
        <label class="form-grid__wide">
          <span>release notes</span>
          <input v-model="form.releaseNotes" placeholder="可选" />
        </label>
        <label class="form-grid__wide">
          <span>安装包 (.tar.gz / .zip)</span>
          <input ref="fileInput" type="file" accept=".tar.gz,.zip,application/gzip,application/zip" @change="onFile" />
        </label>
      </div>
      <div class="form-options">
        <label class="checkbox-row">
          <input v-model="form.publish" type="checkbox" />
          <span>上传后立即发布</span>
        </label>
        <label class="checkbox-row">
          <input v-model="form.setLatest" type="checkbox" :disabled="!form.publish" />
          <span>同时设为 latest</span>
        </label>
      </div>
      <div class="panel-actions">
        <button class="btn btn-primary" :disabled="uploading" @click="onUpload">
          {{ uploading ? "上传中…" : "上传" }}
        </button>
      </div>
    </div>

    <p v-if="error" class="banner banner-error" role="alert">{{ error }}</p>
    <p v-if="loading" class="muted">加载中…</p>

    <div v-if="drafts().length" class="table-block">
      <h3 class="table-block__title">草稿</h3>
      <div class="table-scroll">
        <table class="data-table">
          <thead>
            <tr><th>version</th><th>platform</th><th>大小</th><th>来源</th><th>操作</th></tr>
          </thead>
          <tbody>
            <tr v-for="pkg in drafts()" :key="`${pkg.platform}-${pkg.version}-draft`">
              <td><strong>{{ pkg.version }}</strong></td>
              <td>{{ pkg.platform }}</td>
              <td>{{ formatSize(pkg.size_bytes) }}</td>
              <td>{{ pkg.source || "upload" }}</td>
              <td class="actions-cell">
                <button class="btn btn-sm btn-ghost" @click="onPublish(pkg, false)">发布</button>
                <button class="btn btn-sm btn-primary" @click="onPublish(pkg, true)">发布并 latest</button>
                <button class="btn btn-sm btn-danger" @click="onDelete(pkg)">删除</button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <div v-if="published().length" class="table-block">
      <h3 class="table-block__title">已发布</h3>
      <div class="table-scroll">
        <table class="data-table">
          <thead>
            <tr><th>version</th><th>platform</th><th>latest</th><th>大小</th><th>操作</th></tr>
          </thead>
          <tbody>
            <tr v-for="pkg in published()" :key="`${pkg.platform}-${pkg.version}-pub`">
              <td><strong>{{ pkg.version }}</strong></td>
              <td>{{ pkg.platform }}</td>
              <td>
                <span v-if="pkg.is_latest" class="pill pill-ok">latest</span>
                <span v-else class="muted">—</span>
              </td>
              <td>{{ formatSize(pkg.size_bytes) }}</td>
              <td class="actions-cell">
                <a class="btn btn-sm btn-ghost" :href="downloadUrl(pkg)" target="_blank" rel="noopener">下载</a>
                <button v-if="!pkg.is_latest" class="btn btn-sm btn-primary" @click="onPromote(pkg)">设为 latest</button>
                <button v-if="!pkg.is_latest" class="btn btn-sm btn-danger" @click="onDelete(pkg)">删除</button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  </section>
</template>
