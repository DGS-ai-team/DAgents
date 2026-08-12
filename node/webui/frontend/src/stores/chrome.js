import { reactive } from "vue";
import { formatInputStripUsage, parseUsageFields, parseUsageRound } from "../utils/usage.js";

export const chromeStore = reactive({
  sseStatus: "disconnected",
  agentInfo: null,
  llmSettings: null,
  usageStrip: null,
  contextTokens: -1,
  panel: null,
});

export function setUsageFromSSE(data) {
  const snap = parseUsageRound(data) || parseUsageFields(data);
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

export function inputStripRight() {
  // 状态栏右侧：token 用量 → 思考开关 → ContextMeter。
  return formatInputStripUsage(chromeStore.usageStrip);
}

export function resetUsageStrip() {
  chromeStore.usageStrip = null;
}
