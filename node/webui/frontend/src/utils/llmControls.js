export function getThinkingControl(settings) {
  const explicit = String(settings?.thinking_control || "").trim().toLowerCase();
  if (explicit) return explicit;
  // Compatibility with nodes that predate provider/model capability metadata.
  return settings?.reasoning_effort_supported === false ? "toggle" : "effort";
}

export function canToggleThinking(settings) {
  return ["effort", "budget", "toggle"].includes(getThinkingControl(settings));
}

export function hasThinkingSecondaryControl(settings) {
  const control = getThinkingControl(settings);
  return ["effort", "budget"].includes(control) && settings?.reasoning_effort_supported !== false;
}
