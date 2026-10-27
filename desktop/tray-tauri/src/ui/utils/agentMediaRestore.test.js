import { describe, expect, it } from "vitest"
import { restoreTranscriptMedia } from "./agentMediaRestore.js"

describe("restoreTranscriptMedia", () => {
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
