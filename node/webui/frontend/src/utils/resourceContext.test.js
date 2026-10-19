import { describe, expect, it } from "vitest";
import { parseResourceUrl, validateWorkspacePath } from "./resourceContext.js";
import { renderResourceMarkdown } from "./markdown.js";

describe("resource references", () => {
  it("accepts only documented resource forms and decodes once", () => {
    expect(parseResourceUrl("dagents://resource/file?path=reports%2Fsummary.md")).toEqual({ type: "file", path: "reports/summary.md" });
    expect(parseResourceUrl("dagents://resource/image?path=images%2Fplot.png")).toEqual({ type: "image", path: "images/plot.png" });
    expect(parseResourceUrl("dagents://resource/terminal?id=local-terminal-5")).toEqual({ type: "terminal", id: "local-terminal-5" });
    expect(parseResourceUrl("dagents://resource/file?path=reports%2Fgrowth%252e.csv")).toEqual({ type: "file", path: "reports/growth%2e.csv" });
    expect(parseResourceUrl("dagents://resource/file?path=images%2F100%25.png")).toEqual({ type: "file", path: "images/100%.png" });
  });

  it.each([
    "dagents://elsewhere/file?path=a", "dagents://resource/file?path=../secret",
    "dagents://resource/file?path=%2Fetc%2Fpasswd", "dagents://resource/file?path=C%3A%5Csecret",
    "dagents://resource/file?path=%5C%5Cserver%5Cshare", "dagents://resource/file?path=.dagents%2Fsecret",
    "dagents://resource/file?path=a%00b", "dagents://resource/file?path=a%3Ab", "dagents://resource/file?path=a&path=b",
    "dagents://resource/file?path=a&other=b",
    "dagents://resource/unknown?path=a", "dagents://resource/terminal?id=not real",
  ])("rejects unsafe URL %s", (url) => expect(parseResourceUrl(url)).toBeNull());

  it("validates decoded paths without decoding them a second time", () => {
    expect(validateWorkspacePath("reports/%2e%2e%2fsecret")).toBe(true);
    expect(validateWorkspacePath("reports/../secret")).toBe(false);
  });

  it("recognizes resource references only in markdown links and images", () => {
    const code = renderResourceMarkdown("`[fake](dagents://resource/file?path=x)`\n\n```md\n![fake](dagents://resource/image?path=x.png)\n```");
    expect(code.resources).toHaveLength(0);
    const brokenStream = renderResourceMarkdown("[partial](dagents://resource/file?path=");
    expect(brokenStream.resources).toHaveLength(0);
    const valid = renderResourceMarkdown("[报告](dagents://resource/file?path=reports%2Fsummary.md) ![图](dagents://resource/image?path=images%2Fplot.png)");
    expect(valid.resources).toHaveLength(2);
    expect(valid.html).toContain("data-resource-index=\"0\"");
    expect(valid.html).not.toContain("dagents://");
  });

  it("previews only complete explicit render blocks and leaves ordinary examples literal", () => {
    const input = [
      "```html", "<div>fenced example</div>", "```", "",
      '<div>raw HTML</div> <svg xmlns="http://www.w3.org/2000/svg"></svg>', "",
      "Inline <dagents-render type=\"html\">tag</dagents-render>.", "",
      "<dagents-render type=\"html\">", "<section><h2>HTML</h2></section>", "</dagents-render>",
      "Markdown immediately afterward.", "",
      "<dagents-render type=\"svg\">", "<svg xmlns=\"http://www.w3.org/2000/svg\">", "<circle r=\"4\"/>", "</svg>", "</dagents-render>",
    ].join("\n");
    const rendered = renderResourceMarkdown(input);
    expect(rendered.previews.map(({ type }) => type)).toEqual(["html", "svg"]);
    expect(rendered.html).toContain("markdown-render-block");
    expect(rendered.html).toContain("fenced example");
    expect(rendered.html).toContain("&lt;div&gt;raw HTML");
    expect(rendered.html).toContain("Markdown immediately afterward.");
  });

  it("shows incomplete, unknown, and nested wrappers as escaped literal text", () => {
    const incomplete = renderResourceMarkdown('<dagents-render type="html">\npartial\n[resource](dagents://resource/file?path=reports%2Fx.md)');
    expect(incomplete.previews).toHaveLength(0);
    expect(incomplete.resources).toHaveLength(0);
    expect(incomplete.html).toContain("&lt;dagents-render");
    expect(incomplete.html).toContain("dagents://resource/file");

    const unknown = renderResourceMarkdown('<dagents-render type="unknown">\n<p>literal</p>\n</dagents-render>\nTail');
    expect(unknown.previews).toHaveLength(0);
    expect(unknown.html).toContain("&lt;dagents-render type=&quot;unknown&quot;&gt;");
    expect(unknown.html).toContain("Tail");

    const nested = renderResourceMarkdown('<dagents-render type="html">\n<dagents-render type="svg">\n<svg></svg>\n</dagents-render>\n</dagents-render>');
    expect(nested.previews).toHaveLength(0);
    expect(nested.html).toContain("&lt;dagents-render type=&quot;svg&quot;&gt;");
  });

  it("does not activate wrapper-shaped text inside Markdown fences or inline code", () => {
    const rendered = renderResourceMarkdown('~~~html\n<dagents-render type="html">\n<div>example</div>\n</dagents-render>\n~~~\n\n`<dagents-render type="svg">`');
    expect(rendered.previews).toHaveLength(0);
    expect(rendered.html).toContain("markdown-code-block");
    expect(rendered.html).toContain("&lt;dagents-render");
  });

  it("enforces explicit wrapper syntax and preview limits", () => {
    const extraAttribute = renderResourceMarkdown('<dagents-render type="html" title="bad">\n<p>literal</p>\n</dagents-render>');
    expect(extraAttribute.previews).toHaveLength(0);
    expect(extraAttribute.html).toContain("&lt;dagents-render");

    const indented = renderResourceMarkdown('    <dagents-render type="html">\n    <p>code</p>\n    </dagents-render>');
    expect(indented.previews).toHaveLength(0);
    expect(indented.html).toContain("markdown-code-block");

    const block = (content) => `<dagents-render type="html">\n${content}\n</dagents-render>`;
    const capped = renderResourceMarkdown(Array.from({ length: 5 }, (_, index) => block(`<p>${index}</p>`)).join("\n\n"));
    expect(capped.previews).toHaveLength(4);
    expect(capped.html).toContain("&lt;dagents-render");

    const oversized = renderResourceMarkdown(block("x".repeat(64 * 1024 + 1)));
    expect(oversized.previews).toHaveLength(0);
    expect(oversized.html).toContain("&lt;dagents-render");
  });

  it("keeps unsafe resource links inert and escapes raw HTML", () => {
    const { html, resources } = renderResourceMarkdown("[越界](dagents://resource/file?path=..%2Fsecret) <script>alert(1)</script>");
    expect(resources).toHaveLength(0);
    expect(html).not.toContain("href=\"dagents:");
    expect(html).not.toContain("<script>");
  });
});
