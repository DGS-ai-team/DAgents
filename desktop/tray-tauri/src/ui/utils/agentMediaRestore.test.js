import { describe, expect, it, vi } from "vitest"
import { mergeRestoredTranscriptMedia, restoreTranscriptMedia } from "./agentMediaRestore.js"

describe("restoreTranscriptMedia", () => {
  it("preserves the snapshot and transcript row references when there is no media to restore", async () => {
    const row = { item_id: "assistant-1", kind: "assistant", text: "plain text", data: { source: "node" } }
    const snapshot = { transcript: [row] }

    const restored = await restoreTranscriptMedia(snapshot, async source => source)

    expect(restored).toBe(snapshot)
    expect(restored.transcript).toBe(snapshot.transcript)
    expect(restored.transcript[0]).toBe(row)
  })

  it("does not reload or clone media that is already local to the Desktop", async () => {
    const row = {
      item_id: "user-1",
      kind: "user",
      images: ["blob:desktop/image", "data:image/png;base64,inline"],
      data: { media: [{ id: "med_1", url: "blob:desktop/med_1" }] },
    }
    const snapshot = { transcript: [row] }
    const loadSource = vi.fn(async source => source)

    const restored = await restoreTranscriptMedia(snapshot, loadSource)

    expect(restored).toBe(snapshot)
    expect(restored.transcript[0]).toBe(row)
    expect(loadSource).not.toHaveBeenCalled()
  })

  it("only replaces rows whose media URL actually changes", async () => {
    const plain = { item_id: "assistant-1", kind: "assistant", text: "plain text", data: {} }
    const image = {
      item_id: "tool-result-1",
      kind: "tool_result",
      data: { media: [{ id: "med_1", url: "/media/med_1", label: "result" }] },
    }
    const snapshot = { transcript: [plain, image] }

    const restored = await restoreTranscriptMedia(snapshot, async source => source.startsWith("/media/")
      ? `blob:${source.split("/").pop()}`
      : source)

    expect(restored).not.toBe(snapshot)
    expect(restored.transcript[0]).toBe(plain)
    expect(restored.transcript[1]).not.toBe(image)
    expect(restored.transcript[1].data).not.toBe(image.data)
    expect(restored.transcript[1].data.media[0].url).toBe("blob:med_1")
  })

  it("resolves nested tool-result media while preserving metadata", async () => {
    const snapshot = {
      transcript: [{
        kind: "tool_result",
        data: {
          tool_name: "read_image",
          media: [{ id: "med_1", url: "/v1/agents/agent-a/media/med_1", label: "read_image" }],
        },
      }],
    }
    const loadSource = async source => `blob:desktop/${source.split("/").pop()}`

    const restored = await restoreTranscriptMedia(snapshot, loadSource)

    expect(restored.transcript[0].data.media).toEqual([{
      id: "med_1",
      url: "blob:desktop/med_1",
      label: "read_image",
    }])
    expect(snapshot.transcript[0].data.media[0].url).toBe("/v1/agents/agent-a/media/med_1")
  })

  it("continues restoring top-level user images and media", async () => {
    const restored = await restoreTranscriptMedia({ transcript: [{
      kind: "user",
      images: ["/v1/agents/agent-a/media/med_1"],
      media: [{ id: "med_2", url: "/v1/agents/agent-a/media/med_2" }],
    }] }, async source => `blob:desktop/${source.split("/").pop()}`)

    expect(restored.transcript[0].images).toEqual(["blob:desktop/med_1"])
    expect(restored.transcript[0].media[0].url).toBe("blob:desktop/med_2")
  })
})

describe("mergeRestoredTranscriptMedia", () => {
  it("rebases an image restore onto newer unrelated transcript items", () => {
    const currentImage = {
      item_id: "user-image",
      item_version: 3,
      kind: "user",
      images: ["dagents-media://media-1"],
      content: "look at this",
    }
    const newerAssistant = { item_id: "assistant-new", item_version: 1, kind: "assistant", content: "new response" }
    const restored = {
      ...currentImage,
      images: ["blob:desktop/media-1"],
    }

    const merged = mergeRestoredTranscriptMedia([currentImage, newerAssistant], [restored])

    expect(merged).toEqual([{ ...currentImage, images: ["blob:desktop/media-1"] }, newerAssistant])
    expect(merged[0].content).toBe("look at this")
    expect(merged[1]).toBe(newerAssistant)
  })

  it("does not apply restored media after the source item version changes", () => {
    const latest = { item_id: "user-image", item_version: 4, kind: "user", images: ["dagents-media://new-media"] }
    const stale = { item_id: "user-image", item_version: 3, kind: "user", images: ["blob:desktop/old-media"] }

    expect(mergeRestoredTranscriptMedia([latest], [stale])).toBeNull()
  })
})
