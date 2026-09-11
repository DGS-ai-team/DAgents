import { reactive } from "vue";
import { formatInputStripUsage, parseUsageFields } from "../utils/usage.js";

export const chromeStore = reactive({
  sseStatus: "disconnected",
  agentInfo: null,
  llmSettings: null,
  usageStrip: null,
  contextTokens: -1,
  panel: null,
});

export function setUsageFromSSE(data) {
	// 顶层字段是 turn 累计快照，不能再次累加。取消路径会补发一次
	// 同样的累计快照，使用 replace 才不会把末次用量算两遍。
	const cumulative = parseUsageFields(data);
	if (cumulative) {
		chromeStore.usageStrip = { ...cumulative };
	}
}

export function inputStripRight() {
  // 状态栏右侧：token 用量 → 思考开关 → ContextMeter。
  return formatInputStripUsage(chromeStore.usageStrip);
}

export function resetUsageStrip() {
  chromeStore.usageStrip = null;
}
