/** 活动记录文案（压缩、子 Agent 生命周期等）。 */

function intVal(v) {
  const n = Number(v);
  return Number.isFinite(n) ? Math.floor(n) : 0;
}

export function formatChildLifecycle(type, data) {
  const id = String(data?.child_session_id || "").slice(0, 16);
  const purpose = String(data?.purpose || "").trim();
  if (type === "temporary_agent_created") return `临时 Agent 已创建 · ${purpose || id}`;
  if (type === "temporary_agent_cancelled") return `临时 Agent 已取消 · ${id}`;
  return `临时 Agent 已结束 · ${id} · ${data?.status || "completed"}`;
}

export function formatCompressionDetail(mode, data) {
  const status = String(data?.status || "done");
  const count = intVal(data?.compressed_message_count);
  const modeLabel = mode === "blocking" ? "阻塞" : "静默";
  if (status === "applied") {
    let line = `上下文已压缩（${modeLabel}）：合并 ${count} 条消息`;
    const prompt = data?.prompt_tokens;
    const completion = data?.completion_tokens;
    if (prompt != null && completion != null) {
      const rate =
        data?.token_reduction_rate != null
          ? `，token 减少 ${Math.round(Number(data.token_reduction_rate) * 100)}%`
          : "";
      line += `（${prompt} → ${completion}${rate}）`;
    }
    return line;
  }
  if (status === "failed") return `上下文压缩失败（${modeLabel}），保留原上下文`;
  if (status === "stale") return `上下文压缩已过期（${modeLabel}），已丢弃`;
  if (status === "invalid") return `上下文压缩无效（${modeLabel}），已丢弃`;
  return `上下文压缩结束（${modeLabel}）· ${status}`;
}

export function formatCompressionStart(mode, data) {
  const modeLabel = mode === "blocking" ? "阻塞" : "静默";
  const target = data?.compressed_message_count ?? "?";
  return `正在压缩上下文（${modeLabel}，目标 ${target} 条）`;
}
