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
	// 顶层字段是 turn 累计快照，不能再次累加。取消路径会补发一次
	// 同样的累计快照，使用 replace 才不会把末次用量算两遍。
	const cumulative = parseUsageFields(data);
	if (cumulative) {
		chromeStore.usageStrip = { ...cumulative };
		return;
	}
	// 兼容旧服务端仅发送 round_* 的事件；旧格式确实是增量，只在没有
	// 顶层累计字段时累加。
	const round = parseUsageRound(data);
	if (!round) return;
	if (!chromeStore.usageStrip) {
		chromeStore.usageStrip = { ...round };
		return;
	}
	chromeStore.usageStrip.prompt += round.prompt;
	chromeStore.usageStrip.completion += round.completion;
	chromeStore.usageStrip.hit += round.hit;
	chromeStore.usageStrip.reasoning += round.reasoning;
	chromeStore.usageStrip.cacheObserved = chromeStore.usageStrip.cacheObserved || round.cacheObserved;
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
