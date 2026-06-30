const ASYNC_JOB_PREFIX = "async-job-";

function contentStr(content) {
  if (content == null) return "";
  return typeof content === "string" ? content : String(content);
}

function parseKvLine(content, key) {
  const prefix = `${key}=`;
  for (const line of content.split("\n")) {
    const stripped = line.trim();
    if (stripped.startsWith(prefix)) {
      return stripped.slice(prefix.length).trim();
    }
  }
  return "";
}

export function buildToolCallMap(messages) {
  const map = new Map();
  for (const msg of messages) {
    const raw = msg.raw || msg;
    if (raw.role !== "assistant" || !Array.isArray(raw.tool_calls)) continue;
    for (const tc of raw.tool_calls) {
      const id = tc?.id;
      if (!id) continue;
      const fn = tc.function || {};
      map.set(id, {
        name: fn.name || tc.name || "",
        arguments: fn.arguments ?? "",
      });
    }
  }
  return map;
}

export function resolveToolName(raw, priorMessages, toolCallMap) {
  const name = String(raw.name || "").trim();
  if (name) return name;

  const callId = String(raw.tool_call_id || "").trim();
  const content = contentStr(raw.content);

  if (callId.startsWith(ASYNC_JOB_PREFIX)) {
    const asyncName = parseKvLine(content, "tool_name");
    if (asyncName) return asyncName;
    const srcId = parseKvLine(content, "source_tool_call_id");
    if (srcId && toolCallMap.has(srcId)) {
      const matched = toolCallMap.get(srcId).name;
      if (matched) return matched;
    }
    const jobId = callId.slice(ASYNC_JOB_PREFIX.length).trim();
    if (jobId) {
      for (let i = priorMessages.length - 1; i >= 0; i -= 1) {
        const prev = priorMessages[i].raw || priorMessages[i];
        if (prev.role !== "tool") continue;
        const prevContent = contentStr(prev.content);
        if (!prevContent.includes(jobId)) continue;
        const prevCall = String(prev.tool_call_id || "").trim();
        if (!prevCall || !toolCallMap.has(prevCall)) continue;
        const matched = toolCallMap.get(prevCall).name;
        if (matched) return matched;
      }
    }
  }

  if (callId && toolCallMap.has(callId)) {
    const matched = toolCallMap.get(callId).name;
    if (matched) return matched;
  }

  return null;
}

export function filterLinkedToolMessages(messages) {
  const raws = messages.map((m) => m.raw || m);
  const toolCallMap = buildToolCallMap(messages);
  return messages.filter((msg, index) => {
    if (msg.role !== "tool") return true;
    return resolveToolName(msg.raw || {}, raws.slice(0, index), toolCallMap);
  });
}
