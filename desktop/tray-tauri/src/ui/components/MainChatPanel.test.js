// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { flushPromises, mount, shallowMount } from "@vue/test-utils";
import { nextTick, reactive, toRaw } from "vue";
import MainChatPanel from "./MainChatPanel.vue";
import MessageBubble from "./MessageBubble.vue";
import ApprovalBubble from "./ApprovalBubble.vue";
import UserInfoBubble from "./UserInfoBubble.vue";
import ToolGroupRow from "./ToolGroupRow.vue";
import { createTurnStateStore } from "../stores/turnState.js";
import * as streamBuilder from "../composables/useStream.js";
import * as markdown from "../utils/markdown.js";
import { projectAssistantDrafts, reduceAssistantDraftDelta } from "../utils/assistantDrafts.js";
import { desktopChatEntries } from "../workbench-state.js";
import { createDesktopChatItemsStore } from "../stores/desktopChatItems.js";
import {
  clearPerformanceDiagnostics,
  disablePerformanceDiagnostics,
  enablePerformanceDiagnostics,
  getPerformanceDiagnosticsSnapshot,
} from "../stores/performanceDiagnostics.js";
import * as nodeApi from "../api/node.js";

let wrapper;
let frames;
let turnStateStore;
let applyTurnState;

async function flushFrames() {
  while (frames.length) {
    const current = frames;
    frames = [];
    current.forEach((callback) => callback());
    await nextTick();
    await flushPromises();
  }
}

beforeEach(() => {
  const turnLifecycle = createTurnStateStore();
  turnStateStore = turnLifecycle.state;
  applyTurnState = turnLifecycle.applyTurnState;
  frames = [];
  vi.stubGlobal("requestAnimationFrame", (callback) => {
    frames.push(callback);
    return frames.length;
  });
  vi.stubGlobal("cancelAnimationFrame", vi.fn());
});

afterEach(() => {
  wrapper?.unmount();
  wrapper = null;
  disablePerformanceDiagnostics();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("MainChatPanel tool lifecycle", () => {
  it("rebuilds a tool row when authoritative execution changes without changing running IDs", async () => {
    const call = {
      id: "tool-call-row",
      kind: "tool_call",
      blockId: "call-approval",
      data: { tool_call_id: "call-approval", tool_name: "write_file", arguments: "{}" },
    };
    applyTurnState({
      authority: "turn_coordinator",
      phase: "tool_waiting",
      turn_id: "turn-1",
      generation: 1,
      lifecycle_seq: 1,
      tool_executions: [{ tool_call_id: "call-approval", status: "proposed" }],
    });

    wrapper = shallowMount(MainChatPanel, { props: { entries: [call], turnState: turnStateStore } });
    await flushFrames();
    let step = wrapper.findComponent(ToolGroupRow).props("steps")[0];
    expect(step.phase).toBe("pending");

    applyTurnState({
      authority: "turn_coordinator",
      phase: "tool_waiting",
      turn_id: "turn-1",
      generation: 1,
      lifecycle_seq: 2,
      tool_executions: [{ tool_call_id: "call-approval", status: "denied" }],
    });
    await nextTick();
    await flushFrames();

    step = wrapper.findComponent(ToolGroupRow).props("steps")[0];
    expect(step.phase).toBe("rejected");
  });
});

describe("MainChatPanel performance diagnostics", () => {
  it("records projection and Vue DOM commit spans only when diagnostics are enabled", async () => {
    clearPerformanceDiagnostics();
    enablePerformanceDiagnostics();
    wrapper = shallowMount(MainChatPanel, {
      props: {
        entries: [{ id: "perf-user-1", kind: "user", text: "hello" }],
        entriesRevision: 1,
      },
    });
    await flushFrames();
    await nextTick();

    const snapshot = getPerformanceDiagnosticsSnapshot();
    expect(snapshot.durations["stream.projection"]?.count).toBe(1);
    expect(snapshot.durations["stream.build"]?.count).toBe(1);
    expect(snapshot.durations["stream.render_commit"]?.count).toBe(1);
    expect(snapshot.durations["stream.layout"]?.count).toBe(1);
    expect(snapshot.runtime).toMatchObject({ entries: 1, streamItems: 1, visibleItems: 1 });
  });
});

describe("MainChatPanel tool detail pagination", () => {
  it("loads omitted tool rows in stable 50-item pages while preserving call/result pairing", async () => {
    const request = vi.spyOn(nodeApi, "listConversationRoundToolSteps").mockResolvedValue({
      round_id: "round-large", revision_id: "revision-large", total_count: 120, has_more: true,
      next_after_tool_call_id: "call-100",
      steps: [{
        tool_call_id: "call-051",
        call_entry: { kind: "tool_call", id: "tool-call/call-051", item_id: "tool-call/call-051", data: { tool_call_id: "call-051", tool_name: "read_file", arguments: { call_purpose: "inspect" } } },
        result_entry: { kind: "tool_result", id: "result-051", item_id: "result-051", data: { tool_call_id: "call-051", status: "succeeded" } },
      }],
    });
    const marker = {
      kind: "tool_group_more", id: "more-50", item_id: "more-50", detail_item_id: "assistant-tools",
      tool_call_item_id: "assistant-tools", after_tool_call_id: "call-050", remaining_count: 70,
      round_id: "round-large", round_revision_id: "revision-large",
    };
    wrapper = shallowMount(MainChatPanel, { props: { agentId: "agent-large", entries: [marker] } });
    await flushFrames();

    expect(wrapper.get(".chat__tool-more").text()).toContain("70");
    await wrapper.get(".chat__tool-more").trigger("click");
    await flushPromises();
    await flushFrames();

    expect(request).toHaveBeenCalledWith("agent-large", "agent-large", "round-large", {
      revisionId: "revision-large", sourceItemId: "assistant-tools", afterToolCallId: "call-050", limit: 50,
    });
    const group = wrapper.findComponent(ToolGroupRow);
    expect(group.exists()).toBe(true);
    expect(group.props("steps")).toHaveLength(1);
    expect(group.props("steps")[0].callEntry.data.tool_call_id).toBe("call-051");
    expect(group.props("steps")[0].resultEntry.data.tool_call_id).toBe("call-051");
    expect(wrapper.get(".chat__tool-more").text()).toContain("69");
  });

  it("continues rendering successive lazy tool pages", async () => {
    const request = vi.spyOn(nodeApi, "listConversationRoundToolSteps")
      .mockResolvedValueOnce({
        round_id: "round-large", revision_id: "revision-large", total_count: 120, has_more: true,
        next_after_tool_call_id: "call-100",
        steps: [{
          tool_call_id: "call-051",
          call_entry: { kind: "tool_call", id: "call-051", data: { tool_call_id: "call-051", tool_name: "read_file" } },
          result_entry: { kind: "tool_result", id: "result-051", data: { tool_call_id: "call-051" } },
        }],
      })
      .mockResolvedValueOnce({
        round_id: "round-large", revision_id: "revision-large", total_count: 120, has_more: false,
        steps: [{
          tool_call_id: "call-101",
          call_entry: { kind: "tool_call", id: "call-101", data: { tool_call_id: "call-101", tool_name: "read_file" } },
          result_entry: { kind: "tool_result", id: "result-101", data: { tool_call_id: "call-101" } },
        }],
      });
    const marker = {
      kind: "tool_group_more", id: "more-50", item_id: "more-50", detail_item_id: "assistant-tools",
      tool_call_item_id: "assistant-tools", after_tool_call_id: "call-050", remaining_count: 70,
      round_id: "round-large", round_revision_id: "revision-large",
    };
    wrapper = shallowMount(MainChatPanel, { props: { agentId: "agent-large", entries: [marker] } });
    await flushFrames();

    await wrapper.get(".chat__tool-more").trigger("click");
    await flushPromises();
    await flushFrames();
    expect(wrapper.get(".chat__tool-more").text()).toContain("69");

    await wrapper.get(".chat__tool-more").trigger("click");
    await flushPromises();
    await flushFrames();

    expect(request).toHaveBeenCalledTimes(2);
    expect(request.mock.calls[1][3].afterToolCallId).toBe("call-100");
    const group = wrapper.findComponent(ToolGroupRow);
    expect(group.props("steps").map(step => step.callEntry.data.tool_call_id)).toEqual(["call-051", "call-101"]);
    expect(wrapper.find(".chat__tool-more").exists()).toBe(false);
  });
});

describe("MainChatPanel recent-history navigation", () => {
  it("keeps the mounted conversation window bounded and reveals older loaded rows on demand", async () => {
    const entries = Array.from({ length: 400 }, (_, index) => ({
      id: `history-${index}`,
      item_id: `history-${index}`,
      kind: "assistant",
      text: `historical message ${index}`,
      conversation_ordinal: index + 1,
    }));
    wrapper = shallowMount(MainChatPanel, { props: { entries } });
    await flushFrames();

    expect(wrapper.findAllComponents(MessageBubble)).toHaveLength(180);
    expect(wrapper.get(".chat__load-earlier").attributes("aria-label")).toContain("220");
    expect(wrapper.findAllComponents(MessageBubble)[0].props("entry").id).toBe("history-220");

    await wrapper.get(".chat__load-earlier").trigger("click");
    await nextTick();

    expect(wrapper.findAllComponents(MessageBubble)).toHaveLength(180);
    expect(wrapper.findAllComponents(MessageBubble)[0].props("entry").id).toBe("history-40");
    expect(wrapper.get(".chat__load-earlier").attributes("aria-label")).toContain("40");
  });

  it("keeps actual message components bounded for a 10,000-item loaded transcript", async () => {
    const entries = Array.from({ length: 10_000 }, (_, index) => ({
      id: `large-history-${index}`,
      item_id: `large-history-${index}`,
      kind: "assistant",
      text: `historical message ${index}`,
      conversation_ordinal: index + 1,
    }));
    wrapper = shallowMount(MainChatPanel, { props: { entries } });
    await flushFrames();

    expect(wrapper.findAllComponents(MessageBubble)).toHaveLength(180);
    expect(wrapper.findAllComponents(MessageBubble)[0].props("entry").id).toBe("large-history-9820");
    expect(wrapper.get(".chat__load-earlier").attributes("aria-label")).toContain("9820");

    await wrapper.get(".chat__load-earlier").trigger("click");
    await nextTick();

    expect(wrapper.findAllComponents(MessageBubble)).toHaveLength(180);
    expect(wrapper.findAllComponents(MessageBubble)[0].props("entry").id).toBe("large-history-9640");
  });

  it("opens the read-only history view when earlier conversation records exist", async () => {
    wrapper = shallowMount(MainChatPanel, {
      props: { hasEarlierHistory: true, entries: [{ id: "recent", kind: "assistant", text: "recent" }] },
    });
    await flushFrames();

    const link = wrapper.get(".chat__history-link");
    expect(link.text()).toContain("查看更早的对话历史");
    await link.trigger("click");
    expect(wrapper.emitted("open-history")).toEqual([[]]);
  });

  it("publishes an ordinal anchor while the user reads above the latest tail", async () => {
    wrapper = shallowMount(MainChatPanel, {
      props: {
        entries: [{ id: "recent", kind: "assistant", text: "recent", conversation_ordinal: 8 }],
      },
    });
    await flushFrames();

    const stream = wrapper.get(".chat__stream").element;
    Object.defineProperties(stream, {
      scrollHeight: { configurable: true, value: 1200 },
      scrollTop: { configurable: true, writable: true, value: 240 },
      clientHeight: { configurable: true, value: 400 },
    });
    vi.spyOn(stream, "getBoundingClientRect").mockReturnValue({ top: 10, bottom: 410, height: 400, left: 0, right: 600, width: 600, x: 0, y: 10, toJSON() {} });
    const visibleAnchor = document.createElement("div");
    visibleAnchor.dataset.conversationOrdinal = "6";
    visibleAnchor.dataset.streamItemKey = "visible-row-6";
    vi.spyOn(visibleAnchor, "getBoundingClientRect").mockReturnValue({ top: 30, bottom: 80, height: 50, left: 0, right: 600, width: 600, x: 0, y: 30, toJSON() {} });
    stream.querySelector(".chat__stream-content").appendChild(visibleAnchor);

    await wrapper.get(".chat__stream").trigger("scroll");
    await flushFrames();

    expect(wrapper.emitted("reading-window-change")).toEqual([[
      { following: false, anchorOrdinal: 6, endOrdinal: 8, anchorItemKey: "visible-row-6", anchorOffset: 20 },
    ]]);
  });

  it("restores the first visible stream item after content above it changes height", async () => {
    let onResize;
    class ResizeObserverStub {
      constructor(callback) { onResize = callback; }
      observe() {}
      disconnect() {}
    }
    vi.stubGlobal("ResizeObserver", ResizeObserverStub);
    wrapper = shallowMount(MainChatPanel, {
      props: {
        entries: [{ id: "anchor-entry", kind: "assistant", text: "anchor", conversation_ordinal: 3 }],
      },
      global: {
        stubs: {
          MessageBubble: {
            props: ["entry", "conversationOrdinal", "streamItemKey"],
            template: '<div class="msg--assistant" :data-conversation-ordinal="conversationOrdinal" :data-stream-item-key="streamItemKey">{{ entry.text }}</div>',
          },
        },
      },
    });
    await flushFrames();

    const stream = wrapper.get(".chat__stream").element;
    const anchor = wrapper.get(".msg--assistant").element;
    expect(anchor.dataset.streamItemKey).toBe("e-anchor-entry");
    Object.defineProperties(stream, {
      scrollHeight: { configurable: true, value: 1200 },
      clientHeight: { configurable: true, value: 400 },
      scrollTop: { configurable: true, writable: true, value: 300 },
    });
    vi.spyOn(stream, "getBoundingClientRect").mockReturnValue({ top: 10, bottom: 410, height: 400, left: 0, right: 600, width: 600, x: 0, y: 10, toJSON() {} });

    let documentTop = 110;
    vi.spyOn(anchor, "getBoundingClientRect").mockImplementation(() => ({ top: documentTop - (stream.scrollTop - 300) + 10, bottom: documentTop - (stream.scrollTop - 300) + 70, height: 60, left: 0, right: 600, width: 600, x: 0, y: 0, toJSON() {} }));
    await wrapper.get(".chat__stream").trigger("scroll");
    await flushFrames();
    expect(wrapper.emitted("reading-window-change")?.at(-1)?.[0]).toMatchObject({ anchorItemKey: "e-anchor-entry", anchorOffset: 110 });

    documentTop = 160;
    onResize();
    expect(stream.scrollTop).toBe(350);
    expect(anchor.getBoundingClientRect().top - stream.getBoundingClientRect().top).toBe(110);
  });

  it("restores an Agent's saved item and viewport offset after its transcript mounts", async () => {
    wrapper = shallowMount(MainChatPanel, {
      props: {
        entriesRevision: 3,
        readingWindow: {
          anchorOrdinal: 3,
          endOrdinal: 8,
          conversationEpoch: 6,
          anchorItemKey: "e-anchor-entry",
          anchorOffset: 110,
        },
        entries: [{ id: "anchor-entry", kind: "assistant", text: "anchor", conversation_ordinal: 3 }],
      },
      global: {
        stubs: {
          MessageBubble: {
            props: ["entry", "conversationOrdinal", "streamItemKey"],
            template: '<div class="msg--assistant" :data-conversation-ordinal="conversationOrdinal" :data-stream-item-key="streamItemKey">{{ entry.text }}</div>',
          },
        },
      },
    });
    const stream = wrapper.get(".chat__stream").element;
    Object.defineProperties(stream, {
      scrollHeight: { configurable: true, value: 1200 },
      clientHeight: { configurable: true, value: 400 },
      scrollTop: { configurable: true, writable: true, value: 300 },
    });
    const documentTop = 60;
    vi.spyOn(Element.prototype, "getBoundingClientRect").mockImplementation(function getRect() {
      if (this === stream) return { top: 10, bottom: 410, height: 400, left: 0, right: 600, width: 600, x: 0, y: 10, toJSON() {} };
      if (this.dataset?.streamItemKey === "e-anchor-entry") {
        const top = documentTop - (stream.scrollTop - 300) + 10;
        return { top, bottom: top + 60, height: 60, left: 0, right: 600, width: 600, x: 0, y: top, toJSON() {} };
      }
      return { top: 0, bottom: 0, height: 0, left: 0, right: 0, width: 0, x: 0, y: 0, toJSON() {} };
    });

    await flushFrames();
    const anchor = wrapper.get(".msg--assistant").element;
    expect(stream.scrollTop).toBe(250);
    expect(anchor.getBoundingClientRect().top - stream.getBoundingClientRect().top).toBe(110);
  });

  it("observes a stable stream content wrapper after the empty state is replaced", async () => {
    const observed = [];
    let onResize;
    class ResizeObserverStub {
      constructor(callback) { onResize = callback; }
      observe(element) { observed.push(element); }
      disconnect() {}
    }
    vi.stubGlobal("ResizeObserver", ResizeObserverStub);

    wrapper = shallowMount(MainChatPanel, { props: { entries: [] } });
    await flushFrames();
    const stream = wrapper.get(".chat__stream").element;
    const content = wrapper.get(".chat__stream-content").element;
    expect(observed).toEqual([content, stream]);

    let height = 900;
    let top = 0;
    Object.defineProperties(stream, {
      scrollHeight: { configurable: true, get: () => height },
      clientHeight: { configurable: true, value: 300 },
      scrollTop: {
        configurable: true,
        get: () => top,
        set: value => { top = Math.max(0, Math.min(Number(value) || 0, height - 300)); },
      },
    });
    await wrapper.setProps({ entries: [{ id: "first", kind: "assistant", text: "ready" }], entriesRevision: 1 });
    await flushFrames();

    height = 1300;
    onResize();
    expect(top).toBe(1000);
    expect(wrapper.get(".chat__stream-content").element).toBe(content);
  });
});

describe("MainChatPanel incremental history rendering", () => {
  it("keeps an identified streamed assistant item unique and installs its same-ID hydrate object", async () => {
    const draft = {
      id: "assistant-item-1",
      item_id: "assistant-item-1",
      attempt_id: "attempt-1",
      kind: "assistant",
      text: "stable answer",
      streaming: true,
      partial: true,
      data: {},
    };
    wrapper = shallowMount(MainChatPanel, {
      props: { agentId: "agent-a", entries: [draft] },
    });
    await flushFrames();
    expect(wrapper.findAllComponents(MessageBubble)).toHaveLength(1);
    expect(toRaw(wrapper.findComponent(MessageBubble).props("entry"))).toBe(draft);

    const final = { ...draft, attempt_id: undefined, partial: false, streaming: false };
    await wrapper.setProps({ entries: [final], entriesRevision: 1 });
    await flushFrames();

    expect(wrapper.findAllComponents(MessageBubble)).toHaveLength(1);
    expect(toRaw(wrapper.findComponent(MessageBubble).props("entry"))).toBe(final);
    expect(wrapper.findComponent(MessageBubble).props("entry").text).toBe("stable answer");
  });

  it("does not update or reparse a stable prefix when an assistant delta changes only the tail", async () => {
    const prefix = {
      id: "prefix-item",
      item_id: "prefix-item",
      item_version: 1,
      kind: "assistant",
      text: "Cached prefix **markdown**",
      data: {},
    };
    const delta = (chunkSeq, content) => ({
      event_type: "conversation_delta",
      agent_id: "agent-a",
      data: { display_type: "delta", item_id: "tail-item", attempt_id: "attempt-1", part: "content", chunk_seq: chunkSeq, content },
    });
    let draftState = reduceAssistantDraftDelta(new Map(), delta(1, "first token"), {
      agentId: "agent-a", generation: 1,
    }).state;
    const entriesFor = state => desktopChatEntries("agent-a", projectAssistantDrafts(
      [prefix], state, "agent-a", 1,
    ));
    const firstEntries = entriesFor(draftState);
    const parseSpy = vi.spyOn(markdown, "renderResourceMarkdown");
    wrapper = mount(MainChatPanel, {
      props: {
        agentId: "agent-a",
        entries: firstEntries,
        hitlQueue: [{ kind: "approval", data: { request_id: "approval-1" } }],
      },
      global: {
        stubs: {
          ApprovalBubble: true,
          MemoryConflictBubble: true,
          ScrollToTailButton: true,
          ToolGroupRow: true,
          ToolSummaryRow: true,
          UiIcon: true,
          UserInfoBubble: true,
          WorkspaceSwitcher: true,
        },
      },
    });
    await flushFrames();

    const messages = wrapper.findAllComponents(MessageBubble);
    expect(messages).toHaveLength(2);
    const prefixUpdate = vi.spyOn(messages[0].vm.$, "update");
    const prefixParseCalls = parseSpy.mock.calls.filter(([text]) => text === prefix.text).length;
    const allPrefixParseCalls = prefixParseCalls;
    draftState = reduceAssistantDraftDelta(draftState, delta(2, " and more"), {
      agentId: "agent-a", generation: 1,
    }).state;
    const updatedEntries = entriesFor(draftState);
    const updatedTail = updatedEntries[1];
    expect(updatedEntries[0]).toBe(prefix);

    await wrapper.setProps({ entries: updatedEntries, entriesRevision: 1 });
    await flushFrames();

    expect(wrapper.findAllComponents(MessageBubble)).toHaveLength(2);
    expect(wrapper.findAllComponents(MessageBubble)[0].props("entry").item_id).toBe(prefix.item_id);
    expect(prefixUpdate).not.toHaveBeenCalled();
    expect(parseSpy.mock.calls.filter(([text]) => text === prefix.text).length).toBe(allPrefixParseCalls);
    await new Promise(resolve => setTimeout(resolve, 60));
    await flushFrames();
    expect(parseSpy.mock.calls.filter(([text]) => text === updatedTail.text)).toHaveLength(1);
    expect(wrapper.findComponent(ApprovalBubble).exists()).toBe(true);
  });

  it("keeps ordinary tail deltas independent of a 2000-item loaded transcript", async () => {
    const event = (chunkSeq, content) => ({
      event_type: "conversation_delta",
      agent_id: "agent-a",
      data: { display_type: "delta", item_id: "tail-item", attempt_id: "attempt-1", part: "content", chunk_seq: chunkSeq, content },
    });
    let draftState = new Map();
    const history = Array.from({ length: 1996 }, (_, index) => ({
      id: `history-${index}`,
      item_id: `history-${index}`,
      item_version: 1,
      kind: "assistant",
      text: `history ${index}`,
      data: {},
    }));
    history.push(
      { id: "call-row", item_id: "call-row", kind: "tool_call", data: { tool_call_id: "call-1", tool_name: "read_file", arguments: "{}" } },
      { id: "result-row", item_id: "result-row", kind: "tool_result", data: { tool_call_id: "call-1", content: "ok" } },
      { id: "visible-prefix", item_id: "visible-prefix", item_version: 1, kind: "assistant", text: "visible prefix", data: {} },
    );
    const store = createDesktopChatItemsStore();
    store.setScope("agent-a\0generation-7");
    store.replaceSnapshot(desktopChatEntries("agent-a", history));
    const prefix = store.entries.at(-1);
    let revision = store.revision.value;
    const buildSpy = vi.spyOn(streamBuilder, "buildStream");
    wrapper = shallowMount(MainChatPanel, {
      props: {
        agentId: "agent-a",
        entries: store.entries,
        entriesRevision: revision,
        hitlQueue: [{ kind: "approval", data: { request_id: "approval-1" } }],
      },
    });
    await flushFrames();
    expect(store.entries).toHaveLength(1999);
    expect(wrapper.findComponent(ToolGroupRow).exists()).toBe(true);
    expect(wrapper.findComponent(ApprovalBubble).exists()).toBe(true);
    buildSpy.mockClear();
    store.resetMetrics();

    const firstChunk = reduceAssistantDraftDelta(draftState, event(1, "first"), {
      agentId: "agent-a", generation: 7,
    });
    expect(firstChunk.accepted).toBe(true);
    draftState = firstChunk.state;
    const firstInsert = store.upsertDraft(firstChunk.entry);
    expect(firstInsert.inserted).toBe(true);
    revision = firstInsert.revision;
    await wrapper.setProps({
      entriesRevision: firstInsert.revision,
      entriesChange: { revision: firstInsert.revision, kind: "insert", index: firstInsert.index, itemId: firstInsert.entry.item_id },
    });
    await flushFrames();
    const tail = store.entries.at(-1);
    expect(store.entries).toHaveLength(2000);

    for (let sequence = 2; sequence <= 8; sequence += 1) {
      const reduced = reduceAssistantDraftDelta(draftState, event(sequence, ` token-${sequence}`), {
        agentId: "agent-a", generation: 7,
      });
      expect(reduced.accepted).toBe(true);
      draftState = reduced.state;
      store.upsertDraft(reduced.entry);
      await flushFrames();
    }

    expect(store.entries.at(-2)).toBe(prefix);
    expect(store.entries.at(-1)).toBe(tail);
    expect(tail.text).toBe("first token-2 token-3 token-4 token-5 token-6 token-7 token-8");
    expect(wrapper.findAllComponents(MessageBubble).at(-1).props("entry").text).toBe(tail.text);
    expect(store.revision.value).toBe(revision);
    expect(store.metrics.snapshotRowsVisited).toBe(0);
    expect(store.metrics.mapperCalls).toBe(0);
    expect(store.metrics.pointUpdates).toBe(7);
    expect(store.metrics.inserts).toBe(1);
    expect(buildSpy).not.toHaveBeenCalled();
    expect(wrapper.findComponent(ToolGroupRow).exists()).toBe(true);
    expect(wrapper.findComponent(ApprovalBubble).exists()).toBe(true);
    buildSpy.mockRestore();
  });

  it("keeps a failed-turn message visible alongside already generated partial output", async () => {
    wrapper = shallowMount(MainChatPanel, {
      props: {
        agentId: "agent-a",
        entries: [
          { id: "user-1", kind: "user", text: "question" },
          { id: "partial-answer", item_id: "partial-answer", kind: "assistant", text: "partial answer", partial: true, streaming: true },
        ],
        turnError: "模型服务暂时不可用，已重试2次。已保留本轮已生成的部分内容。",
      },
    });
    await flushFrames();
    expect(wrapper.find('[role="alert"]').text()).toContain("已重试2次");
    expect(wrapper.findAllComponents(MessageBubble).at(-1).props("entry").text).toBe("partial answer");
  });

  it("labels model metadata as the current selection while the prior turn error remains visible", async () => {
    wrapper = shallowMount(MainChatPanel, {
      props: {
        turnError: "模型账户额度不足。",
        modelRecoveryAvailable: true,
        modelName: "qwen3.7-plus",
        modelProfile: "qwen-free",
      },
    });

    expect(wrapper.get('[role="alert"]').text()).toBe("模型账户额度不足。");
    expect(wrapper.get(".chat__turn-error-model").text()).toBe("当前选择 · 模型：qwen3.7-plus · 配置：qwen-free");
    const switchModel = wrapper.get('[aria-label="切换模型配置"]');
    expect(switchModel.attributes('title')).toBe('切换模型配置');
    const openSettings = wrapper.get('[aria-label="打开模型设置"]');
    await switchModel.trigger("click");
    await openSettings.trigger("click");
    expect(wrapper.emitted("open-model-picker")).toEqual([[]]);
    expect(wrapper.emitted("open-model-settings")).toEqual([[]]);

    await wrapper.setProps({ modelName: "deepseek-flash", modelProfile: "deepseek-v4-flash" });
    expect(wrapper.get('[role="alert"]').text()).toBe("模型账户额度不足。");
    expect(wrapper.get(".chat__turn-error-model").text()).toBe("当前选择 · 模型：deepseek-flash · 配置：deepseek-v4-flash");
  });

  it("never offers to replay a failed user turn after a completed file write", async () => {
    wrapper = shallowMount(MainChatPanel, {
      props: {
        entries: [
          { id: "user-1", kind: "user", text: "update the report" },
          { id: "write-call", kind: "tool_call", blockId: "write-block", data: { tool_call_id: "write-1", tool_name: "write_file", arguments: { path: "report.md" } } },
          { id: "write-result", kind: "tool_result", blockId: "write-block", data: { tool_call_id: "write-1", tool_name: "write_file", status: "succeeded", content: "wrote report.md" } },
        ],
        turnError: "模型服务暂时不可用。",
        modelRecoveryAvailable: true,
      },
    });
    await flushFrames();

    expect(wrapper.findComponent(ToolGroupRow).exists()).toBe(true);
    expect(wrapper.find('[aria-label="重试本轮"]').exists()).toBe(false);
    expect(wrapper.find('[aria-label="切换模型配置"]').exists()).toBe(true);
  });

  it("retains an approval card after a long transcript reaches the tail", async () => {
    const entries = Array.from({ length: 220 }, (_, index) => ({
      id: `assistant-${index}`,
      kind: "assistant",
      text: `historical message ${index}`,
    }));
    wrapper = shallowMount(MainChatPanel, {
      props: {
        entries,
        hitlQueue: [{
          kind: "approval",
          data: {
            approval_id: "approval-at-tail",
            approval_args: { tool_calls: [{ id: "write-review", name: "write_file", arguments: { path: "review.md" } }] },
          },
        }],
      },
    });
    await flushFrames();

    expect(wrapper.findComponent(ApprovalBubble).exists()).toBe(true);
    expect(wrapper.findComponent(ApprovalBubble).props("requestKey")).toBe("approval::approval-at-tail");
  });

  it("updates an identity-bearing draft row in place without rebuilding history", async () => {
    const buildSpy = vi.spyOn(streamBuilder, "buildStream");
    const draft = reactive({
      id: "answer-item",
      item_id: "answer-item",
      attempt_id: "attempt-1",
      kind: "assistant",
      text: "alpha",
      partial: true,
      streaming: true,
      data: {},
    });
    wrapper = shallowMount(MainChatPanel, {
      props: {
        agentId: "agent-a",
        entries: [{ id: "user-1", kind: "user", text: "question" }, draft],
      },
    });
    await flushFrames();
    expect(buildSpy).toHaveBeenCalledTimes(1);
    expect(wrapper.findAllComponents(MessageBubble).at(-1).props("entry")).toBe(draft);
    expect(wrapper.findAllComponents(MessageBubble).at(-1).props("entry").text).toBe("alpha");

    draft.text = "bravo";
    await nextTick();
    expect(wrapper.findAllComponents(MessageBubble).at(-1).props("entry").text).toBe("bravo");
    expect(buildSpy).toHaveBeenCalledTimes(1);
    buildSpy.mockRestore();
  });

  it("rebuilds a same-length history snapshot when its structural revision changes", async () => {
    wrapper = shallowMount(MainChatPanel, {
      props: { entries: [{ id: "assistant-1", kind: "assistant", text: "old" }] },
    });
    await flushFrames();
    await wrapper.setProps({ entries: [{ id: "assistant-1", kind: "assistant", text: "new" }], entriesRevision: 1 });
    await nextTick();
    await flushFrames();

    expect(wrapper.findComponent(MessageBubble).props("entry").text).toBe("new");
  });

  it("rebuilds same-reference history rows when the item store advances its revision", async () => {
    const entries = reactive([{
      id: "tool-1",
      kind: "tool_call",
      blockId: "call-1",
      data: { tool_call_id: "call-1", tool_name: "read_file", arguments: "one" },
    }]);
    wrapper = shallowMount(MainChatPanel, { props: { entries } });
    await flushFrames();

    entries[0].data.arguments = "two";
    await wrapper.setProps({ entriesRevision: 1 });
    await nextTick();
    await flushFrames();

    expect(wrapper.findComponent(ToolGroupRow).props("steps")[0].callEntry.data.arguments).toBe("two");
  });

  it("forwards the approval selection with its queue index and uses reject-all for the batch action", async () => {
    wrapper = shallowMount(MainChatPanel, {
      props: {
        hitlQueue: [{
          kind: "approval",
          data: {
            approval_id: "approval-main",
            approval_args: { tool_calls: [{ id: "call-a", name: "read_file", arguments: { path: "a.txt" } }] },
          },
        }],
      },
    });
    await flushFrames();

    expect(wrapper.findComponent(ApprovalBubble).props("requestKey")).toBe("approval::approval-main");
    wrapper.findComponent(ApprovalBubble).vm.$emit("approve-selection", ["call-a"]);
    wrapper.findComponent(ApprovalBubble).vm.$emit("reject-all");
    expect(wrapper.emitted("approve-selection")).toEqual([[{ index: 0, approvedCallIds: ["call-a"] }]]);
    expect(wrapper.emitted("reject-all")).toEqual([[0]]);
    expect(wrapper.emitted("approve-one")).toBeUndefined();
    expect(wrapper.emitted("approve-all")).toBeUndefined();
  });
});

describe("MainChatPanel user input requests", () => {
  it("forwards an explicit user-information card answer without using the normal composer", async () => {
    wrapper = shallowMount(MainChatPanel, {
      props: {
        hitlQueue: [{
          kind: "user_information",
          data: { request_id: "ask-1", user_information_args: { question: "Which date range?", options: [{ id: "q1", label: "This quarter" }] } },
        }],
      },
    });
    await flushFrames();

    const request = wrapper.findComponent(UserInfoBubble);
    expect(request.exists()).toBe(true);
    request.vm.$emit("submit", "This quarter");
    expect(wrapper.emitted("user-info-submit")).toEqual([[0, "This quarter"]]);
  });
});
