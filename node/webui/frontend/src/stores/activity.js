import { reactive } from "vue";

/** Activity 面板刷新信号（/clear 等清空后 bump）。 */
export const activityStore = reactive({
  tick: 0,
});

export function bumpActivityRefresh() {
  activityStore.tick += 1;
}
