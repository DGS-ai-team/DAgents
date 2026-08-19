/**
 * 将连续的工具步骤合并成一个展示分组。
 *
 * 只改变展示层结构，不改变 transcript 中的原始消息顺序，因而不会影响
 * tool_call/tool_result 的配对，也不会改变 assistant 正文与工具步骤之间的时序。
 */
export function groupConsecutiveToolSteps(items = []) {
  if (!Array.isArray(items) || !items.length) return [];

  const output = [];
  let toolRun = [];

  function flushToolRun() {
    if (!toolRun.length) return;
    if (toolRun.length === 1) {
      output.push(toolRun[0]);
    } else {
      output.push({
        key: `tool-group-${toolRun[0].key}`,
        kind: "tool_group",
        steps: toolRun,
      });
    }
    toolRun = [];
  }

  for (const item of items) {
    if (item?.kind === "tool_step") {
      toolRun.push(item);
      continue;
    }
    flushToolRun();
    output.push(item);
  }
  flushToolRun();
  return output;
}
