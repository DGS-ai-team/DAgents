import { reactive } from "vue";

/** Shared clock for elapsed labels; it carries no conversation or turn state. */
export const liveClock = reactive({ tick: Date.now() });

let subscribers = 0;
let timer = null;

export function acquireLiveClock() {
  subscribers += 1;
  if (!timer) {
    timer = setInterval(() => {
      liveClock.tick = Date.now();
    }, 500);
  }

  let released = false;
  return () => {
    if (released) return;
    released = true;
    subscribers = Math.max(0, subscribers - 1);
    if (subscribers === 0 && timer) {
      clearInterval(timer);
      timer = null;
    }
  };
}
