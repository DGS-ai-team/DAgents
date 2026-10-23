import { describe, expect, it } from "vitest";
import {
  normalizeWorkbenchMode,
  terminalInputText,
  terminalStatusLabel,
  terminalStatusReady,
  terminalTargetLabel,
} from "./terminalWorkbench.js";

describe("terminal workbench helpers", () => {
  it("normalizes the two input destinations", () => {
    expect(normalizeWorkbenchMode("agent")).toBe("agent");
    expect(normalizeWorkbenchMode("terminal")).toBe("terminal");
    expect(normalizeWorkbenchMode("anything-else")).toBe("terminal");
  });

  it("keeps target labels explicit about local and remote context", () => {
    expect(terminalTargetLabel({ target_kind: "local", shell: "powershell" })).toBe("本机 · powershell");
    expect(terminalTargetLabel({ target_kind: "local", target_id: "wsl", shell: "bash" })).toBe("本机 · WSL");
    expect(
      terminalTargetLabel({ target_kind: "linux_channel", shell: "bash", username: "dev", host: "staging" }),
    ).toBe("远程 Linux · dev@staging · bash");
  });

  it("maps lifecycle labels and preserves terminal input semantics", () => {
    expect(terminalStatusLabel("running")).toBe("运行中");
    expect(terminalStatusLabel("exited")).toBe("已退出");
    expect(terminalStatusLabel("unknown")).toBe("unknown");
    expect(terminalInputText("printf ok", { appendNewline: true })).toBe("printf ok\n");
    expect(terminalInputText("line 1\nline 2")).toBe("line 1\nline 2");
    expect(terminalInputText("", { appendNewline: true })).toBe("");
  });

  it("treats authoritative running sessions as input-ready", () => {
    expect(terminalStatusReady("running")).toBe(true);
    expect(terminalStatusReady("已连接")).toBe(true);
    expect(terminalStatusReady("reconnecting")).toBe(false);
    expect(terminalStatusReady("exited")).toBe(false);
  });
});
