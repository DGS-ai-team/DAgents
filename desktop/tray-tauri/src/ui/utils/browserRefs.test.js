import { describe, expect, it } from "vitest";
import {
  attachBrowserRefsToAssistants,
  collectBrowserRefsFromEntries,
  parseBrowserToolContent,
} from "./browserRefs.js";

describe("browserRefs", () => {
  it("parses tool content JSON", () => {
    const parsed = parseBrowserToolContent(
      JSON.stringify({ ok: true, detail: { status: "succeeded", value: "标题是 Example" } }),
    );
    expect(parsed.detail.value).toBe("标题是 Example");
  });

  it("collects refs between user and assistant slot", () => {
    const entries = [
      { kind: "user", text: "查一下" },
      {
        kind: "tool_result",
        id: 2,
        data: {
          tool_name: "browser_call",
          media: [{ id: "m1", url: "/v1/agents/a/media/m1", label: "browser_call" }],
          content: JSON.stringify({
            ok: true,
            detail: {
              status: "succeeded",
              observation: { content: "找到了标题" },
              action_results: [{ op: "navigate", status: "succeeded" }],
            },
          }),
        },
      },
      { kind: "assistant", text: "结果如下" },
    ];
    const refs = collectBrowserRefsFromEntries(entries, 2);
    expect(refs).toHaveLength(1);
    expect(refs[0].summary).toBe("找到了标题");
    expect(refs[0].action_results[0].op).toBe("navigate");
    expect(refs[0].screenshots).toEqual([
      { id: "m1", url: "/v1/agents/a/media/m1", label: "browser_call", caption: null },
    ]);
  });

  it("attaches refs on hydrate assistants", () => {
    const entries = [
      { kind: "user", text: "u" },
      {
        kind: "tool_result",
        data: {
          tool_name: "browser_evaluate",
          content: JSON.stringify({
            ok: true,
            detail: { status: "succeeded", value: "ok" },
          }),
        },
      },
      { kind: "assistant", text: "答" },
    ];
    attachBrowserRefsToAssistants(entries);
    expect(entries[2].browser_refs?.[0]?.summary).toBe("ok");
  });
});
