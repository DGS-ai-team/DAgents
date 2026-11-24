import { performance } from "node:perf_hooks";
import { createServer } from "vite";
import { renderResourceMarkdown } from "../src/ui/utils/markdown.js";

const warmupRuns = 5;
const measuredRuns = 30;

function percentile(samples, fraction) {
  const sorted = [...samples].sort((a, b) => a - b);
  return Number(sorted[Math.min(sorted.length - 1, Math.floor(sorted.length * fraction))].toFixed(2));
}

function measureMarkdownRender(size, renderResourceMarkdown) {
  const paragraphs = [];
  let length = 0;
  for (let index = 0; length < size; index += 1) {
    const paragraph = `P${index} **bold** [link](https://e.test/${index}) words for parsing.\n\n`;
    paragraphs.push(paragraph);
    length += paragraph.length;
  }
  const text = paragraphs.join("");
  const render = () => {
    const result = renderResourceMarkdown(text);
    if (!result.html.includes(`P${paragraphs.length - 1}`)) {
      throw new Error(`markdown-${size}: last paragraph is missing from output`);
    }
  };
  for (let index = 0; index < warmupRuns; index += 1) render();
  const samples = [];
  for (let index = 0; index < measuredRuns; index += 1) {
    const started = performance.now();
    render();
    samples.push(performance.now() - started);
  }
  return {
    name: `markdown-${size}`,
    characters: text.length,
    paragraphs: paragraphs.length,
    samples: samples.length,
    medianMs: percentile(samples, 0.5),
    p95Ms: percentile(samples, 0.95),
    maxMs: Number(Math.max(...samples).toFixed(2)),
  };
}

function assistantEntries(count) {
  return Array.from({ length: count }, (_, index) => ({
    id: `assistant-${index}`,
    kind: "assistant",
    text: "historical message body",
  }));
}

function toolEntries(count) {
  const entries = [];
  for (let index = 0; index < count; index += 1) {
    const callId = `call-${index}`;
    entries.push(
      {
        id: `tool-call-${index}`,
        kind: "tool_call",
        data: { tool_call_id: callId, tool_name: "read_file", arguments: { path: `file-${index}.txt` } },
      },
      {
        id: `tool-result-${index}`,
        kind: "tool_result",
        data: { tool_call_id: callId, status: "succeeded", content: "ok" },
      },
    );
  }
  return entries;
}

function toolWindowEntries(roundCount, callsPerRound, visibleResultsPerRound) {
  const entries = [];
  for (let round = 0; round < roundCount; round += 1) {
    const roundId = `round-${round}`;
    entries.push({ id: `${roundId}-user`, kind: "user", text: `request ${round}` });
    for (let call = 0; call < callsPerRound; call += 1) {
      const callId = `${roundId}-call-${call}`;
      entries.push({
        id: `${callId}-entry`,
        kind: "tool_call",
        data: { tool_call_id: callId, tool_name: "read_file", arguments: { path: `file-${call}.txt` } },
      });
    }
    for (let result = 0; result < visibleResultsPerRound; result += 1) {
      const callId = `${roundId}-call-${result}`;
      entries.push({
        id: `${callId}-result`,
        kind: "tool_result",
        data: { tool_call_id: callId, status: "succeeded", content: "ok" },
      });
    }
  }
  return entries;
}

function hydrateAssistantEntries(count) {
  return assistantEntries(count).map((entry, index) => ({
    ...entry,
    id: `assistant-${index}`,
    item_id: `assistant-${index}`,
    item_version: 1,
    conversation_ordinal: index + 1,
    conversation_entry_index: 0,
    round_id: `round-${Math.floor(index / 3)}`,
    round_revision_id: `revision-${Math.floor(index / 3)}`,
    data: {},
  }));
}

function hydrateEntriesWithMedia(count) {
  return hydrateAssistantEntries(count).map((entry, index) => index % 50 === 0
    ? { ...entry, data: { media: [{ id: `media-${index}`, url: `/v1/agents/benchmark-agent/media/${index}` }] } }
    : entry);
}

async function measureMediaRestore(name, transcript, restoreTranscriptMedia) {
  const resolved = new Map();
  const loadSource = async (source) => {
    if (!resolved.has(source)) resolved.set(source, `blob:benchmark/${resolved.size}`);
    return resolved.get(source);
  };
  const source = { transcript };
  const started = performance.now();
  let restored = await restoreTranscriptMedia(source, loadSource);
  const coldMs = Number((performance.now() - started).toFixed(2));
  const changedRows = transcript.reduce((count, entry, index) => count + Number(restored.transcript[index] !== entry), 0);
  for (let index = 0; index < warmupRuns; index += 1) restored = await restoreTranscriptMedia(restored, loadSource);
  const samples = [];
  for (let index = 0; index < measuredRuns; index += 1) {
    const sampleStarted = performance.now();
    const next = await restoreTranscriptMedia(restored, loadSource);
    if (next !== restored) throw new Error(`${name}: stable media URLs should reuse the accepted snapshot`);
    samples.push(performance.now() - sampleStarted);
  }
  return {
    name,
    transcriptItems: transcript.length,
    mediaRowsChangedOnInitialRestore: changedRows,
    coldMs,
    stableMedianMs: percentile(samples, 0.5),
    stableP95Ms: percentile(samples, 0.95),
    samples: samples.length,
  };
}

function measureHydrateApply(name, transcript, modules, { forceStreamBuild = false } = {}) {
  const { mergeHydrateSnapshot, mergeTranscriptReadWindow } = modules.hydrateSnapshot;
  const { mergeTranscriptReferenceWindow } = modules.transcriptIdentity;
  const { reconcileAssistantDrafts, projectAssistantDrafts } = modules.assistantDrafts;
  const { desktopChatEntries } = modules.workbenchState;
  const { createDesktopChatItemsStore } = modules.desktopChatItems;
  const { buildStream } = modules.useStream;
  const { groupConsecutiveToolSteps } = modules.streamDisplay;
  const agentId = "benchmark-agent";
  const generation = 1;
  let windows = new Map();
  const store = createDesktopChatItemsStore();
  store.setScope(`${agentId}\0${generation}`);
  let currentSnapshot = null;
  let currentTranscript = [];
  let draftState = new Map();
  let result;
  let projectedRevision = -1;
  let stream = [];
  let display = [];
  let streamBuilds = 0;
  const stageSamples = Object.fromEntries([
    "hydrateMerge",
    "referenceMerge",
    "readWindowMerge",
    "draftReconcile",
    "draftProjection",
    "entryMapping",
    "storeReplace",
    "streamProjection",
  ].map((stage) => [stage, []]));

  const apply = (recordStages = false) => {
    const runStage = (name, fn) => {
      const started = performance.now();
      const value = fn();
      if (recordStages) stageSamples[name].push(performance.now() - started);
      return value;
    };
    const incoming = {
      history_revision: 1,
      conversation_epoch: 1,
      transcript,
      turn_state: { lifecycle_seq: 1, terminal: true },
    };
    const merged = runStage("hydrateMerge", () => mergeHydrateSnapshot(currentSnapshot, incoming, { historyRevision: 1, lifecycleSeq: 1 }));
    const referenceMerge = runStage("referenceMerge", () => mergeTranscriptReferenceWindow(windows, agentId, generation, incoming.transcript));
    windows = referenceMerge.windows;
    const visible = runStage("readWindowMerge", () => mergeTranscriptReadWindow(currentTranscript, referenceMerge.transcript, null, 1));
    const reconciled = runStage("draftReconcile", () => reconcileAssistantDrafts(draftState, agentId, generation, visible, { terminal: true }));
    draftState = reconciled.state;
    currentSnapshot = { ...merged.snapshot, transcript: visible };
    currentTranscript = visible;
    const projected = runStage("draftProjection", () => projectAssistantDrafts(visible, draftState, agentId, generation, { terminal: true }));
    const entries = runStage("entryMapping", () => desktopChatEntries(agentId, projected, null));
    const revision = runStage("storeReplace", () => store.replaceSnapshot(entries));
    const streamProjectionChanged = runStage("streamProjection", () => {
      if (!forceStreamBuild && revision === projectedRevision) return false;
      stream = buildStream(store.entries, []);
      display = groupConsecutiveToolSteps(stream);
      projectedRevision = revision;
      streamBuilds += 1;
      return true;
    });
    if (store.entries.length !== transcript.length || stream.length !== transcript.length) {
      throw new Error(`${name}: expected ${transcript.length} rows, got store=${store.entries.length}, stream=${stream.length}`);
    }
    return { transcriptItems: transcript.length, storeItems: store.entries.length, streamItems: stream.length, displayItems: display.length, streamProjectionChanged };
  };

  // The first pass creates the store's stable rows; measured passes represent
  // subsequent authoritative hydrate snapshots reusing those row identities.
  apply();
  for (let index = 0; index < warmupRuns; index += 1) apply();
  const samples = [];
  for (let index = 0; index < measuredRuns; index += 1) {
    const started = performance.now();
    result = apply(true);
    samples.push(performance.now() - started);
  }
  return {
    name,
    ...result,
    samples: samples.length,
    medianMs: percentile(samples, 0.5),
    p95Ms: percentile(samples, 0.95),
    maxMs: Number(Math.max(...samples).toFixed(2)),
    stageP95Ms: Object.fromEntries(Object.entries(stageSamples).map(([stage, values]) => [stage, percentile(values, 0.95)])),
    streamBuilds,
    storeMetrics: { ...store.metrics },
  };
}

function measureAssistantDraftUpdates(name, historySize, createConversationStore) {
  const agentId = "benchmark-agent";
  const generation = 1;
  const store = createConversationStore();
  store.setActiveScope(agentId, generation);
  store.items.replaceSnapshot(hydrateAssistantEntries(historySize));
  const entriesRef = store.items.entries;
  const stablePrefix = entriesRef[0];
  const eventAt = chunkSeq => ({
    agent_id: agentId,
    agent_seq: chunkSeq,
    event_type: "conversation_delta",
    data: {
      display_type: "delta",
      item_id: "benchmark-step/assistant/content",
      attempt_id: "benchmark-attempt",
      turn_id: "benchmark-turn",
      part: "content",
      chunk_seq: chunkSeq,
      content: "x",
    },
  });

  // First delta inserts the live row. Warmups then measure only point updates.
  let sequence = 1;
  store.applyAssistantDraftDelta(eventAt(sequence), { agentId, generation });
  for (let index = 0; index < warmupRuns; index += 1) {
    sequence += 1;
    store.applyAssistantDraftDelta(eventAt(sequence), { agentId, generation });
  }
  store.items.resetMetrics();
  const samples = [];
  for (let index = 0; index < measuredRuns; index += 1) {
    sequence += 1;
    const started = performance.now();
    const update = store.applyAssistantDraftDelta(eventAt(sequence), { agentId, generation });
    samples.push(performance.now() - started);
    if (!update.accepted || update.itemUpdate.inserted) throw new Error(`${name}: expected an in-place draft update`);
  }
  if (store.items.entries !== entriesRef || store.items.entries[0] !== stablePrefix) {
    throw new Error(`${name}: stable history prefix reference changed while streaming`);
  }
  return {
    name,
    historyItems: historySize,
    samples: samples.length,
    medianMs: percentile(samples, 0.5),
    p95Ms: percentile(samples, 0.95),
    maxMs: Number(Math.max(...samples).toFixed(2)),
    storeMetrics: { ...store.items.metrics },
  };
}

function measureConversationPatch(name, historySize, createConversationStore, { append = false } = {}) {
  const agentId = "benchmark-agent";
  const generation = 1;
  const store = createConversationStore();
  store.setActiveScope(agentId, generation);
  const transcript = hydrateAssistantEntries(historySize);
  store.items.replaceSnapshot(transcript);
  store.applyHydrateSnapshot({
    conversation_epoch: 1,
    conversation_revision: 1,
    history_revision: 1,
    transcript,
  }, { agentId, generation });
  const stablePrefix = store.hydrate.value.transcript[0];
  let sequence = 1;

  const apply = () => {
    sequence += 1;
    const ordinal = append ? historySize + sequence : historySize;
    const itemId = append ? `patch-tail-${sequence}` : `assistant-${historySize - 1}`;
    const itemVersion = append ? 1 : sequence + 1;
    const result = store.applyConversationPatch({
      agent_id: agentId,
      event_type: "conversation_patch",
      data: {
        conversation_id: agentId,
        conversation_revision: sequence + 1,
        history_revision: sequence + 1,
        upsert_items: [{
          id: itemId,
          item_id: itemId,
          item_version: itemVersion,
          conversation_ordinal: ordinal,
          conversation_entry_index: 0,
          kind: "assistant",
          text: `patch ${sequence}`,
          data: {},
        }],
        remove_item_ids: [],
      },
    }, { agentId, generation });
    if (!result.accepted || result.transcriptRowsVisited !== Number(append)) {
      throw new Error(`${name}: unexpected patch result ${JSON.stringify(result)}`);
    }
    if (store.hydrate.value.transcript[0] !== stablePrefix) {
      throw new Error(`${name}: stable prefix identity changed`);
    }
    return result;
  };

  for (let index = 0; index < warmupRuns; index += 1) apply();
  store.items.resetMetrics();
  const samples = [];
  for (let index = 0; index < measuredRuns; index += 1) {
    const started = performance.now();
    apply();
    samples.push(performance.now() - started);
  }
  return {
    name,
    historyItems: historySize,
    patchKind: append ? "tail_append" : "tail_item_update",
    stablePrefixReferenceRetained: store.hydrate.value.transcript[0] === stablePrefix,
    transcriptRowsVisitedPerPatch: Number(append),
    storeMetrics: { ...store.items.metrics },
    samples: samples.length,
    medianMs: percentile(samples, 0.5),
    p95Ms: percentile(samples, 0.95),
    maxMs: Number(Math.max(...samples).toFixed(2)),
  };
}

function measureProjection(name, entries, buildStream, groupConsecutiveToolSteps, expectedStreamItems) {
  const project = () => {
    const stream = buildStream(entries, []);
    const display = groupConsecutiveToolSteps(stream);
    if (stream.length !== expectedStreamItems) {
      throw new Error(`${name}: expected ${expectedStreamItems} projected items, got ${stream.length}`);
    }
    return { streamItems: stream.length, displayItems: display.length };
  };

  for (let index = 0; index < warmupRuns; index += 1) project();
  const samples = [];
  let output;
  for (let index = 0; index < measuredRuns; index += 1) {
    const started = performance.now();
    output = project();
    samples.push(performance.now() - started);
  }
  return {
    name,
    rawEntries: entries.length,
    ...output,
    samples: samples.length,
    medianMs: percentile(samples, 0.5),
    p95Ms: percentile(samples, 0.95),
    maxMs: Number(Math.max(...samples).toFixed(2)),
  };
}

function measurePendingInputProjection(name, transcriptSize, queueSize, desktopChatEntries) {
  const transcript = hydrateAssistantEntries(transcriptSize);
  const overlappingCount = Math.floor(queueSize / 2);
  const pendingInputs = Array.from({ length: queueSize }, (_, index) => ({
    input_id: `benchmark-input-${index}`,
    item_id: `benchmark-input-${index}`,
    kind: "user",
    text: `queued message ${index}`,
  }));
  for (let index = 0; index < overlappingCount; index += 1) {
    const itemId = pendingInputs[index].item_id;
    transcript[transcript.length - overlappingCount + index] = {
      id: itemId,
      item_id: itemId,
      item_version: 1,
      kind: "user",
      text: pendingInputs[index].text,
      data: {},
    };
  }
  pendingInputs.push({ id: "pending:local-optimistic", client_request_id: "local-optimistic", kind: "user", text: "optimistic message" });
  const expectedRows = transcriptSize + queueSize - overlappingCount + 1;
  const project = () => {
    const entries = desktopChatEntries("benchmark-agent", transcript, pendingInputs);
    if (entries.length !== expectedRows) throw new Error(`${name}: expected ${expectedRows} entries, got ${entries.length}`);
    return entries.length;
  };
  for (let index = 0; index < warmupRuns; index += 1) project();
  const samples = [];
  for (let index = 0; index < measuredRuns; index += 1) {
    const started = performance.now();
    project();
    samples.push(performance.now() - started);
  }
  return {
    name,
    transcriptItems: transcriptSize,
    pendingInputs: queueSize + 1,
    overlapItems: overlappingCount,
    projectedItems: expectedRows,
    samples: samples.length,
    medianMs: percentile(samples, 0.5),
    p95Ms: percentile(samples, 0.95),
    maxMs: Number(Math.max(...samples).toFixed(2)),
  };
}

const vite = await createServer({
  configFile: "vite.config.js",
  optimizeDeps: { noDiscovery: true, include: [] },
  server: { middlewareMode: true },
  appType: "custom",
});

try {
  const { buildStream } = await vite.ssrLoadModule("/src/ui/composables/useStream.js");
  const { groupConsecutiveToolSteps } = await vite.ssrLoadModule("/src/ui/utils/streamDisplay.js");
  const modules = {
    hydrateSnapshot: await vite.ssrLoadModule("/src/ui/utils/hydrateSnapshot.js"),
    transcriptIdentity: await vite.ssrLoadModule("/src/ui/utils/transcriptIdentity.js"),
    assistantDrafts: await vite.ssrLoadModule("/src/ui/utils/assistantDrafts.js"),
    workbenchState: await vite.ssrLoadModule("/src/ui/workbench-state.js"),
    desktopChatItems: await vite.ssrLoadModule("/src/ui/stores/desktopChatItems.js"),
    useStream: { buildStream },
    streamDisplay: { groupConsecutiveToolSteps },
    mediaRestore: await vite.ssrLoadModule("/src/ui/utils/agentMediaRestore.js"),
    conversationStore: (await vite.ssrLoadModule("/src/ui/stores/conversationStore.js")).createConversationStore,
  };
  const results = [
    ...[180, 2000, 10000].map((count) =>
      measureProjection(`assistant-${count}`, assistantEntries(count), buildStream, groupConsecutiveToolSteps, count),
    ),
    measureProjection("tool-pairs-1000", toolEntries(1000), buildStream, groupConsecutiveToolSteps, 1000),
    measureProjection(
      "tool-main-window-5x1000-calls-250-results",
      toolWindowEntries(5, 1000, 50),
      buildStream,
      groupConsecutiveToolSteps,
      5005,
    ),
  ];
  const toolWindow = results.at(-1);
  if (toolWindow.displayItems !== 10) {
    throw new Error(`tool-main-window: expected 5 user rows and 5 grouped tool rows, got ${toolWindow.displayItems} display rows`);
  }
  const hydrateApply = [180, 2000, 10000].flatMap((count) => {
    const transcript = hydrateAssistantEntries(count);
    return [
      measureHydrateApply(`hydrate-apply-stable-${count}`, transcript, modules),
      measureHydrateApply(`hydrate-apply-rebuild-${count}`, transcript, modules, { forceStreamBuild: true }),
    ];
  });
  const mediaRestore = await Promise.all([180, 2000, 10000].map((count) =>
    measureMediaRestore(`media-restore-${count}`, hydrateEntriesWithMedia(count), modules.mediaRestore.restoreTranscriptMedia),
  ));
  const draftUpdates = [180, 2000, 10000].map((count) =>
    measureAssistantDraftUpdates(`assistant-draft-update-${count}`, count, modules.conversationStore),
  );
  const conversationPatches = [180, 2000, 10000].flatMap((count) => [
    measureConversationPatch(`conversation-patch-update-${count}`, count, modules.conversationStore),
    measureConversationPatch(`conversation-patch-append-${count}`, count, modules.conversationStore, { append: true }),
  ]);
  const pendingProjection = [180, 2000, 10000].flatMap((transcriptSize) => [0, 64, 257].map((queueSize) =>
    measurePendingInputProjection(`pending-projection-${transcriptSize}-q${queueSize}`, transcriptSize, queueSize, modules.workbenchState.desktopChatEntries),
  ));
  const markdownRender = [5000, 60000, 300000].map((size) => measureMarkdownRender(size, renderResourceMarkdown));
  process.stdout.write(`${JSON.stringify({ warmupRuns, measuredRuns, results, hydrateApply, mediaRestore, draftUpdates, conversationPatches, pendingProjection, markdownRender }, null, 2)}\n`);
} finally {
  await vite.close();
}
