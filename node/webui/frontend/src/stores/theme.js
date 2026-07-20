import { reactive } from "vue";

const THEME_KEY = "dagents_webui_theme";
const THEMES = new Set(["dark", "light"]);

export const themeStore = reactive({
  mode: "dark",
});

function sanitizeTheme(mode) {
  const m = String(mode || "").toLowerCase();
  return THEMES.has(m) ? m : "dark";
}

function readPersistedTheme() {
  try {
    const saved = localStorage.getItem(THEME_KEY);
    if (saved) return sanitizeTheme(saved);
  } catch {
    // ignore
  }
  if (typeof window !== "undefined" && window.matchMedia) {
    return window.matchMedia("(prefers-color-scheme: light)").matches ? "light" : "dark";
  }
  return "dark";
}

function applyTheme(mode) {
  const next = sanitizeTheme(mode);
  themeStore.mode = next;
  if (typeof document !== "undefined") {
    document.documentElement.setAttribute("data-theme", next);
  }
  try {
    localStorage.setItem(THEME_KEY, next);
  } catch {
    // ignore
  }
}

export function initTheme() {
  applyTheme(readPersistedTheme());
}

export function setTheme(mode) {
  applyTheme(mode);
}

export function toggleTheme() {
  applyTheme(themeStore.mode === "dark" ? "light" : "dark");
}
