/** 解析 SSE / API usage 字段为统一快照。 */
export function parseUsageFields(data) {
  if (!data || typeof data !== "object") return null;
  const prompt = intVal(data.prompt_tokens);
  const completion = intVal(data.completion_tokens);
  if (prompt <= 0 && completion <= 0) return null;
  let hit = intVal(data.prompt_cache_hit_tokens);
  const cached = intVal(data.prompt_cached_tokens);
  if (hit <= 0 && cached > 0) hit = cached;
  let rate = -1;
  if (typeof data.prompt_cache_hit_rate === "number" && data.prompt_cache_hit_rate >= 0) {
    rate = data.prompt_cache_hit_rate;
  } else if (prompt > 0 && hit > 0) {
    rate = Math.min(1, hit / prompt);
  }
  let reasoning = intVal(data.reasoning_tokens);
  if (reasoning <= 0 && data.completion_tokens_details) {
    reasoning = intVal(data.completion_tokens_details.reasoning_tokens);
  }
  return { prompt, completion, hit, rate, reasoning };
}
export function parseUsageRound(data) {
  if (!data || typeof data !== "object") return null;
  return parseUsageFields({
    prompt_tokens: data.round_prompt_tokens,
    completion_tokens: data.round_completion_tokens,
    prompt_cache_hit_tokens: data.round_prompt_cache_hit_tokens,
    prompt_cached_tokens: data.round_prompt_cached_tokens,
    prompt_cache_hit_rate: data.round_prompt_cache_hit_rate,
    reasoning_tokens: data.round_reasoning_tokens,
    completion_tokens_details: data.round_completion_tokens_details,
  });
}

function intVal(v) {
  const n = Number(v);
  return Number.isFinite(n) && n > 0 ? Math.floor(n) : 0;
}

export function formatCompactTokens(n) {
  if (n >= 10000) return `${(Math.round(n / 100) / 10).toFixed(1).replace(/\.0$/, "")}k`;
  return n.toLocaleString("en-US");
}

export function formatInputStripUsage(snap) {
  if (!snap) return "";
  let t = `↑${formatCompactTokens(snap.prompt)} ↓${formatCompactTokens(snap.completion)}`;
  if (snap.hit > 0) {
    t +=
      snap.rate >= 0
        ? ` · hit ${formatCompactTokens(snap.hit)} (${Math.round(snap.rate * 100)}%)`
        : ` · hit ${formatCompactTokens(snap.hit)}`;
  }
  if (snap.reasoning > 0) t += ` · think ${formatCompactTokens(snap.reasoning)}`;
  return t;
}
