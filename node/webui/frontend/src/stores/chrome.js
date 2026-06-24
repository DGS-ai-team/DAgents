import { reactive } from "vue";
import { formatInputStripUsage } from "./transcript.js";

export const chromeStore = reactive({
  sseStatus: "disconnected",
  agentInfo: null,
  llmSettings: null,
  usageStrip: null,
  contextTokens: -1,
  childSummary: "",
  hitlQueueLen: 0,
  panel: null,
  panelData: null,
});

export function setUsageFromSSE(data) {
  const round = {
    prompt_tokens: data.round_prompt_tokens,
    completion_tokens: data.round_completion_tokens,
    prompt_cache_hit_tokens: data.round_prompt_cache_hit_tokens,
    prompt_cached_tokens: data.round_prompt_cached_tokens,
    prompt_cache_hit_rate: data.round_prompt_cache_hit_rate,
    reasoning_tokens: data.round_reasoning_tokens,
    completion_tokens_details: data.round_completion_tokens_details,
  };
  const snap = parseUsageFields(round) || parseUsageFields(data);
  if (!snap) return;
  if (!chromeStore.usageStrip) {
    chromeStore.usageStrip = { ...snap };
    return;
  }
  chromeStore.usageStrip.prompt += snap.prompt;
  chromeStore.usageStrip.completion += snap.completion;
  chromeStore.usageStrip.hit += snap.hit;
  chromeStore.usageStrip.reasoning += snap.reasoning;
  if (chromeStore.usageStrip.prompt > 0 && chromeStore.usageStrip.hit > 0) {
    chromeStore.usageStrip.rate = Math.min(1, chromeStore.usageStrip.hit / chromeStore.usageStrip.prompt);
  }
}

function parseUsageFields(data) {
  const prompt = intVal(data.prompt_tokens);
  const completion = intVal(data.completion_tokens);
  if (prompt <= 0 && completion <= 0) return null;
  let hit = intVal(data.prompt_cache_hit_tokens);
  const cached = intVal(data.prompt_cached_tokens);
  if (hit <= 0 && cached > 0) hit = cached;
  let rate = -1;
  if (typeof data.prompt_cache_hit_rate === "number" && data.prompt_cache_hit_rate >= 0) rate = data.prompt_cache_hit_rate;
  else if (prompt > 0 && hit > 0) rate = Math.min(1, hit / prompt);
  let reasoning = intVal(data.reasoning_tokens);
  if (reasoning <= 0 && data.completion_tokens_details) reasoning = intVal(data.completion_tokens_details.reasoning_tokens);
  return { prompt, completion, hit, rate, reasoning };
}

function intVal(v) {
  const n = Number(v);
  return Number.isFinite(n) && n > 0 ? Math.floor(n) : 0;
}

export function formatThinkingSummary(llm) {
  if (!llm?.thinking_supported) return "";
  const thinking = String(llm.thinking || "").trim().toLowerCase();
  if (["disabled", "off"].includes(thinking)) return "off";
  if (["enabled", "on"].includes(thinking) || thinking) {
    const effort = String(llm.reasoning_effort || "high").trim() || "high";
    return `on · ${effort}`;
  }
  return "";
}

export function inputStripRight() {
  const parts = [];
  const thinking = formatThinkingSummary(chromeStore.llmSettings);
  if (thinking) parts.push(`thinking ${thinking}`);
  const usage = formatInputStripUsage(chromeStore.usageStrip);
  if (usage) parts.push(usage);
  else if (chromeStore.contextTokens >= 0) parts.push(`ctx ${chromeStore.contextTokens.toLocaleString("en-US")}`);
  return parts.join(" · ");
}

export function resetUsageStrip() {
  chromeStore.usageStrip = null;
}
