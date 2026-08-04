<script setup>
import { computed } from "vue";
import { buildReadFilePreview } from "../utils/readFilePreview.js";

const props = defineProps({
  path: { type: String, default: "" },
  content: { type: String, default: "" },
});

const preview = computed(() => buildReadFilePreview(props.path, props.content));
</script>

<template>
  <div class="read-file-tool">
    <div v-if="preview.meta" class="read-file-structured-meta-bar">
      <span class="read-file-structured-meta-bar__item">
        <span class="read-file-structured-meta-bar__label">文件</span>
        <span class="read-file-structured-meta-bar__value">{{ preview.path || "�? }}</span>
      </span>
    </div>
    <div class="write-file-tool__preview read-file-structured__preview-wrap">
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
