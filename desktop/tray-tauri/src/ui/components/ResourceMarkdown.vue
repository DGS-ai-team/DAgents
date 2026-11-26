<script setup>
import { computed, onBeforeUnmount, ref, useAttrs, watch } from "vue";
import { advanceMarkdownBoundaryScanner, createMarkdownBoundaryScanner } from "../utils/markdownStream.js";
import { collectMarkdownReferenceDefinitions } from "../utils/markdown.js";
import ResourceMarkdownContent from "./ResourceMarkdownContent.vue";

const props = defineProps({
  entry: { type: Object, required: true },
  agentId: { type: String, default: "" },
  source: { type: String, default: "main" },
  streaming: { type: Boolean, default: false },
});
defineOptions({ inheritAttrs: false });
const attrs = useAttrs();

const STREAM_RENDER_INTERVAL_MS = 48;
const STREAM_BLOCK_BATCH_SIZE = 16;
const STREAM_BLOCK_BATCH_CHAR_LIMIT = 16_384;
const STATIC_SEGMENT_THRESHOLD_CHARS = 24_000;
const STATIC_RENDER_BATCH_CHAR_LIMIT = 16_384;
const STATIC_RENDER_INTERVAL_MS = 16;
const segmented = ref(false);
const stableSegments = ref([]);
const referenceDefinitions = ref("");
const tailText = ref("");
const tailBaseEntry = ref({ ...props.entry, text: "" });
const generation = ref(0);
let referenceDefinitionSet = new Set();
let scanner = createMarkdownBoundaryScanner();
let committedOffset = 0;
let committedRevision = 0;
let segmentSequence = 0;
let pendingSource = "";
let currentSource = "";
let renderTimer = 0;
let staticRenderTimer = 0;
let staticSource = "";
let staticScannedOffset = 0;

const normalizedEntry = computed(() => ({
  ...props.entry,
  kind: props.entry?.kind || "assistant",
  text: String(props.entry?.text || ""),
}));

function normalizeSource(text) {
  return String(text || "").replace(/\r\n?/g, "\n");
}

function cancelRenderTimer() {
  if (!renderTimer) return;
  clearTimeout(renderTimer);
  renderTimer = 0;
}

function cancelStaticRenderTimer() {
  if (!staticRenderTimer) return;
  clearTimeout(staticRenderTimer);
  staticRenderTimer = 0;
}

function resetSegments() {
  cancelStaticRenderTimer();
  generation.value += 1;
  segmented.value = false;
  stableSegments.value = [];
  referenceDefinitions.value = "";
  referenceDefinitionSet = new Set();
  tailText.value = "";
  scanner = createMarkdownBoundaryScanner();
  committedOffset = 0;
  committedRevision = 0;
  segmentSequence = 0;
  staticSource = "";
  staticScannedOffset = 0;
}

function updateReferenceDefinitions(source, { replace = false } = {}) {
  const definitions = collectMarkdownReferenceDefinitions(source).split("\n").filter(Boolean);
  if (replace) referenceDefinitionSet = new Set(definitions);
  else for (const definition of definitions) referenceDefinitionSet.add(definition);
  referenceDefinitions.value = [...referenceDefinitionSet].join("\n");
}

function appendStableSegment(text) {
  if (!text) return;
  segmentSequence += 1;
  stableSegments.value.push({
    key: `${generation.value}:${segmentSequence}`,
    entry: { ...normalizedEntry.value, text },
  });
  // Stable Markdown segments already contain any complete definitions they
  // own. Parse only this new segment; the completed snapshot is parsed once
  // at finalization to include definitions still in the active tail.
  updateReferenceDefinitions(text);
}

function renderStreamSource(text, { final = false } = {}) {
  const source = normalizeSource(text);
  const appended = source.startsWith(currentSource);
  const result = advanceMarkdownBoundaryScanner(scanner, source, {
    // Retry attempts and provider corrections may rewrite an active snapshot.
    // Keep committed blocks only while the new text is an exact append.
    appendOnly: appended,
  });
  if (result.reset) {
    resetSegments();
    scanner = createMarkdownBoundaryScanner();
    advanceMarkdownBoundaryScanner(scanner, source, { appendOnly: true });
  }
  currentSource = source;
  const boundary = result.reset ? scanner.stableBoundary : result.stableBoundary;
  const stableRevision = scanner.stableRevision;
  const readyBlocks = stableRevision - committedRevision;
  const readyCharacters = boundary - committedOffset;
  const shouldCommit = boundary > committedOffset && (
    final || readyBlocks >= STREAM_BLOCK_BATCH_SIZE || readyCharacters >= STREAM_BLOCK_BATCH_CHAR_LIMIT
  );
  if (shouldCommit) {
    appendStableSegment(source.slice(committedOffset, boundary));
    committedOffset = boundary;
    committedRevision = stableRevision;
  }
  tailText.value = source.slice(committedOffset);
  if (final) updateReferenceDefinitions(source, { replace: true });
  if (final && stableSegments.value.length === 0) {
    // Small answers need no segmented wrapper: render their final Markdown once.
    segmented.value = false;
    return;
  }
  segmented.value = true;
}

function beginStreaming(text) {
  resetSegments();
  tailBaseEntry.value = { ...props.entry, text: "" };
  currentSource = "";
  pendingSource = normalizeSource(text);
  segmented.value = true;
  renderStreamSource(pendingSource);
}

function queueStreamingRender(text) {
  pendingSource = normalizeSource(text);
  if (!renderTimer) {
    renderTimer = setTimeout(() => {
      renderTimer = 0;
      renderStreamSource(pendingSource);
    }, STREAM_RENDER_INTERVAL_MS);
  }
}

function finishStreaming(text) {
  cancelRenderTimer();
  pendingSource = normalizeSource(text);
  renderStreamSource(pendingSource, { final: true });
}

function advanceStaticRender(expectedGeneration) {
  staticRenderTimer = 0;
  if (expectedGeneration !== generation.value || !staticSource) return;

  staticScannedOffset = Math.min(staticSource.length, staticScannedOffset + STATIC_RENDER_BATCH_CHAR_LIMIT);
  const sourcePrefix = staticSource.slice(0, staticScannedOffset);
  advanceMarkdownBoundaryScanner(scanner, sourcePrefix, { appendOnly: true });

  const boundary = scanner.stableBoundary;
  if (boundary > committedOffset) {
    appendStableSegment(staticSource.slice(committedOffset, boundary));
    committedOffset = boundary;
    committedRevision = scanner.stableRevision;
  }

  const final = staticScannedOffset >= staticSource.length;
  if (final) {
    updateReferenceDefinitions(staticSource, { replace: true });
    const remainder = staticSource.slice(committedOffset);
    if (remainder && stableSegments.value.length === 0) {
      // A single huge Markdown block has no safe split point. Render it once
      // instead of repeatedly reparsing prefixes and changing its semantics.
      segmented.value = false;
      tailBaseEntry.value = { ...normalizedEntry.value, text: staticSource };
      tailText.value = "";
    } else {
      tailText.value = remainder;
    }
    currentSource = staticSource;
    staticSource = "";
    return;
  }

  // Keep the unclosed tail bounded to one scan batch while stable blocks are
  // committed independently. This also lets a long fetched message appear
  // progressively without one large Markdown/DOM commit blocking the WebView.
  tailText.value = staticSource.slice(committedOffset, staticScannedOffset);
  staticRenderTimer = setTimeout(
    () => advanceStaticRender(expectedGeneration),
    STATIC_RENDER_INTERVAL_MS,
  );
}

function beginStaticSegmentation(text) {
  cancelRenderTimer();
  resetSegments();
  tailBaseEntry.value = { ...normalizedEntry.value, text: "" };
  currentSource = "";
  pendingSource = "";
  staticSource = normalizeSource(text);
  segmented.value = true;
  const expectedGeneration = generation.value;
  advanceStaticRender(expectedGeneration);
}

watch(
  [() => props.entry?.id, () => props.entry?.kind, () => props.entry?.attempt_id, () => props.entry?.text, () => props.streaming],
  ([id, kind, attemptId, text, streaming], previous) => {
    const identityChanged = !previous || id !== previous[0] || kind !== previous[1];
    const attemptChanged = Boolean(previous && streaming && (attemptId || previous[2]) && attemptId !== previous[2]);
    const isAssistant = (kind || "assistant") === "assistant";
    if (identityChanged || attemptChanged) {
      cancelRenderTimer();
      resetSegments();
      tailBaseEntry.value = { ...props.entry, text: "" };
      currentSource = "";
      pendingSource = "";
      if (streaming && isAssistant) beginStreaming(text);
      else if (isAssistant && normalizeSource(text).length > STATIC_SEGMENT_THRESHOLD_CHARS) beginStaticSegmentation(text);
      return;
    }
    if (streaming && isAssistant) {
      if (!segmented.value) beginStreaming(text);
      else if (normalizeSource(text) !== currentSource) queueStreamingRender(text);
      return;
    }
    if (isAssistant && normalizeSource(text).length > STATIC_SEGMENT_THRESHOLD_CHARS && normalizeSource(text) !== normalizeSource(previous?.[3])) {
      beginStaticSegmentation(text);
      return;
    }
    if (segmented.value && previous?.[4]) finishStreaming(text);
    else if (segmented.value) {
      // A completed message correction is a new render snapshot, not a stream append.
      cancelRenderTimer();
      resetSegments();
      currentSource = normalizeSource(text);
    }
  },
);

onBeforeUnmount(() => {
  cancelRenderTimer();
  cancelStaticRenderTimer();
});

if (props.streaming && (props.entry?.kind || "assistant") === "assistant") {
  beginStreaming(props.entry?.text || "");
} else if ((props.entry?.kind || "assistant") === "assistant" && normalizeSource(props.entry?.text).length > STATIC_SEGMENT_THRESHOLD_CHARS) {
  beginStaticSegmentation(props.entry?.text || "");
}

const staticKey = computed(() => `${generation.value}:${props.entry?.id || "message"}:${props.entry?.kind || "assistant"}`);
const tailEntry = computed(() => ({ ...tailBaseEntry.value, text: tailText.value }));
</script>

<template>
  <div class="resource-markdown-stream">
    <template v-if="segmented">
    <ResourceMarkdownContent
      v-for="segment in stableSegments"
      :key="segment.key"
      v-bind="attrs"
      :entry="segment.entry"
      :reference-definitions="referenceDefinitions"
      :agent-id="agentId"
      :source="source"
    />
    <ResourceMarkdownContent
      v-if="tailText"
      :key="`${generation}:tail`"
      v-bind="attrs"
      :entry="tailEntry"
      :reference-definitions="referenceDefinitions"
      :agent-id="agentId"
      :source="source"
    />
    </template>
    <ResourceMarkdownContent
      v-else
      :key="staticKey"
      v-bind="attrs"
      :entry="normalizedEntry"
      :reference-definitions="referenceDefinitions"
      :agent-id="agentId"
      :source="source"
    />
  </div>
</template>

<style>
.resource-markdown-stream { display: contents; }
</style>
