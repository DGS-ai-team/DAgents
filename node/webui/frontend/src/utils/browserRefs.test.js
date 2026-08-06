import { describe, expect, it } from "vitest";
import {
  attachBrowserRefsToAssistants,
  collectBrowserRefsFromEntries,
  parseBrowserToolContent,
} from "./browserRefs.js";

describe("browserRefs", () => {
  it("parses tool content JSON", () => {
    const parsed = parseBrowserToolContent(
      JSON.stringify({
        ok: true,
        extracted_content: "标题是 Example",
        detail: { task_id: "btask-1", status: "completed", summary: "标题是 Example", success: true },
      }),
    );
    expect(parsed.detail.task_id).toBe("btask-1");
    expect(parsed.extracted_content).toBe("标题是 Example");
  });

  it("collects refs between user and assistant slot", () => {
    const entries = [
      { kind: "user", text: "查一下" },
      {
        kind: "tool_result",
        id: 2,
        data: {
          tool_name: "browser_run_task",
          content: JSON.stringify({
            ok: true,
            detail: {
              task_id: "btask-9",
              status: "completed",
              summary: "找到了标题",
              success: true,
              action_names: ["navigate", "done"],
              urls: ["https://example.com"],
              detail_md: "tasks/btask-9.md",
            },
          }),
        },
      },
      { kind: "assistant", text: "结果如下" },
    ];
    const refs = collectBrowserRefsFromEntries(entries, 2);
    expect(refs).toHaveLength(1);
    expect(refs[0].task_id).toBe("btask-9");
    expect(refs[0].summary).toBe("找到了标题");
    expect(refs[0].detail_md).toBe("tasks/btask-9.md");
  });

  it("attaches refs on hydrate assistants", () => {
    const entries = [
      { kind: "user", text: "u" },
      {
        kind: "tool_result",
        data: {
          tool_name: "browser_run_task",
          content: JSON.stringify({
            ok: true,
            detail: { task_id: "t1", status: "completed", summary: "ok", success: true },
          }),
        },
      },
      { kind: "assistant", text: "答" },
    ];
    attachBrowserRefsToAssistants(entries);
    expect(entries[2].browser_refs?.[0]?.task_id).toBe("t1");
  });
});
