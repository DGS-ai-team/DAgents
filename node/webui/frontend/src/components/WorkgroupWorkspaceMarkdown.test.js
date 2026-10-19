import { describe, expect, it } from "vitest";
import workgroupSource from "../../../../../desktop/ui/components/WorkgroupWorkspace.vue?raw";

describe("WorkgroupWorkspace assistant Markdown", () => {
  it("uses the shared safe preview renderer for assistant content without guessing an Agent owner", () => {
    expect(workgroupSource).not.toContain("renderMarkdown(");
    expect(workgroupSource.match(/<ResourceMarkdown\b/g)).toHaveLength(4);
    expect(workgroupSource.match(/agent-id=""/g)).toHaveLength(4);
    expect(workgroupSource).toContain(":entry=\"{ kind: 'assistant', text: item.ev.text || '' }\"");
    expect(workgroupSource).toContain("item.ev.type !== 'human_message'");
  });
});
