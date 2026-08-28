<script setup>
import { computed, onBeforeUnmount, ref } from "vue";
import { buildReadFilePreview } from "../utils/readFilePreview.js";
import { copyText } from "../utils/clipboard.js";

const props = defineProps({
  path: { type: String, default: "" },
  content: { type: String, default: "" },
});

const preview = computed(() => buildReadFilePreview(props.path, props.content));
const copyState = ref("");
let copyTimer = null;
const copyValue = computed(() => (preview.value.mode === "json" ? preview.value.jsonText : preview.value.body));

function clearCopyState() {
  if (copyTimer) {
    clearTimeout(copyTimer);
    copyTimer = null;
  }
}

async function copyContent() {
  if (!copyValue.value) return;
  try {
    copyState.value = (await copyText(copyValue.value)) ? "已复制" : "复制失败";
  } catch {
    copyState.value = "复制失败";
  }
  clearCopyState();
  copyTimer = setTimeout(() => {
    copyState.value = "";
    copyTimer = null;
  }, 1600);
}

onBeforeUnmount(clearCopyState);
</script>

<template>
  <div class="read-file-tool">
    <div class="read-file-tool__code-heading">
      <span class="read-file-tool__code-label">文本</span>
      <button
        v-if="copyValue"
        type="button"
        class="tool-output__action"
        aria-label="复制读取内容"
        title="复制读取内容"
        @click="copyContent"
      >{{ copyState || "复制内容" }}</button>
    </div>
    <div class="read-file-tool__preview">
      <div
        v-if="preview.mode === 'markdown'"
        class="tool-exec-bubble__markdown read-file-structured"
        v-html="preview.html"
      />
      <iframe
        v-else-if="preview.mode === 'html'"
        class="read-file-html-frame"
        sandbox=""
        :srcdoc="preview.body"
        title="HTML preview"
      />
      <pre v-else-if="preview.mode === 'json'" class="tool-exec-bubble__code read-file-structured">{{ preview.jsonText }}</pre>
      <div v-else-if="preview.mode === 'csv'" class="read-file-csv-wrap">
        <table class="read-file-csv">
          <tbody>
            <tr v-for="(row, ri) in preview.csvRows" :key="ri">
              <td v-for="(cell, ci) in row" :key="ci">{{ cell }}</td>
            </tr>
          </tbody>
        </table>
      </div>
      <pre v-else class="tool-exec-bubble__code read-file-structured">{{ preview.body }}</pre>
    </div>
  </div>
</template>

<style scoped>
.read-file-tool {
  display: flex;
  flex-direction: column;
  gap: 0;
  margin-top: 10px;
  overflow: hidden;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-sm);
  background: var(--color-code-bg);
}
.read-file-tool__code-heading {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  min-height: 24px;
  padding: 3px 7px 3px 8px;
  border-bottom: 1px solid var(--color-border);
  background: var(--color-surface-muted);
}
.read-file-tool__code-label {
  min-width: 0;
  color: var(--color-text-subtle);
  font-size: 10.5px;
  line-height: 1.35;
}
.read-file-tool__preview {
  border: 0;
  border-radius: 0;
  background: transparent;
  padding: 8px 10px 10px;
}
.read-file-html-frame {
  width: 100%;
  min-height: 120px;
  max-height: min(50vh, 360px);
  border: 0;
  background: var(--color-surface);
}
.read-file-csv-wrap {
  overflow: auto;
  max-height: min(50vh, 360px);
}
.read-file-csv {
  width: 100%;
  border-collapse: collapse;
  font-size: 12.5px;
}
.read-file-csv td {
  border: 1px solid var(--color-border);
  padding: 4px 8px;
  vertical-align: top;
  word-break: break-word;
}
</style>
