const STORAGE_KEY = "dagents:configuration-changed";
const EVENT_NAME = "dagents:configuration-changed";

export function notifyConfigurationChanged(kind = "general") {
  if (typeof window === "undefined") return;
  const detail = { kind: String(kind || "general"), at: Date.now() };
  window.dispatchEvent(new CustomEvent(EVENT_NAME, { detail }));
  try {
    window.localStorage.setItem(STORAGE_KEY, JSON.stringify(detail));
  } catch {
    // Storage can be unavailable in embedded/private contexts; the local event still works.
  }
}

export function onConfigurationChanged(handler) {
  if (typeof window === "undefined" || typeof handler !== "function") return () => {};
  const onEvent = (event) => handler(event?.detail || {});
  const onStorage = (event) => {
    if (event.key !== STORAGE_KEY || !event.newValue) return;
    try {
      handler(JSON.parse(event.newValue));
    } catch {
      handler({ kind: "general" });
    }
  };
  window.addEventListener(EVENT_NAME, onEvent);
  window.addEventListener("storage", onStorage);
  return () => {
    window.removeEventListener(EVENT_NAME, onEvent);
    window.removeEventListener("storage", onStorage);
  };
}
