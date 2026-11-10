/** @vitest-environment node */
import { readdirSync, readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import postcss from "postcss";
import selectorParser from "postcss-selector-parser";
import { describe, expect, it } from "vitest";

const stylesDir = dirname(fileURLToPath(import.meta.url));
const read = (path) => readFileSync(resolve(stylesDir, path), "utf8");
const isSharedControlClass = (name) =>
  /^(?:btn(?:--[\w-]+)?|desktop-icon-button(?:--[\w-]+)?|badge(?:--[\w-]+)?|settings-field__(?:label(?:--error)?|input|hint|error)|settings-switch-input|form-checkbox|ui-select(?:__[\w-]+|--[\w-]+)?|ui-context-menu(?:__[\w-]+)?|ui-dialog(?:__[\w-]+)?|ui-dialog-backdrop|dialog-scrim|panel-overlay|ui-popover|panel-overlay__card)$/.test(name);

function selectorTargetsSharedControl(selector) {
  let targetsSharedControl = false;
  selectorParser((selectorRoot) => {
    selectorRoot.walkClasses((classNode) => {
      if (!isSharedControlClass(classNode.value)) return;
      for (let ancestor = classNode.parent?.parent; ancestor; ancestor = ancestor.parent) {
        if (ancestor.type === "pseudo" && ancestor.value === ":not") return;
      }
      if (classNode.next()?.type === "combinator") return;
      targetsSharedControl = true;
    });
  }).processSync(selector);
  return targetsSharedControl;
}

function walkFiles(directory, extensions) {
  return readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
    const path = resolve(directory, entry.name);
    if (entry.isDirectory()) return walkFiles(path, extensions);
    return extensions.some((extension) => entry.name.endsWith(extension)) ? [path] : [];
  });
}

describe("Desktop shared control styles ownership", () => {
  it("only treats positive selectors on the control itself as shared ownership", () => {
    expect(selectorTargetsSharedControl("button:not(.btn):not(.desktop-icon-button)")).toBe(false);
    expect(selectorTargetsSharedControl(".first-run :deep(.btn--ghost:hover:not(:disabled))")).toBe(true);
    expect(selectorTargetsSharedControl(".panel-overlay__card pre")).toBe(false);
    expect(selectorTargetsSharedControl(".settings-field__input[aria-invalid='true']")).toBe(true);
  });

  it("loads one ordered stylesheet entry after app components", () => {
    const main = read("../../main.js");
    const desktopApp = read("../DesktopApp.vue");
    const entry = read("index.css");
    const imports = [...entry.matchAll(/^@import\s+["']([^"']+)["'];/gm)].map((match) => match[1]);

    expect(main).toContain('@desktop-ui/styles/index.css');
    expect(main).not.toMatch(/@desktop-ui\/styles\/(?:tokens|layout|workbench|overrides|controls)\.css/);
    expect(desktopApp).not.toMatch(/import\s+['"][^'"]+\.css['"]/);
    expect(imports).toEqual([
      "./tokens.css",
      "./layout.css",
      "../desktop-ui.css",
      "./workbench.css",
      "./overrides.css",
      "./controls.css",
    ]);
    expect(new Set(imports).size).toBe(imports.length);
  });

  it("keeps shared visual base selectors in controls.css, not shell or select consumers", () => {
    const selectors = [
      ".btn",
      ".btn--primary",
      ".btn--danger",
      ".btn--ghost",
      ".btn--sm",
      ".desktop-icon-button",
      ".settings-field",
      ".settings-field__label",
      ".settings-field__label--error",
      ".settings-field__input",
      ".settings-field__hint",
      ".settings-field__error",
      ".settings-toggle",
      ".settings-switch-input",
      ".form-checkbox",
      ".badge",
      ".ui-select__trigger",
      ".ui-select__menu",
      ".ui-select__option",
      ".ui-context-menu",
      ".ui-dialog",
      ".ui-dialog-backdrop",
      ".dialog-scrim",
      ".panel-overlay",
      ".ui-popover",
      ".panel-overlay__card",
    ];
    const controlCss = read("controls.css");
    const controlRoot = postcss.parse(controlCss);
    const owned = new Set();
    controlRoot.walkRules((rule) => {
      for (const selector of rule.selectors || []) owned.add(selector.trim());
    });

    for (const selector of selectors) expect(owned.has(selector), `${selector} must be owned by controls.css`).toBe(true);
    expect(owned.has(".settings-field__input:disabled")).toBe(true);
    expect(owned.has(".desktop-icon-button:not(.btn):disabled")).toBe(true);
    let sharedBackdropRule = false;
    controlRoot.walkRules((rule) => {
      const ruleSelectors = rule.selectors || [];
      if ([".dialog-scrim", ".panel-overlay", ".ui-dialog-backdrop"].every((selector) => ruleSelectors.includes(selector))) {
        sharedBackdropRule = rule.nodes?.some((node) => node.type === "decl" && node.prop === "background" && node.value === "var(--surface-overlay)") || false;
      }
    });
    expect(sharedBackdropRule).toBe(true);

    const uiRoot = resolve(stylesDir, "..");
    const globalCss = [
      ...walkFiles(resolve(stylesDir), [".css"]).filter((path) => !path.endsWith("controls.css")),
      resolve(uiRoot, "desktop-ui.css"),
    ];
    const vueFiles = walkFiles(uiRoot, [".vue"]);
    const consumers = [
      ...globalCss.map((path) => [path, readFileSync(path, "utf8")]),
      ...vueFiles.flatMap((path) => {
        const source = readFileSync(path, "utf8");
        return [...source.matchAll(/<style\b[^>]*>([\s\S]*?)<\/style>/g)].map((match) => [path, match[1]]);
      }),
    ];
    const layoutProperties = new Set([
      "align-content", "align-items", "align-self", "box-sizing", "bottom", "display", "flex",
      "flex-basis", "flex-direction", "flex-flow", "flex-grow", "flex-shrink", "flex-wrap", "gap",
      "grid", "grid-area", "grid-auto-columns", "grid-auto-flow", "grid-auto-rows", "grid-column",
      "grid-column-end", "grid-column-start", "grid-row", "grid-row-end", "grid-row-start",
      "grid-template", "grid-template-areas", "grid-template-columns", "grid-template-rows", "height",
      "inset", "inset-block", "inset-block-end", "inset-block-start", "inset-inline", "inset-inline-end",
      "inset-inline-start", "justify-content", "justify-items", "justify-self", "left", "margin",
      "margin-block", "margin-block-end", "margin-block-start", "margin-bottom", "margin-inline",
      "margin-inline-end", "margin-inline-start", "margin-left", "margin-right", "margin-top", "max-height",
      "max-width", "min-height", "min-width", "order", "overflow", "overflow-x", "overflow-y", "place-content",
      "place-items", "place-self", "position", "right", "row-gap", "scroll-margin", "scroll-margin-bottom",
      "scroll-padding", "scroll-padding-bottom", "top", "vertical-align", "width", "column-gap", "object-fit",
      "z-index",
    ]);
    const violations = [];

    for (const [file, source] of consumers) {
      const root = postcss.parse(source, { from: file });
      root.walkRules((rule) => {
        for (const selector of rule.selectors || []) {
          if (!selectorTargetsSharedControl(selector)) continue;
          for (const declaration of rule.nodes || []) {
            if (declaration.type !== "decl") continue;
            const isOverlayPadding = declaration.prop.toLowerCase() === "padding"
              && /(?:^|[\s>+~])\.(?:panel-overlay|dialog-scrim)(?=$|[\s:.#\x5b])/.test(selector);
            if (!layoutProperties.has(declaration.prop.toLowerCase()) && !isOverlayPadding) {
              violations.push(`${file}: ${selector} sets non-layout ${declaration.prop}: ${declaration.value}`);
            }
          }
        }
      });
    }
    expect(violations).toEqual([]);
  });
});
