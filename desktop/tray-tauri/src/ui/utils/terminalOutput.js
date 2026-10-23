/* ANSI control sequences intentionally contain control characters. */
/* eslint-disable no-control-regex */
const TERMINAL_RESULT_TOOLS = new Set(["terminal_read", "terminal_terminate"]);

export function isTerminalResultTool(name) {
  return TERMINAL_RESULT_TOOLS.has(String(name || "").trim());
}

/**
 * Turn a PTY byte stream into readable text for message/tool-result views.
 * The live xterm panel deliberately does not use this function: it needs the
 * original control sequences to render colors, cursor movement and prompts.
 */
export function normalizeTerminalOutput(value) {
  let text = String(value ?? "");
  text = text
    .replace(/\u001b\][\s\S]*?(?:\u0007|\u001b\\)/g, "")
    .replace(/\u001b\[[0-?]*[ -/]*[@-~]/g, "")
    .replace(/\u001b(?:[@-Z\\-_])/g, "")
    .replace(/\r\n/g, "\n")
    .replace(/\r/g, "\n");

  return text
    .split("\n")
    .map((line) => {
      const output = [];
      for (const char of line) {
        if (char === "\b") {
          output.pop();
          continue;
        }
        const code = char.codePointAt(0);
        if (char === "\t" || (code >= 0x20 && code !== 0x7f)) output.push(char);
      }
      return output.join("");
    })
    .join("\n");
}

/** Sanitize only the output field while preserving terminal cursor metadata. */
export function normalizeTerminalResultContent(content) {
  const text = String(content ?? "");
  try {
    const payload = JSON.parse(text);
    if (!payload || typeof payload !== "object" || typeof payload.output !== "string") {
      return normalizeTerminalOutput(text);
    }
    return JSON.stringify({ ...payload, output: normalizeTerminalOutput(payload.output) });
  } catch {
    return normalizeTerminalOutput(text);
  }
}
