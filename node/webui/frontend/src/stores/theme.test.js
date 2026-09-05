import { beforeEach, describe, expect, it, vi } from "vitest";
import { cycleTheme, initTheme, setTheme, themeStore } from "./theme.js";

const mockGetItem = vi.fn(() => "");
const mockSetItem = vi.fn();

vi.stubGlobal("localStorage", {
  getItem: mockGetItem,
  setItem: mockSetItem,
});

const docState = {};
vi.stubGlobal("document", {
  documentElement: {
    setAttribute: vi.fn((k, v) => {
      docState[k] = v;
    }),
    getAttribute: vi.fn((k) => docState[k] ?? null),
    removeAttribute: vi.fn((k) => {
      delete docState[k];
    }),
  },
});

vi.stubGlobal("window", {
  matchMedia: vi.fn(() => ({
    matches: true,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    addListener: vi.fn(),
    removeListener: vi.fn(),
  })),
});

beforeEach(() => {
  document.documentElement.removeAttribute("data-theme");
  themeStore.mode = "system";
  themeStore.resolved = "dark";
  mockGetItem.mockReset();
  mockSetItem.mockReset();
  mockGetItem.mockReturnValue("");
  window.matchMedia.mockReturnValue({
    matches: true,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    addListener: vi.fn(),
    removeListener: vi.fn(),
  });
});

describe("theme store", () => {
  it("initializes from saved theme", () => {
    mockGetItem.mockReturnValue("light");
    initTheme();
    expect(themeStore.mode).toBe("light");
    expect(themeStore.resolved).toBe("light");
    expect(document.documentElement.getAttribute("data-theme")).toBe("light");
  });

  it("defaults to system and resolves via prefers-color-scheme", () => {
    mockGetItem.mockReturnValue("");
    initTheme();
    expect(themeStore.mode).toBe("system");
    expect(themeStore.resolved).toBe("dark");
    expect(document.documentElement.getAttribute("data-theme")).toBe("dark");
  });

  it("cycles system → light → dark → system", () => {
    setTheme("system");
    cycleTheme();
    expect(themeStore.mode).toBe("light");
    cycleTheme();
    expect(themeStore.mode).toBe("dark");
    cycleTheme();
    expect(themeStore.mode).toBe("system");
  });
});
