import { reactive } from "vue";

/** 当前选中的 Agent；消息与回合状态由 hydrate 投影直接提供。 */
export const agentStore = reactive({ agentId: "" });
