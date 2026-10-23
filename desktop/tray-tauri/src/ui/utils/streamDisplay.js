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
  // 某些模型在生成 tool_call 时会先留下一个没有正文的 reasoning
  // 流式条目。它只是“思考中”的占位符，不应把同一批工具拆成两个气泡。
  let deferredEmptyThinking = [];

  function isEmptyStreamingThinking(item) {
    if (item?.kind !== "reasoning" && item?.kind !== "assistant") return false;
    if (!item?.entry?.streaming) return false;
    return !String(item.entry.text || "").trim();
  }

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

  function flushDeferredThinking() {
    if (!deferredEmptyThinking.length) return;
    output.push(...deferredEmptyThinking);
    deferredEmptyThinking = [];
  }

  function nextMeaningfulItem(index) {
    for (let nextIndex = index + 1; nextIndex < items.length; nextIndex += 1) {
      if (!isEmptyStreamingThinking(items[nextIndex])) return items[nextIndex];
    }
    return null;
  }

  for (let index = 0; index < items.length; index += 1) {
    const item = items[index];
    if (item?.kind === "tool_step") {
      // 当前工具意味着前一个空 reasoning 占位符确实是工具生成态，
      // 直接吸收到这次工具合并中。
      deferredEmptyThinking = [];
      toolRun.push(item);
      continue;
    }

    // 只有在工具步骤之后才延迟这个空占位符；如果后面紧接着仍是工具，
    // 让工具保持在同一个合并气泡中，否则恢复原有消息顺序。
    if (
      isEmptyStreamingThinking(item) &&
      (toolRun.length || nextMeaningfulItem(index)?.kind === "tool_step")
    ) {
      deferredEmptyThinking.push(item);
      continue;
    }

    flushToolRun();
    flushDeferredThinking();
    output.push(item);
  }
  flushToolRun();
  flushDeferredThinking();
  return output;
}
