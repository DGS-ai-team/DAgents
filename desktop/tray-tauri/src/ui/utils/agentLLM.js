/** Read the profile bound to an Agent snapshot, accepting API casing variants. */
export function agentActiveProfile(agent) {
  const snapshot = agent?.config_snapshot || agent?.ConfigSnapshot || {};
  const defaults = snapshot?.defaults || snapshot?.Defaults || {};
  const llm = defaults?.llm || defaults?.LLM || {};
  return String(llm?.active || llm?.active_profile || llm?.activeProfile || "").trim();
}
