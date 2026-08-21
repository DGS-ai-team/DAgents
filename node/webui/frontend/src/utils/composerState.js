/**
 * HITL user-information answers are submitted through the normal composer,
 * even while the interrupted Turn is still marked as sending.
 */
export function hasPendingUserInformation(hitlQueue) {
  return Array.isArray(hitlQueue) && hitlQueue[0]?.kind === "user_information";
}

export function canSubmitComposer({
  disabled = false,
  cancelling = false,
  sending = false,
  hitlBusy = false,
  hasUserInformation = false,
  hasContent = false,
} = {}) {
  if (disabled || cancelling || hitlBusy || !hasContent) return false;
  return !sending || hasUserInformation;
}

export function shouldShowCancel({ sending = false, hitlBusy = false, hasUserInformation = false } = {}) {
  return !!sending && !hitlBusy && !hasUserInformation;
}
