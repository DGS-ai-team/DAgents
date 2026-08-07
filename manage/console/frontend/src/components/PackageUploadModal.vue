<script setup>
import { computed, reactive, ref, watch } from "vue";

const KIND = {
  skill: {
    title: "上传 Skill 包",
    idKey: "package_id",
    idPlaceholder: "service-restart",
    namePlaceholder: "Service Restart",
    fileLabel: "zip 文件",
    accept: ".zip",
    showPlatform: false,
  },
  hook: {
    title: "上传 Hook 包",
    idKey: "package_id",
    idPlaceholder: "protect-loaded-skill",
    namePlaceholder: "Protect Loaded Skill",
    fileLabel: ".so 文件",
    accept: ".so",
    showPlatform: true,
  },
  tool: {
    title: "上传外置工具",
    idKey: "package_id",
    idPlaceholder: "officecli",
    namePlaceholder: "OfficeCLI",
    fileLabel: "可执行文件 / zip",
    accept: "",
    showPlatform: true,
  },
};

const props = defineProps({
  open: { type: Boolean, default: false },
  kind: { type: String, default: "skill" }, // skill | hook | tool
  uploading: { type: Boolean, default: false },
});

const emit = defineEmits(["close", "submit"]);

const meta = computed(() => KIND[props.kind] || KIND.skill);
const fileInput = ref(null);
const localError = ref("");

const draft = reactive(emptyDraft());

function emptyDraft() {
  return {
    package_id: "",
    version: "",
    name: "",
    platform: "any",
    risk_level: "low",
    file: null,
  };
}

function reset() {
  localError.value = "";
  Object.assign(draft, emptyDraft());
  if (fileInput.value) fileInput.value.value = "";
}

watch(
  () => props.open,
  (open) => {
    if (open) reset();
  },
);

function onFile(e) {
  draft.file = e.target.files?.[0] || null;
}

function onBackdropClick(e) {
  if (e.target === e.currentTarget && !props.uploading) emit("close");
}

function submit() {
  if (!draft.package_id.trim() || !draft.version.trim() || !draft.name.trim() || !draft.file) {
    localError.value = "包 ID、版本、名称与文件必填";
    return;
  }
  localError.value = "";
  emit("submit", {
    packageId: draft.package_id.trim(),
    version: draft.version.trim(),
    name: draft.name.trim(),
    platform: draft.platform,
    riskLevel: draft.risk_level,
    file: draft.file,
  });
}
</script>

<template>
  <Teleport to="body">
    <div v-if="open" class="modal-backdrop" @click="onBackdropClick">
      <section
        class="modal-panel llm-config-modal mkt-upload-modal"
        role="dialog"
        aria-modal="true"
        :aria-label="meta.title"
      >
        <header class="drawer-header">
          <div class="drawer-title-block">
            <h2>{{ meta.title }}</h2>
          </div>
          <button
            type="button"
            class="btn btn-ghost"
            aria-label="关闭"
            :disabled="uploading"
            @click="emit('close')"
          >
            ×
          </button>
        </header>

        <div class="drawer-body llm-config-modal__body">
          <p class="muted mkt-upload-modal__hint">上传后为草稿，发布后才会出现在目录中。</p>
          <div class="form-grid">
            <label>
              <span>包 ID</span>
              <input v-model="draft.package_id" :placeholder="meta.idPlaceholder" autocomplete="off" />
            </label>
            <label>
              <span>版本</span>
              <input v-model="draft.version" placeholder="1.0.0" autocomplete="off" />
            </label>
            <label v-if="meta.showPlatform">
              <span>平台</span>
              <select v-model="draft.platform">
                <option value="any">任意</option>
                <option value="linux-amd64">linux-amd64</option>
                <option value="windows-amd64">windows-amd64</option>
                <option value="darwin-arm64">darwin-arm64</option>
              </select>
            </label>
            <label>
              <span>风险</span>
              <select v-model="draft.risk_level">
                <option value="low">低</option>
                <option value="medium">中</option>
                <option value="high">高</option>
              </select>
            </label>
            <label class="form-grid__wide">
              <span>名称</span>
              <input v-model="draft.name" :placeholder="meta.namePlaceholder" autocomplete="off" />
            </label>
            <label class="form-grid__wide">
              <span>{{ meta.fileLabel }}</span>
              <input
                ref="fileInput"
                type="file"
                :accept="meta.accept || undefined"
                @change="onFile"
              />
            </label>
          </div>
          <p v-if="localError" class="banner banner-error" role="alert">{{ localError }}</p>
        </div>

        <footer class="drawer-footer mkt-upload-modal__footer">
          <button type="button" class="btn btn-ghost" :disabled="uploading" @click="emit('close')">
            取消
          </button>
          <button type="button" class="btn btn-primary" :disabled="uploading" @click="submit">
            {{ uploading ? "上传中…" : "上传为草稿" }}
          </button>
        </footer>
      </section>
    </div>
  </Teleport>
</template>
