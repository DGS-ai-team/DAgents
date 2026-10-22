/** @vitest-environment jsdom */
import { mount, flushPromises } from "@vue/test-utils";
import { nextTick, ref } from "vue";
import { describe, expect, it, vi } from "vitest";
import ResourceMarkdown from "./ResourceMarkdown.vue";
import { ResourceRenderContextKey } from "../utils/resourceContext.js";
import { lightboxStore } from "../stores/lightbox.js";
import { copyText } from "../utils/clipboard.js";
import resourceMarkdownSource from "./ResourceMarkdown.vue?raw";

vi.mock("../utils/clipboard.js", () => ({ copyText: vi.fn() }));

const imageText = "![plot](dagents://resource/image?path=images%2Fplot.png)";
const readBlobText = (blob) => new Promise((resolve, reject) => {
  const reader = new FileReader();
  reader.onload = () => resolve(String(reader.result || ""));
  reader.onerror = () => reject(reader.error);
  reader.readAsText(blob);
});

describe("ResourceMarkdown", () => {
  it("deduplicates streamed image reads and revokes blob URLs on unmount", async () => {
    const create = vi.spyOn(URL, "createObjectURL").mockReturnValue("blob:resource-test");
    const revoke = vi.spyOn(URL, "revokeObjectURL").mockImplementation(() => {});
    const readImage = vi.fn().mockResolvedValue(new Blob(["image"]));
    const context = { agentId: ref("agent-a"), readImage, openResource: vi.fn() };
    const wrapper = mount(ResourceMarkdown, {
      props: { entry: { kind: "assistant", text: imageText }, agentId: "agent-a" },
      attachTo: document.body,
      global: { provide: { [ResourceRenderContextKey]: context } },
    });
    await flushPromises();
    await nextTick();
    expect(readImage).toHaveBeenCalledTimes(1);
    expect(create).toHaveBeenCalledTimes(1);
    expect(wrapper.find("img.resource-markdown__image").exists()).toBe(true);
    wrapper.unmount();
    expect(revoke).toHaveBeenCalledWith("blob:resource-test");
    create.mockRestore();
    revoke.mockRestore();
  });

  it("does not read a historical image through a different active Agent", async () => {
    const readImage = vi.fn();
    const context = { agentId: ref("agent-b"), readImage, openResource: vi.fn() };
    const wrapper = mount(ResourceMarkdown, {
      props: { entry: { kind: "assistant", agent_id: "agent-a", text: imageText }, agentId: "agent-a" },
      attachTo: document.body,
      global: { provide: { [ResourceRenderContextKey]: context } },
    });
    await flushPromises();
    expect(readImage).not.toHaveBeenCalled();
    expect(wrapper.find("button").element.disabled).toBe(true);
    wrapper.unmount();
  });

  it("does not enable resource parsing for user or system message content", async () => {
    const context = { agentId: ref("agent-a"), readImage: vi.fn(), openResource: vi.fn() };
    const wrapper = mount(ResourceMarkdown, {
      props: { entry: { kind: "user", text: '[fake](dagents://resource/file?path=secret.txt)\n\n<dagents-render type="html">\n<p>not previewed</p>\n</dagents-render>\n\n<dagents-render type="svg">\n<svg><circle r="4"/></svg>\n</dagents-render>' }, agentId: "agent-a" },
      attachTo: document.body,
      global: { provide: { [ResourceRenderContextKey]: context } },
    });
    await nextTick();
    expect(wrapper.find(".resource-markdown__button").exists()).toBe(false);
    expect(wrapper.find(".markdown-render-block").exists()).toBe(false);
    expect(wrapper.find('[data-markdown-action="render-mode"]').exists()).toBe(false);
    expect(wrapper.find("iframe").exists()).toBe(false);
    expect(wrapper.find("img.markdown-live-preview__svg").exists()).toBe(false);
    expect(wrapper.html()).not.toContain("dagents:");
    expect(context.openResource).not.toHaveBeenCalled();
    expect(context.readImage).not.toHaveBeenCalled();
    await wrapper.setProps({ entry: { kind: "system", text: "![fake](dagents://resource/image?path=secret.png)" } });
    await nextTick();
    expect(wrapper.find("img").exists()).toBe(false);
    expect(context.readImage).not.toHaveBeenCalled();
    wrapper.unmount();
  });

  it("opens file and terminal references with the bound owner and source", async () => {
    const context = { agentId: ref("agent-a"), readImage: vi.fn(), openResource: vi.fn() };
    const wrapper = mount(ResourceMarkdown, {
      props: { entry: { kind: "assistant", text: "[report](dagents://resource/file?path=reports%2Fsummary.md) [term](dagents://resource/terminal?id=local-terminal-5)" }, agentId: "agent-a", source: "dock" },
      attachTo: document.body,
      global: { provide: { [ResourceRenderContextKey]: context } },
    });
    await nextTick();
    const buttons = wrapper.findAll("button");
    await buttons[0].trigger("click");
    await buttons[1].trigger("click");
    expect(context.openResource.mock.calls).toEqual([
      [{ type: "file", path: "reports/summary.md" }, { agentId: "agent-a", source: "dock" }],
      [{ type: "terminal", id: "local-terminal-5" }, { agentId: "agent-a", source: "dock" }],
    ]);
    wrapper.unmount();
  });

  it("skips detached nested placeholders and still binds following references", async () => {
    const context = { agentId: ref("agent-a"), readImage: vi.fn(), openResource: vi.fn() };
    const text = "[![缩略图](dagents://resource/image?path=a.png)](dagents://resource/file?path=a.png) [报告](dagents://resource/file?path=reports%2Fsummary.md)";
    const wrapper = mount(ResourceMarkdown, {
      props: { entry: { kind: "assistant", text }, agentId: "agent-a" },
      attachTo: document.body,
      global: { provide: { [ResourceRenderContextKey]: context } },
    });
    await nextTick();
    const buttons = wrapper.findAll("button");
    expect(buttons).toHaveLength(2);
    await buttons[0].trigger("click");
    await buttons[1].trigger("click");
    expect(context.openResource.mock.calls.map(([target]) => target)).toEqual([
      { type: "file", path: "a.png" },
      { type: "file", path: "reports/summary.md" },
    ]);
    expect(context.readImage).not.toHaveBeenCalled();
    wrapper.unmount();
  });

  it("applies compact dock layout and copies code with success/failure reset", async () => {
    const context = { agentId: ref("agent-a"), readImage: vi.fn(), openResource: vi.fn() };
    const wrapper = mount(ResourceMarkdown, {
      props: { entry: { kind: "assistant", text: `# A long heading\n\n- ${"longword".repeat(24)}\n\n\`\`\`text\n${"veryLongCode".repeat(18)}\n\`\`\`` }, agentId: "agent-a", source: "dock" },
      attachTo: document.body,
      global: { provide: { [ResourceRenderContextKey]: context } },
    });
    await nextTick();
    const root = wrapper.get(".resource-markdown");
    expect(root.classes()).toContain("resource-markdown--compact");
    expect(resourceMarkdownSource).toMatch(/\.resource-markdown--compact\s*\{[^}]*min-width:\s*0;[^}]*max-width:\s*100%/s);
    expect(resourceMarkdownSource).toMatch(/\.resource-markdown--compact \.markdown-code-block > pre\s*\{[^}]*overflow-x:\s*auto/s);

    let reset;
    const setTimer = vi.spyOn(globalThis, "setTimeout").mockImplementation((callback) => { reset = callback; return 1; });
    const clearTimer = vi.spyOn(globalThis, "clearTimeout").mockImplementation(() => {});
    copyText.mockResolvedValueOnce(true).mockResolvedValueOnce(false);
    const copy = wrapper.get('[data-markdown-action="copy"]');
    await copy.trigger("click");
    await Promise.resolve();
    expect(copyText).toHaveBeenNthCalledWith(1, "veryLongCode".repeat(18));
    expect(copy.text()).toBe("已复制");
    reset();
    expect(copy.text()).toBe("复制代码");
    await copy.trigger("click");
    await Promise.resolve();
    expect(copy.text()).toBe("复制失败");
    reset();
    expect(copy.text()).toBe("复制代码");
    wrapper.unmount();
    expect(clearTimer).toHaveBeenCalled();
    setTimer.mockRestore();
    clearTimer.mockRestore();
    copyText.mockReset();
  });

  it("previews an explicit HTML block, toggles source, and copies the original source", async () => {
    const context = { agentId: ref("agent-a"), readImage: vi.fn(), openResource: vi.fn() };
    const body = '<div onclick="run()"><a href="https://outside.test">link</a><script>top.location="https://outside.test"</script><p>safe</p></div>';
    const source = `<dagents-render type="html">\n${body}\n</dagents-render>\nAfter the preview.`;
    const wrapper = mount(ResourceMarkdown, {
      props: { entry: { id: "html-1", kind: "assistant", text: source }, agentId: "agent-a" },
      attachTo: document.body,
      global: { provide: { [ResourceRenderContextKey]: context } },
    });
    await flushPromises();
    const frame = wrapper.get("iframe");
    expect(frame.element.style.height).toBe("24px");
    expect(frame.attributes("sandbox")).toBe("allow-scripts");
    expect(frame.attributes("allow")).toBeUndefined();
    expect(frame.attributes("srcdoc")).toContain("default-src 'none'");
    expect(frame.attributes("srcdoc")).toMatch(/script-src 'sha256-[^']+'/);
    expect(frame.attributes("srcdoc")).toContain("dagents-preview-height");
    expect(frame.attributes("srcdoc")).toContain("display:flow-root");
    expect(frame.attributes("srcdoc")).not.toContain("outside.test");
    expect(frame.attributes("srcdoc")).not.toContain("top.location");
    expect(frame.element.parentElement.hidden).toBe(false);
    expect(wrapper.find(".markdown-render-block__source").element.hidden).toBe(true);
    expect(wrapper.text()).toContain("After the preview.");
    const toggle = wrapper.find('[data-markdown-action="render-mode"]');
    expect(toggle.text()).toBe("源码");
    await toggle.trigger("click");
    expect(wrapper.find(".markdown-render-block__source").element.hidden).toBe(false);
    expect(frame.element.parentElement.hidden).toBe(true);
    expect(toggle.text()).toBe("预览");
    copyText.mockResolvedValueOnce(true);
    await wrapper.find('[data-markdown-action="copy"]').trigger("click");
    await flushPromises();
    expect(copyText).toHaveBeenCalledWith(body);
    wrapper.unmount();
    copyText.mockReset();
  });

  it("expands to authenticated heights, ignores invalid reports, and shows pathological sizes", async () => {
    const context = { agentId: ref("agent-a"), readImage: vi.fn(), openResource: vi.fn() };
    const text = '<dagents-render type="html">\n<p>Short content</p>\n</dagents-render>';
    const wrapper = mount(ResourceMarkdown, {
      props: { entry: { id: "height-1", kind: "assistant", text }, agentId: "agent-a" },
      attachTo: document.body,
      global: { provide: { [ResourceRenderContextKey]: context } },
    });
    await flushPromises();
    const frame = wrapper.get("iframe");
    const channel = frame.attributes("srcdoc").match(/data-channel="([a-f0-9]{32})"/)?.[1];
    expect(channel).toMatch(/^[a-f0-9]{32}$/);
    const report = (source, data) => window.dispatchEvent(new MessageEvent("message", { source, data }));
    report(window, { kind: "dagents-preview-height", channel, height: 80 });
    report(frame.element.contentWindow, { kind: "dagents-preview-height", channel: `${channel.slice(1)}0`, height: 90 });
    await new Promise((resolve) => requestAnimationFrame(resolve));
    expect(frame.element.style.height).toBe("24px");

    report(frame.element.contentWindow, { kind: "dagents-preview-height", channel, height: 91 });
    await vi.waitFor(() => expect(frame.element.style.height).toBe("91px"));
    report(frame.element.contentWindow, { kind: "dagents-preview-height", channel, height: 10000 });
    await vi.waitFor(() => expect(frame.element.style.height).toBe("10000px"));
    report(frame.element.contentWindow, { kind: "dagents-preview-height", channel, height: -20 });
    report(frame.element.contentWindow, { kind: "dagents-preview-height", channel, height: 0 });
    await new Promise((resolve) => requestAnimationFrame(resolve));
    expect(frame.element.style.height).toBe("10000px");
    report(frame.element.contentWindow, { kind: "dagents-preview-height", channel, height: 1 });
    await vi.waitFor(() => expect(frame.element.style.height).toBe("24px"));
    report(frame.element.contentWindow, { kind: "dagents-preview-height", channel, height: 91 });
    await vi.waitFor(() => expect(frame.element.style.height).toBe("91px"));

    const postMessage = vi.spyOn(frame.element.contentWindow, "postMessage");
    const toggle = wrapper.find('[data-markdown-action="render-mode"]');
    await toggle.trigger("click");
    await toggle.trigger("click");
    expect(postMessage).toHaveBeenCalledWith({ kind: "dagents-preview-measure", channel }, "*");

    const oldFrame = frame.element;
    await wrapper.setProps({ entry: { id: "height-1", kind: "assistant", text: `${text}\n\nAdditional streamed Markdown.` } });
    await flushPromises();
    const currentFrame = wrapper.get("iframe").element;
    expect(currentFrame.style.height).toBe("91px");
    report(oldFrame.contentWindow, { kind: "dagents-preview-height", channel, height: 130 });
    await new Promise((resolve) => requestAnimationFrame(resolve));
    expect(currentFrame.style.height).toBe("91px");
    wrapper.unmount();
    postMessage.mockRestore();

    const compact = mount(ResourceMarkdown, {
      props: { entry: { id: "height-dock", kind: "assistant", text }, agentId: "agent-a", source: "dock" },
      attachTo: document.body,
      global: { provide: { [ResourceRenderContextKey]: context } },
    });
    await flushPromises();
    const compactFrame = compact.get("iframe");
    const compactChannel = compactFrame.attributes("srcdoc").match(/data-channel="([a-f0-9]{32})"/)?.[1];
    report(compactFrame.element.contentWindow, { kind: "dagents-preview-height", channel: compactChannel, height: 900 });
    await vi.waitFor(() => expect(compactFrame.element.style.height).toBe("900px"));
    report(compactFrame.element.contentWindow, { kind: "dagents-preview-height", channel: compactChannel, height: 1_000_001 });
    await vi.waitFor(() => expect(compact.text()).toContain("HTML 预览尺寸超出显示范围"));
    expect(compact.find("iframe").exists()).toBe(false);
    compact.unmount();
  });

  it("fails closed when a cryptographic preview channel is unavailable", async () => {
    const context = { agentId: ref("agent-a"), readImage: vi.fn(), openResource: vi.fn() };
    vi.stubGlobal("crypto", undefined);
    const wrapper = mount(ResourceMarkdown, {
      props: { entry: { kind: "assistant", text: '<dagents-render type="html">\n<p>content</p>\n</dagents-render>' }, agentId: "agent-a" },
      attachTo: document.body,
      global: { provide: { [ResourceRenderContextKey]: context } },
    });
    try {
      await flushPromises();
      expect(wrapper.find("iframe").exists()).toBe(false);
      expect(wrapper.text()).toContain("HTML 预览不可用");
    } finally {
      wrapper.unmount();
      vi.unstubAllGlobals();
    }
  });

  it("toggles explicit blocks independently and preserves mode for the same streaming message", async () => {
    const context = { agentId: ref("agent-a"), readImage: vi.fn(), openResource: vi.fn() };
    const first = "<p>First</p>";
    const second = "<div>Second</div>";
    const block = (type, body) => `<dagents-render type="${type}">\n${body}\n</dagents-render>`;
    const makeText = (one, two) => `${block("html", one)}\n\n${block("html", two)}`;
    const wrapper = mount(ResourceMarkdown, {
      props: { entry: { id: "message-1", kind: "assistant", text: makeText(first, second) }, agentId: "agent-a" },
      attachTo: document.body,
      global: { provide: { [ResourceRenderContextKey]: context } },
    });
    await flushPromises();
    let toggles = wrapper.findAll('[data-markdown-action="render-mode"]');
    expect(toggles).toHaveLength(2);
    await toggles[0].trigger("click");
    expect(toggles[0].text()).toBe("预览");
    expect(toggles[1].text()).toBe("源码");
    expect(wrapper.findAll(".markdown-render-block__source")[0].element.hidden).toBe(false);
    expect(wrapper.findAll(".markdown-render-block__source")[1].element.hidden).toBe(true);

    const updated = "<p>First streamed update</p>";
    await wrapper.setProps({ entry: { id: "message-1", kind: "assistant", text: makeText(updated, second) } });
    await flushPromises();
    toggles = wrapper.findAll('[data-markdown-action="render-mode"]');
    expect(toggles[0].text()).toBe("预览");
    expect(toggles[1].text()).toBe("源码");
    expect(wrapper.findAll("iframe")[0].attributes("srcdoc")).toContain("First streamed update");

    await wrapper.setProps({ entry: { id: "message-2", kind: "assistant", text: makeText(updated, second) } });
    await flushPromises();
    expect(wrapper.findAll('[data-markdown-action="render-mode"]')[0].text()).toBe("源码");
    expect(wrapper.findAll(".markdown-render-block__source")[0].element.hidden).toBe(true);
    wrapper.unmount();
  });

  it("does not preview raw HTML/SVG, fenced examples, inline tags, or wrappers inside code fences", async () => {
    const context = { agentId: ref("agent-a"), readImage: vi.fn(), openResource: vi.fn() };
    const source = [
      '<div>raw HTML</div>',
      '<svg xmlns="http://www.w3.org/2000/svg"><circle r="4"/></svg>',
      "```html", '<dagents-render type="html"><p>fenced</p></dagents-render>', "```",
      "~~~svg", '<svg xmlns="http://www.w3.org/2000/svg"><circle r="5"/></svg>', "~~~",
      "Inline <dagents-render type=\"html\">tag</dagents-render>.",
      "```text", '<dagents-render type="svg">', "```",
    ].join("\n\n");
    const wrapper = mount(ResourceMarkdown, {
      props: { entry: { kind: "assistant", text: source }, agentId: "agent-a" },
      attachTo: document.body,
      global: { provide: { [ResourceRenderContextKey]: context } },
    });
    await flushPromises();
    expect(wrapper.find("iframe").exists()).toBe(false);
    expect(wrapper.find("img.markdown-live-preview__svg").exists()).toBe(false);
    expect(wrapper.find(".markdown-render-block").exists()).toBe(false);
    expect(wrapper.find('[data-markdown-action="render-mode"]').exists()).toBe(false);
    expect(wrapper.text()).toContain("raw HTML");
    wrapper.unmount();
  });

  it("shows an explicit HTML doctype and keeps incomplete wrappers visible without activating their links", async () => {
    const context = { agentId: ref("agent-a"), readImage: vi.fn(), openResource: vi.fn() };
    const complete = mount(ResourceMarkdown, {
      props: { entry: { kind: "assistant", text: '<dagents-render type="html">\n<!doctype html>\n<html><body><h1>Card</h1></body></html>\n</dagents-render>' }, agentId: "agent-a" },
      attachTo: document.body,
      global: { provide: { [ResourceRenderContextKey]: context } },
    });
    await flushPromises();
    expect(complete.find("iframe").attributes("srcdoc")).toContain("<h1>Card</h1>");
    complete.unmount();

    const incomplete = mount(ResourceMarkdown, {
      props: { entry: { kind: "assistant", text: '<dagents-render type="html">\n<div>partial</div>\n[report](dagents://resource/file?path=reports%2Fx.md)\nFollowing user text' }, agentId: "agent-a" },
      attachTo: document.body,
      global: { provide: { [ResourceRenderContextKey]: context } },
    });
    await flushPromises();
    expect(incomplete.find("iframe").exists()).toBe(false);
    expect(incomplete.find(".resource-markdown__button").exists()).toBe(false);
    expect(incomplete.text()).toContain("Following user text");
    expect(incomplete.text()).toContain("dagents://resource/file");
    incomplete.unmount();
  });

  it("sanitizes explicit SVG previews into image blobs and opens them in the lightbox", async () => {
    const context = { agentId: ref("agent-a"), readImage: vi.fn(), openResource: vi.fn() };
    let previewBlob;
    const create = vi.spyOn(URL, "createObjectURL").mockImplementation((blob) => { previewBlob = blob; return "blob:sanitized-svg"; });
    const svg = '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10" onload="run()"><circle cx="5" cy="5" r="4"/><script>run()</script><foreignObject><div>unsafe</div></foreignObject></svg>';
    const wrapper = mount(ResourceMarkdown, {
      props: { entry: { kind: "assistant", text: `<dagents-render type="svg">\n${svg}\n</dagents-render>` }, agentId: "agent-a" },
      attachTo: document.body,
      global: { provide: { [ResourceRenderContextKey]: context } },
    });
    await flushPromises();
    expect(wrapper.find("img.markdown-live-preview__svg").exists()).toBe(true);
    expect(previewBlob.type).toBe("image/svg+xml");
    const sanitized = await readBlobText(previewBlob);
    expect(sanitized).not.toContain("onload");
    expect(sanitized).not.toContain("<script");
    expect(sanitized).not.toContain("foreignObject");
    await wrapper.find(".markdown-live-preview__image-button").trigger("click");
    expect(lightboxStore.open).toBe(true);
    wrapper.unmount();
    expect(lightboxStore.open).toBe(false);
    create.mockRestore();
  });
  it("reclaims superseded SVG preview URLs during streamed updates", async () => {
    let nextUrl = 0;
    const create = vi.spyOn(URL, "createObjectURL").mockImplementation(() => `blob:stream-svg-${++nextUrl}`);
    const revoke = vi.spyOn(URL, "revokeObjectURL").mockImplementation(() => {});
    const context = { agentId: ref("agent-a"), readImage: vi.fn(), openResource: vi.fn() };
    const svg = (radius) => `<svg xmlns="http://www.w3.org/2000/svg"><circle r="${radius}"/></svg>`;
    const wrapper = mount(ResourceMarkdown, {
      props: { entry: { kind: "assistant", text: `<dagents-render type="svg">\n${svg(4)}\n</dagents-render>` }, agentId: "agent-a" },
      attachTo: document.body,
      global: { provide: { [ResourceRenderContextKey]: context } },
    });
    await nextTick();
    expect(wrapper.find("img.markdown-live-preview__svg").exists()).toBe(true);
    await wrapper.setProps({ entry: { kind: "assistant", text: `<dagents-render type="svg">\n${svg(5)}\n</dagents-render>` } });
    await nextTick();
    expect(revoke).toHaveBeenCalledWith("blob:stream-svg-1");
    expect(create).toHaveBeenCalledTimes(2);
    wrapper.unmount();
    expect(revoke).toHaveBeenCalledWith("blob:stream-svg-2");
    create.mockRestore();
    revoke.mockRestore();
  });

  it("sanitizes SVG resources before displaying them as an image", async () => {
    let shownBlob;
    const create = vi.spyOn(URL, "createObjectURL").mockImplementation((blob) => { shownBlob = blob; return "blob:resource-svg"; });
    const unsafe = '<svg xmlns="http://www.w3.org/2000/svg"><path d="M0 0"/><script>run()</script><image href="https://outside.test/a.png"/></svg>';
    const context = { agentId: ref("agent-a"), readImage: vi.fn().mockResolvedValue(new Blob([unsafe], { type: "image/svg+xml" })), openResource: vi.fn() };
    const wrapper = mount(ResourceMarkdown, {
      props: { entry: { kind: "assistant", text: "![diagram](dagents://resource/image?path=images%2Fdiagram.svg)" }, agentId: "agent-a" },
      attachTo: document.body,
      global: { provide: { [ResourceRenderContextKey]: context } },
    });
    await vi.waitFor(() => {
      expect(context.readImage).toHaveBeenCalledWith("agent-a", "images/diagram.svg");
      expect(wrapper.find("img.resource-markdown__image").exists(), `Resource image state: ${wrapper.text()}`).toBe(true);
    });
    const safe = await readBlobText(shownBlob);
    expect(safe).not.toContain("<script");
    expect(safe).not.toContain("outside.test");
    wrapper.unmount();
    create.mockRestore();
  });

  it("keeps later links usable during a pending read, reuses streamed reads, and cleans the lightbox", async () => {
    let resolveImage;
    const imagePromise = new Promise((resolve) => { resolveImage = resolve; });
    const create = vi.spyOn(URL, "createObjectURL").mockReturnValue("blob:pending-resource");
    const revoke = vi.spyOn(URL, "revokeObjectURL").mockImplementation(() => {});
    const context = { agentId: ref("agent-a"), readImage: vi.fn(() => imagePromise), openResource: vi.fn() };
    const wrapper = mount(ResourceMarkdown, {
      props: { entry: { kind: "assistant", text: `${imageText} [report](dagents://resource/file?path=reports%2Fsummary.md)` }, agentId: "agent-a" },
      attachTo: document.body,
      global: { provide: { [ResourceRenderContextKey]: context } },
    });
    await nextTick();
    await flushPromises();
    expect(context.readImage).toHaveBeenCalledTimes(1);
    expect(wrapper.findAll("button")).toHaveLength(2);
    await wrapper.findAll("button")[1].trigger("click");
    expect(context.openResource).toHaveBeenCalledTimes(1);

    await wrapper.setProps({ entry: { kind: "assistant", text: `${imageText} streamed [report](dagents://resource/file?path=reports%2Fsummary.md)` } });
    await nextTick();
    await flushPromises();
    expect(context.readImage).toHaveBeenCalledTimes(1);
    resolveImage(new Blob(["image"]));
    await flushPromises();
    await nextTick();
    await wrapper.find("img.resource-markdown__image").trigger("click");
    expect(lightboxStore.open).toBe(true);
    wrapper.unmount();
    expect(lightboxStore.open).toBe(false);
    expect(revoke).toHaveBeenCalledWith("blob:pending-resource");
    create.mockRestore();
    revoke.mockRestore();
  });

  it("does not publish a pending image after its Agent changes and reports broken image data", async () => {
    let resolveImage;
    const imagePromise = new Promise((resolve) => { resolveImage = resolve; });
    const create = vi.spyOn(URL, "createObjectURL").mockReturnValue("blob:broken-resource");
    const context = { agentId: ref("agent-a"), readImage: vi.fn(() => imagePromise), openResource: vi.fn() };
    const wrapper = mount(ResourceMarkdown, {
      props: { entry: { kind: "assistant", text: imageText }, agentId: "agent-a" },
      attachTo: document.body,
      global: { provide: { [ResourceRenderContextKey]: context } },
    });
    await nextTick();
    await flushPromises();
    context.agentId.value = "agent-b";
    await nextTick();
    resolveImage(new Blob(["bad image"]));
    await flushPromises();
    expect(create).not.toHaveBeenCalled();
    expect(wrapper.find("button").element.disabled).toBe(true);
    wrapper.unmount();
    create.mockRestore();

    const brokenContext = { agentId: ref("agent-a"), readImage: vi.fn().mockResolvedValue(new Blob(["broken"])), openResource: vi.fn() };
    const broken = mount(ResourceMarkdown, {
      props: { entry: { kind: "assistant", text: imageText }, agentId: "agent-a" },
      attachTo: document.body,
      global: { provide: { [ResourceRenderContextKey]: brokenContext } },
    });
    await flushPromises();
    await nextTick();
    await broken.find("img").trigger("error");
    expect(broken.text()).toContain("加载失败");
    expect(broken.find("button").element.disabled).toBe(true);
    broken.unmount();
  });
});
