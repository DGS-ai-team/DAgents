/** 权威 Turn 仍在生成且长时间无 SSE 活动时触发 hydrate 对账。 */

export const STUCK_STATUS_MS = 30_000;
export const WATCH_INTERVAL_MS = 5_000;

/**
 * @param {object} opts
 * @param {() => boolean} opts.isAwaiting
 * @param {() => boolean} opts.hasStuckStatus  模型生成状态仍在展示
 * @param {() => (void|Promise<void>)} opts.onStuck
 * @param {number} [opts.stuckMs]
 * @param {number} [opts.intervalMs]
 */
export function createTurnWatchdog({
  isAwaiting,
  hasStuckStatus,
  onStuck,
  stuckMs = STUCK_STATUS_MS,
  intervalMs = WATCH_INTERVAL_MS,
}) {
  let lastActivityAt = Date.now();
  let timer = null;
  let inFlight = false;

  return {
    noteActivity() {
      lastActivityAt = Date.now();
    },
    start() {
      if (timer) return;
      lastActivityAt = Date.now();
      timer = setInterval(() => {
        if (inFlight) return;
        if (!isAwaiting?.()) return;
        if (!hasStuckStatus?.()) return;
        if (Date.now() - lastActivityAt < stuckMs) return;
        inFlight = true;
        lastActivityAt = Date.now();
        Promise.resolve()
          .then(() => onStuck?.())
          .catch(() => {})
          .finally(() => {
            inFlight = false;
          });
      }, intervalMs);
    },
    stop() {
      if (timer) {
        clearInterval(timer);
        timer = null;
      }
      inFlight = false;
    },
  };
}
