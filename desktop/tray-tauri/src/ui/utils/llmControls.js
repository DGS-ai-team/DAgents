export function getThinkingControl(settings) {
  const explicit = String(settings?.thinking_control || "").trim().toLowerCase();
  return explicit || "effort";
}

export function canToggleThinking(settings) {
  return ["effort", "budget", "toggle"].includes(getThinkingControl(settings));
}

export function hasThinkingSecondaryControl(settings) {
  const control = getThinkingControl(settings);
  return ["effort", "budget"].includes(control);
}
