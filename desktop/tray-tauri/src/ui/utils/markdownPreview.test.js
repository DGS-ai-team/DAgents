/** @vitest-environment jsdom */
import { createHash } from "node:crypto";
import { runInNewContext } from "node:vm";
import { describe, expect, it } from "vitest";
import { createHtmlPreviewDocument, sanitizeHtmlPreview, sanitizeSvg } from "./markdownPreview.js";

describe("isolated markdown previews", () => {
  it("sanitizes HTML navigation, active content, and external resources", () => {
    const html = sanitizeHtmlPreview('<div onclick="run()"><a href="https://example.com">link</a><img src="https://example.com/p.png"><img src="data:image/png;base64,YQ=="><form action="https://example.com"><input></form><script>run()</script><style>@import url(https://example.com/x.css)</style></div>');
    expect(html).not.toContain("onclick");
    expect(html).not.toContain("href=");
    expect(html).not.toContain("https://example.com");
    expect(html).not.toContain("<form");
    expect(html).not.toContain("<script");
    expect(html).toContain("data:image/png;base64,YQ==");
    const document = createHtmlPreviewDocument("<style>html,body{height:100vh;max-height:100px;overflow:auto}</style><p>LONG_PREVIEW_END_VISIBLE_AFTER_INNER_SCROLL</p><img width='120' height='20' style='width:120px;height:20px' src='data:image/png;base64,YQ=='><script>alert('model script')</script><a onclick='run()'>x</a>", "0123456789abcdef0123456789abcdef");
    expect(document).toContain("default-src 'none'");
    expect(document).toContain('data-channel="0123456789abcdef0123456789abcdef"');
    expect(document).toContain("dagents-preview-height");
    expect(document).toContain("display:flow-root");
    expect(document).toContain("overflow:visible!important");
    expect(document.indexOf("overflow:auto")).toBeLessThan(document.indexOf("overflow:visible!important"));
    expect(document).toContain("overflow-wrap:anywhere!important");
    expect(document).toContain("word-break:break-word!important");
    expect(document).toContain("max-width:100%!important");
    expect(document).not.toContain("#dp-0123456789abcdef0123456789abcdef img");
    expect(document).toContain("LONG_PREVIEW_END_VISIBLE_AFTER_INNER_SCROLL");
    expect(document).toContain("connect-src 'none'");
    expect(document).toContain("form-action 'none'");
    expect(document).not.toContain("model script");
    expect(document).not.toContain("onclick");
    const parsed = new DOMParser().parseFromString(document, "text/html");
    const previewImage = parsed.querySelector("img");
    expect(previewImage.getAttribute("width")).toBe("120");
    expect(previewImage.getAttribute("height")).toBe("20");
    expect(previewImage.style.width).toBe("120px");
    expect(previewImage.style.height).toBe("20px");
    const scripts = [...parsed.querySelectorAll("script")];
    expect(scripts).toHaveLength(1);
    const scriptHash = `'sha256-${createHash("sha256").update(scripts[0].textContent).digest("base64")}'`;
    const csp = parsed.querySelector('meta[http-equiv="Content-Security-Policy"]').content;
    expect(csp.split(";").map((directive) => directive.trim())).toContain(`script-src ${scriptHash}`);
    expect(createHtmlPreviewDocument("<p>Preview</p>")).toBe("");
  });

  it("includes horizontal scrollbar height in measurements and removes it when the bar disappears", () => {
    const html = createHtmlPreviewDocument("<p>Long preview</p>", "0123456789abcdef0123456789abcdef");
    const parsed = new DOMParser().parseFromString(html, "text/html");
    const script = parsed.querySelector("script");
    let boxHeight = 744;
    const box = { getBoundingClientRect: () => ({ height: boxHeight }), get scrollHeight() { return boxHeight; } };
    const documentListeners = {};
    const windowListeners = {};
    const pendingFrames = [];
    const reports = [];
    let clientHeight = 784;
    let clientWidth = 380;
    const doc = {
      currentScript: script,
      documentElement: { get clientHeight() { return clientHeight; }, get clientWidth() { return clientWidth; } },
      getElementById: (id) => id === script.dataset.content ? box : null,
      addEventListener: (name, listener) => { (documentListeners[name] ||= []).push(listener); },
    };
    const win = {
      innerHeight: 800,
      addEventListener: (name, listener) => { (windowListeners[name] ||= []).push(listener); },
    };
    class ResizeObserverMock { constructor(callback) { this.callback = callback; } observe() {} }
    runInNewContext(script.textContent, {
      document: doc,
      window: win,
      parent: { postMessage: (message) => reports.push(message) },
      ResizeObserver: ResizeObserverMock,
      requestAnimationFrame: (callback) => { pendingFrames.push(callback); return pendingFrames.length; },
    });
    const flushFrame = () => pendingFrames.splice(0).forEach((callback) => callback());

    flushFrame();
    expect(reports.at(-1).height).toBe(760);

    clientHeight = 800;
    windowListeners.load[0]();
    flushFrame();
    expect(reports.at(-1).height).toBe(744);

    clientWidth = 364;
    windowListeners.load[0]();
    flushFrame();
    expect(reports).toHaveLength(2);
  });

  it("keeps a sanitized static SVG chart inside an HTML preview card", () => {
    const html = sanitizeHtmlPreview('<section><svg xmlns="http://www.w3.org/2000/svg"><defs><linearGradient id="g"><stop offset="0%" stop-color="#fff"/></linearGradient></defs><path d="M0 0L5 5" fill="url(#g)" onload="run()"/></svg></section>');
    expect(html).toContain("data:image/svg+xml;base64,");
    expect(html).not.toContain("<svg");
    expect(html).not.toContain("onload");
  });

  it("keeps local gradient and marker references while removing active SVG features", () => {
    const safe = sanitizeSvg('<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20" onload="run()"><defs><linearGradient id="paint" gradientTransform="translate(1 2)"><stop offset="0%" stop-color="#fff"/><stop offset="100%" stop-color="#000"/></linearGradient><marker id="arrow" markerWidth="6" markerHeight="6" refX="5" refY="3"><path d="M0 0 L4 2 L0 4" fill="#123abc"/></marker><clipPath id="clip"><circle r="8"/></clipPath></defs><path d="M1 1L19 19" fill="url(#paint)" stroke="#222" marker-end="url(#arrow)" clip-path="url(#clip)"/><foreignObject><script>run()</script></foreignObject><image href="https://example.com/a.png"/></svg>');
    expect(safe).toContain("linearGradient");
    expect(safe).toContain("url(#paint)");
    expect(safe).toContain("marker-end=\"url(#arrow)\"");
    expect(safe).toContain("clip-path=\"url(#clip)\"");
    expect(safe).toContain("fill=\"#123abc\"");
    expect(safe).toContain("stroke=\"#222\"");
    expect(safe).toContain("markerWidth=\"6\"");
    expect(safe).toContain("gradientTransform=\"translate(1 2)\"");
    expect(safe).not.toContain("onload");
    expect(safe).not.toContain("foreignObject");
    expect(safe).not.toContain("<script");
    expect(safe).not.toContain("https://");
    expect(sanitizeSvg('<svg><path fill="url(https://example.com/x)" d="M0 0"/></svg>')).not.toContain("url(https");
  });

  it("sanitizes SVG resource source without rejecting removable tags", () => {
    expect(sanitizeSvg('<svg xmlns="http://www.w3.org/2000/svg"><path d="M0 0"/><script>run()</script><image href="https://outside.test/a.png"/></svg>')).toContain("<path");
  });
});
