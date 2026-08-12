import { describe, expect, it } from "vitest";
import {
  PROTECTED_POLICY_TOOL,
  canSetPolicyMode,
  decisionToMode,
  entryMode,
  filterPolicyShellEntries,
  filterPolicyTools,
} from "./policyEditor.js";
import { formatPolicyMode } from "./panelFormat.js";

describe("policyEditor", () => {
  it("blocks deny for protected tool", () => {
    expect(canSetPolicyMode(PROTECTED_POLICY_TOOL, "deny")).toBe(false);
    expect(canSetPolicyMode(PROTECTED_POLICY_TOOL, "always")).toBe(true);
  });

  it("filters tools by name", () => {
    const out = filterPolicyTools(
      [{ name: "read_file" }, { name: "bash_run" }],
      "read",
    );
    expect(out).toHaveLength(1);
    expect(out[0].name).toBe("read_file");
  });

  it("filters shell commands by name", () => {
    const rows = [
      { command: "ls", mode: "never" },
      { command: "git", mode: "rule" },
    ];
    expect(filterPolicyShellEntries(rows, "ls")).toHaveLength(1);
    expect(filterPolicyShellEntries(rows, "")).toHaveLength(2);
  });

  it("prefers mode over legacy decision", () => {
    expect(entryMode({ mode: "rule", decision: "require_approval" })).toBe("rule");
    expect(decisionToMode("allow_auto")).toBe("never");
  });
});

describe("formatPolicyMode", () => {
  it("labels four modes correctly", () => {
    expect(formatPolicyMode("never")).toBe("自动允许");
    expect(formatPolicyMode("always")).toBe("需审批");
    expect(formatPolicyMode("rule")).toBe("特殊规则");
    expect(formatPolicyMode("deny")).toBe("禁止");
  });
});
