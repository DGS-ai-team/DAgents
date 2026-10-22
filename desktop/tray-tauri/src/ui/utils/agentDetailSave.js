export async function saveAgentDetailStages({
  agentId,
  agentPayload,
  promptPayload,
  patchAgent,
  putPromptContext,
  reloadAgentRuntime,
}) {
  let updatedAgent;
  try {
    updatedAgent = await patchAgent(agentId, agentPayload);
  } catch (error) {
    return { ok: false, stage: "agent", error, promptSaved: false };
  }

  try {
    await putPromptContext(agentId, promptPayload);
  } catch (error) {
    return { ok: false, stage: "prompt", error, updatedAgent, promptSaved: false };
  }

  try {
    await reloadAgentRuntime(agentId);
  } catch (error) {
    return { ok: false, stage: "reload", error, updatedAgent, promptSaved: true };
  }

  return { ok: true, updatedAgent, promptSaved: true };
}
