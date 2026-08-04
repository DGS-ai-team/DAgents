import { reactive } from "vue";

const THEME_KEY = "dagents_webui_theme";
const THEMES = new Set(["dark", "light", "system"]);

export const themeStore = reactive({
  /** User preference: dark | light | system */
  mode: "system",
  /** Resolved appearance applied to document */
  resolved: "dark",
});

let mediaQuery = null;
let mediaListener = null;

function sanitizeTheme(mode) {
  const m = String(mode || "").toLowerCase();
  return THEMES.has(m) ? m : "system";
}

function systemPrefersDark() {
  if (typeof window !== "undefined" && window.matchMedia) {
    return window.matchMedia("(prefers-color-scheme: dark)").matches;
  }
  return true;
}

function resolveAppearance(mode) {
  const m = sanitizeTheme(mode);
  if (m === "system") {
    return systemPrefersDark() ? "dark" : "light";
  }
  return m;
}

function readPersistedTheme() {
  try {
    const saved = localStorage.getItem(THEME_KEY);
    if (saved) return sanitizeTheme(saved);
  } catch {
    // ignore
  }
  return "system";
}

function detachSystemListener() {
  if (mediaQuery && mediaListener) {
    if (mediaQuery.removeEventListener) {
      mediaQuery.removeEventListener("change", mediaListener);
    } else if (mediaQuery.removeListener) {
      mediaQuery.removeListener(mediaListener);
    }
  }
  mediaQuery = null;
  mediaListener = null;
}

function attachSystemListener() {
  detachSystemListener();
  if (typeof window === "undefined" || !window.matchMedia) return;
  mediaQuery = window.matchMedia("(prefers-color-scheme: dark)");
  mediaListener = () => {
    if (themeStore.mode === "system") {
      applyResolved(resolveAppearance("system"));
    }
  };
  if (mediaQuery.addEventListener) {
    mediaQuery.addEventListener("change", mediaListener);
  } else if (mediaQuery.addListener) {
    mediaQuery.addListener(mediaListener);
  }
}

function applyResolved(appearance) {
  const next = appearance === "light" ? "light" : "dark";
  themeStore.resolved = next;
  if (typeof document !== "undefined") {
    document.documentElement.setAttribute("data-theme", next);
    document.documentElement.style.colorScheme = next;
  }
}

function applyTheme(mode) {
  const next = sanitizeTheme(mode);
  themeStore.mode = next;
  applyResolved(resolveAppearance(next));
  try {
    localStorage.setItem(THEME_KEY, next);
  } catch {
    // ignore
  }
  if (next === "system") {
    attachSystemListener();
  } else {
    detachSystemListener();
  }
}

export function initTheme() {
  applyTheme(readPersistedTheme());
}

export function setTheme(mode) {
  applyTheme(mode);
}

export function toggleTheme() {
  if (themeStore.mode === "system") {
    applyTheme(systemPrefersDark() ? "light" : "dark");
    return;
  }
  applyTheme(themeStore.mode === "dark" ? "light" : "dark");
}
