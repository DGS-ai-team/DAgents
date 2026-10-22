const nativeDialogs = () => globalThis.__DAGENTS_NATIVE_DIALOGS__;
let dialogInFlight = false;

export function confirmDesktopAction(message, options = {}) {
  if (dialogInFlight) return Promise.resolve(false);
  dialogInFlight = true;
  const text = String(message ?? "");
  const bridge = nativeDialogs();
  return Promise.resolve().then(async () => {
    if (typeof bridge?.confirm === "function") return Boolean(await bridge.confirm(text, options));
    return typeof window !== "undefined" && typeof window.confirm === "function"
      ? window.confirm(text)
      : false;
  }).finally(() => { dialogInFlight = false; });
}

export function showDesktopMessage(message, options = {}) {
  if (dialogInFlight) return Promise.resolve();
  dialogInFlight = true;
  const text = String(message ?? "");
  const bridge = nativeDialogs();
  return Promise.resolve().then(async () => {
    if (typeof bridge?.message === "function") {
      await bridge.message(text, options);
      return;
    }
    if (typeof window !== "undefined" && typeof window.alert === "function") window.alert(text);
  }).finally(() => { dialogInFlight = false; });
}
