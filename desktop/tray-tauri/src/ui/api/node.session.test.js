/** @vitest-environment jsdom */
import { afterEach, describe, expect, it, vi } from "vitest";
import { createAgentTodo, deleteAgentTodo, editQueuedMessage, getAgentBrowserView, getAgentWatermarks, getConversationItemContent, getConversationResubmitTarget, getConversationRoundToolStep, listConversationLegacyItems, listConversationRoundItems, listConversationRoundRevisions, listConversationRoundToolSteps, listConversationRounds, pickPlatformDirectory, putAutoConfig, setInputQueuePaused, submitConversationResubmit, submitMessage, updateAgentTodo, withdrawQueuedMessage } from "./node.js";

afterEach(() => {
  globalThis.sessionStorage?.clear()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

describe("simplified Auto todo API", () => {
  it("uses the correct verbs and CAS request bodies", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation(async () => ({ ok: true, json: async () => ({}) }));
    await putAutoConfig("a/1", { expected_revision: 2, responsibility: "owner" });
    await createAgentTodo("a/1", { text: "x" });
    await updateAgentTodo("a/1", "t/2", { expected_revision: 3, status: "completed" });
    await deleteAgentTodo("a/1", "t/2", 4);
    expect(fetchMock.mock.calls[0][1]).toMatchObject({ method: "PUT" });
    expect(JSON.parse(fetchMock.mock.calls[0][1].body)).toEqual({ expected_revision: 2, responsibility: "owner" });
    expect(fetchMock.mock.calls[1][1]).toMatchObject({ method: "POST" });
    expect(JSON.parse(fetchMock.mock.calls[1][1].body)).toEqual({ text: "x" });
    expect(fetchMock.mock.calls[2][1]).toMatchObject({ method: "PATCH" });
    expect(JSON.parse(fetchMock.mock.calls[2][1].body).expected_revision).toBe(3);
    expect(fetchMock.mock.calls[3][1]).toMatchObject({ method: "DELETE" });
    expect(JSON.parse(fetchMock.mock.calls[3][1].body)).toEqual({ expected_revision: 4 });
    expect(String(fetchMock.mock.calls[2][0])).toContain("t%2F2");
  });
});

describe("read-only Agent browser view API", () => {
  it("uses the authenticated Desktop JSON bridge with the Agent-owned path and optional page id", async () => {
    const request = vi.fn(async () => ({ page_id: "page-1", image_base64: "cG5n" }));
    vi.stubGlobal("__DAGENTS_API_REQUEST__", request);
    await getAgentBrowserView("agent/1", "page/2");
    expect(request).toHaveBeenCalledWith({ method: "GET", path: "/v1/agents/agent%2F1/browser/view?page_id=page%2F2", body: undefined });
  });
});

describe("read-only conversation history API", () => {
  it("uses stable round and item cursor parameters through the Node bridge", async () => {
    const request = vi.fn(async () => ({}));
    vi.stubGlobal("__DAGENTS_API_REQUEST__", request);
    await listConversationRounds("agent/1", "agent/1", { beforeRoundId: "round/2", limit: 8 });
    await listConversationRoundRevisions("agent/1", "agent/1", "round/2");
    await listConversationRoundItems("agent/1", "agent/1", "round/2", { revisionId: "rev/3", afterItemId: "item/4", limit: 40 });
    await listConversationRoundToolSteps("agent/1", "agent/1", "round/2", { revisionId: "rev/3", sourceItemId: "item/4", afterToolCallId: "call/5", limit: 25 });
    await getConversationRoundToolStep("agent/1", "agent/1", "round/2", "call/5", { revisionId: "rev/3", sourceItemId: "item/4" });
    await getConversationItemContent("agent/1", "agent/1", "round/2", "item/4", { revisionId: "rev/3", part: "content" });
    await listConversationLegacyItems("agent/1", "agent/1", { afterItemId: "old/5", limit: 30 });
    expect(request.mock.calls.map(([options]) => options.path)).toEqual([
      "/v1/agents/agent%2F1/conversations/agent%2F1/rounds?before_round_id=round%2F2&limit=8",
      "/v1/agents/agent%2F1/conversations/agent%2F1/rounds/round%2F2/revisions",
      "/v1/agents/agent%2F1/conversations/agent%2F1/rounds/round%2F2/items?revision_id=rev%2F3&after_item_id=item%2F4&limit=40",
      "/v1/agents/agent%2F1/conversations/agent%2F1/rounds/round%2F2/tool-steps?revision_id=rev%2F3&source_item_id=item%2F4&after_tool_call_id=call%2F5&limit=25",
      "/v1/agents/agent%2F1/conversations/agent%2F1/rounds/round%2F2/tool-steps/call%2F5?revision_id=rev%2F3&source_item_id=item%2F4",
      "/v1/agents/agent%2F1/conversations/agent%2F1/rounds/round%2F2/items/item%2F4/content?revision_id=rev%2F3&part=content",
      "/v1/agents/agent%2F1/conversations/agent%2F1/legacy-items?after_item_id=old%2F5&limit=30",
    ]);
  });

  it("loads an editable target and resubmits it through its immutable revision identity", async () => {
    const request = vi.fn(async () => ({ accepted: true, input_seq: 13 }))
    vi.stubGlobal("__DAGENTS_API_REQUEST__", request)
    await getConversationResubmitTarget("agent/1", "agent/1", "round/2", "revision/3")
    await submitConversationResubmit("agent/1", "round/2", {
      revisionId: "revision/3",
      conversationRevision: 9,
      conversationEpoch: 4,
      preCheckpointRevision: 6,
      content: "edited text",
      contentParts: [{ type: "image_url", image_url: { url: "data:image/png;base64,aGVsbG8=" } }],
      fileRefs: [{ path: "notes.md", name: "notes.md", revision: "file-v1" }],
      userMessageName: "human",
    })

    expect(request.mock.calls[0][0]).toMatchObject({
      method: "GET",
      path: "/v1/agents/agent%2F1/conversations/agent%2F1/rounds/round%2F2/resubmit-target?revision_id=revision%2F3",
    })
    expect(request.mock.calls[1][0]).toMatchObject({
      method: "POST",
      path: "/v1/agents/agent%2F1/conversations/agent%2F1/rounds/round%2F2/resubmit",
      body: {
        expected_selected_revision_id: "revision/3",
        expected_conversation_revision: 9,
        pre_checkpoint_revision: 6,
        conversation_epoch: 4,
        content: "edited text",
        content_parts: [{ type: "image_url", image_url: { url: "data:image/png;base64,aGVsbG8=" } }],
        file_refs: [{ path: "notes.md", name: "notes.md", revision: "file-v1" }],
        user_message_name: "human",
        client_request_id: expect.any(String),
      },
    })
  })

  it("reconciles a lost resubmit response through its durable input receipt", async () => {
    const timeout = Object.assign(new Error("response lost"), { status: 503 })
    const request = vi.fn(async options => {
      if (options.method === "POST") throw timeout
      return { found: true, input_id: "agent-a/input/21", input_seq: 21, input_revision: 1, status: "consumed", conversation_epoch: 4 }
    })
    vi.stubGlobal("__DAGENTS_API_REQUEST__", request)

    await expect(submitConversationResubmit("agent-a", "round-1", {
      revisionId: "revision-1",
      conversationRevision: 8,
      conversationEpoch: 4,
      preCheckpointRevision: 5,
      content: "edited",
    })).resolves.toMatchObject({ accepted: true, input_seq: 21, duplicate: true })

    expect(request.mock.calls.map(([options]) => options.method)).toEqual(["POST", "GET"])
    expect(request.mock.calls[0][0].body.client_request_id).toBeTruthy()
    expect(request.mock.calls[1][0].path).toContain("conversation_epoch=4")
  })
});

describe("conversation watermark API", () => {
  it("uses the Node bridge and returns content-free sync targets", async () => {
    const request = vi.fn(async () => ({
      stream_epoch: "epoch-1",
      agents: [{ agent_id: "agent-1", conversation_id: "agent-1", conversation_revision: 4, latest_agent_seq: 9 }],
    }));
    vi.stubGlobal("__DAGENTS_API_REQUEST__", request);
    const result = await getAgentWatermarks();
    expect(request).toHaveBeenCalledWith({ method: "GET", path: "/v1/agents/watermarks", body: undefined });
    expect(result.agents[0]).toMatchObject({ conversation_revision: 4, latest_agent_seq: 9 });
  });
});

describe("structured workspace file references", () => {
  it("preserves anchor metadata in the Node message payload", async () => {
    const request = vi.fn(async () => ({ accepted: true }));
    vi.stubGlobal("__DAGENTS_API_REQUEST__", request);
    await submitMessage("agent-a", "compare these cells", null, [{
      id: "range-1",
      path: "reports/plan.csv",
      name: "plan.csv",
      revision: "rev-7",
      draft: true,
      annotation: "check total",
      anchor: { type: "sheet-range", label: "B2:B4", quote: "12\n14", location: { range: "B2:B4", sheet: "Summary" } },
    }]);
    const post = request.mock.calls.find(([options]) => options.method === "POST")?.[0]
    expect(post).toMatchObject({
      method: "POST",
      path: "/v1/messages",
      body: {
        agent_id: "agent-a",
        request_type: "message",
        content: "compare these cells",
        conversation_epoch: 1,
        file_refs: [{
          id: "range-1",
          path: "reports/plan.csv",
          name: "plan.csv",
          revision: "rev-7",
          draft: true,
          annotation: "check total",
          anchor: { type: "sheet-range", label: "B2:B4", quote: "12\n14", location: { range: "B2:B4", sheet: "Summary" } },
        }],
      },
    });
    expect(post.body.client_request_id).toEqual(expect.any(String))
  });
});

describe("message submission idempotency", () => {
  it("includes a stable request ID and the caller's conversation epoch", async () => {
    const request = vi.fn(async () => ({ accepted: true, input_seq: 4, status: "queued" }))
    vi.stubGlobal("__DAGENTS_API_REQUEST__", request)

    await submitMessage("agent-a", "hello", null, null, { conversationEpoch: 12 })

    const post = request.mock.calls[0][0]
    expect(post.method).toBe("POST")
    expect(post.body).toMatchObject({
      agent_id: "agent-a",
      request_type: "message",
      content: "hello",
      conversation_epoch: 12,
    })
    expect(post.body.client_request_id).toEqual(expect.any(String))
  })

  it("uses the optimistic row's request ID and publishes it before the POST", async () => {
    const order = []
    const request = vi.fn(async () => { order.push("post"); return { accepted: true, input_seq: 5 } })
    vi.stubGlobal("__DAGENTS_API_REQUEST__", request)

    await submitMessage("agent-a", "identified", null, null, {
      conversationEpoch: 3,
      clientRequestId: "client-row-5",
      onClientRequestId: id => order.push(`id:${id}`),
    })

    expect(order).toEqual(["id:client-row-5", "post"])
    expect(request.mock.calls[0][0].body.client_request_id).toBe("client-row-5")
  })

  it("reconciles a lost response from the durable Node receipt without posting twice", async () => {
    const timeout = new Error("request timed out")
    const request = vi.fn(async options => {
      if (options.method === "POST") throw timeout
      return { found: true, input_id: "agent-a/input/9", input_seq: 9, input_revision: 1, status: "queued", conversation_epoch: 3 }
    })
    vi.stubGlobal("__DAGENTS_API_REQUEST__", request)

    await expect(submitMessage("agent-a", "queued safely", null, null, { conversationEpoch: 3 })).resolves.toMatchObject({
      accepted: true,
      input_seq: 9,
      duplicate: true,
    })
    expect(request.mock.calls.map(([options]) => options.method)).toEqual(["POST", "GET"])
    expect(request.mock.calls[0][0].body.client_request_id).toBeTruthy()
    expect(request.mock.calls[1][0].path).toContain("conversation_epoch=3")
  })

  it("replays the exact request ID after a not-found receipt and transient retry", async () => {
    const unavailable = Object.assign(new Error("temporarily unavailable"), { status: 503 })
    let posts = 0
    const request = vi.fn(async options => {
      if (options.method === "POST" && ++posts === 1) throw unavailable
      if (options.method === "GET") return { found: false, conversation_epoch: 5 }
      return { accepted: true, input_seq: 6, status: "queued", conversation_epoch: 5 }
    })
    vi.stubGlobal("__DAGENTS_API_REQUEST__", request)

    await expect(submitMessage("agent-a", "retry with same identity", null, null, { conversationEpoch: 5 })).resolves.toMatchObject({
      accepted: true,
      input_seq: 6,
    })
    const postBodies = request.mock.calls.filter(([options]) => options.method === "POST").map(([options]) => options.body)
    expect(postBodies).toHaveLength(2)
    expect(postBodies[0].client_request_id).toBe(postBodies[1].client_request_id)
  })

  it("retains the request ID when reconciliation itself is unavailable", async () => {
    const request = vi.fn(async options => {
      throw new Error(options.method === "POST" ? "post response lost" : "receipt unavailable")
    })
    vi.stubGlobal("__DAGENTS_API_REQUEST__", request)

    await expect(submitMessage("agent-a", "retry after reconnect", null, null, { conversationEpoch: 8 })).rejects.toMatchObject({ unknownSubmission: true })
    const firstRequestId = request.mock.calls.find(([options]) => options.method === "POST")[0].body.client_request_id
    request.mockImplementation(async options => options.method === "POST" ? { accepted: true, input_seq: 10 } : { found: false })

    const retries = []
    await submitMessage("agent-a", "retry after reconnect", null, null, {
      conversationEpoch: 8,
      onClientRequestId: id => retries.push(id),
    })
    const postIds = request.mock.calls.filter(([options]) => options.method === "POST").map(([options]) => options.body.client_request_id)
    expect(postIds).toEqual([firstRequestId, firstRequestId])
    expect(retries).toEqual([firstRequestId])
  })

  it("does not reconcile or retry a definite API rejection", async () => {
    const conflict = Object.assign(new Error("conversation changed"), { status: 409 })
    const request = vi.fn(async () => { throw conflict })
    vi.stubGlobal("__DAGENTS_API_REQUEST__", request)

    await expect(submitMessage("agent-a", "stale conversation", null, null, { conversationEpoch: 2 })).rejects.toBe(conflict)
    expect(request).toHaveBeenCalledTimes(1)
  })
})

describe("queued message mutations", () => {
  it("uses revision CAS and keeps omitted attachment fields out of text-only edits", async () => {
    const request = vi.fn(async () => ({ input_id: "agent/input/7", input_seq: 7, revision: 2, status: "queued" }));
    vi.stubGlobal("__DAGENTS_API_REQUEST__", request);
    await editQueuedMessage("agent/1", 7, { expectedRevision: 1, content: "updated" });
    await withdrawQueuedMessage("agent/1", 7, 2);
    expect(request).toHaveBeenNthCalledWith(1, {
      method: "PATCH",
      path: "/v1/agents/agent%2F1/inputs/7",
      body: { expected_revision: 1, content: "updated" },
    });
    expect(request).toHaveBeenNthCalledWith(2, {
      method: "DELETE",
      path: "/v1/agents/agent%2F1/inputs/7",
      body: { expected_revision: 2 },
    });
  });
});

describe("durable input queue control API", () => {
  it("pauses and resumes the queue using the hydrate control revision", async () => {
    const request = vi.fn(async () => ({ queue_paused: true, queue_control_revision: 8 }));
    vi.stubGlobal("__DAGENTS_API_REQUEST__", request);

    await setInputQueuePaused("agent/1", true, 7);

    expect(request).toHaveBeenCalledWith({
      method: "POST",
      path: "/v1/agents/agent%2F1/inputs/control",
      body: { paused: true, expected_revision: 7 },
    });
  });
});

describe("platform directory picker", () => {
  it("uses the Desktop-owned native dialog when the Desktop bridge is available", async () => {
    const picker = vi.fn(async () => ({ ok: true, cancelled: false, path: "D:\\Projects\\agent" }));
    const fetchMock = vi.spyOn(globalThis, "fetch");
    vi.stubGlobal("__DAGENTS_PICK_DIRECTORY__", picker);

    await expect(pickPlatformDirectory()).resolves.toEqual({ ok: true, cancelled: false, path: "D:\\Projects\\agent" });
    expect(picker).toHaveBeenCalledTimes(1);
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("uses the Node API when the native picker bridge is not injected", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue({ ok: true, json: async () => ({ ok: true, cancelled: true }) });
    await expect(pickPlatformDirectory()).resolves.toEqual({ ok: true, cancelled: true });
    expect(String(fetchMock.mock.calls[0][0])).toContain("/v1/platform/directory-picker");
    expect(fetchMock.mock.calls[0][1]).toMatchObject({ method: "POST" });
  });
});
