import { beforeEach, describe, expect, it, vi } from "vitest";
import { initTheme, setTheme, themeStore, toggleTheme } from "./theme.js";

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

beforeEach(() => {
  document.documentElement.removeAttribute("data-theme");
  themeStore.mode = "dark";
  mockGetItem.mockReset();
  mockSetItem.mockReset();
  mockGetItem.mockReturnValue("");
});

describe("theme store", () => {
  it("initializes from saved theme", () => {
    mockGetItem.mockReturnValue("light");
    initTheme();
    expect(themeStore.mode).toBe("light");
    expect(document.documentElement.getAttribute("data-theme")).toBe("light");
  });

  it("toggles and persists", () => {
    setTheme("dark");
    toggleTheme();
    expect(themeStore.mode).toBe("light");
    expect(mockSetItem).toHaveBeenCalledWith("dagents_webui_theme", "light");
  });
});
