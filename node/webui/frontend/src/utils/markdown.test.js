import { describe, expect, it } from "vitest";
import { renderMarkdown } from "./markdown.js";

describe("renderMarkdown", () => {
  it("renders headings and inline formatting", () => {
    const html = renderMarkdown("# Title\n\n**bold** and `code`");
    expect(html).toContain("<h1>Title</h1>");
    expect(html).toContain("<strong>bold</strong>");
    expect(html).toContain("<code>code</code>");
  });

  it("renders horizontal rules", () => {
    const html = renderMarkdown("above\n\n---\n\nbelow");
    expect(html).toContain("<hr>");
    expect(html).toContain("above");
    expect(html).toContain("below");
    expect(html).toMatch(/<hr>\s*<p>below<\/p>/);
  });

  it("renders markdown tables", () => {
    const md = [
      "| Name | Value |",
      "| --- | --- |",
      "| foo | 1 |",
      "| bar | 2 |",
    ].join("\n");
    const html = renderMarkdown(md);
    expect(html).toContain("<table>");
    expect(html).toContain("<th>Name</th>");
    expect(html).toContain("<th>Value</th>");
    expect(html).toContain("<td>foo</td>");
    expect(html).toContain("<td>bar</td>");
    expect(html).not.toContain("<br>|");
  });

  it("does not treat table separator as horizontal rule", () => {
    const md = "| A | B |\n| --- | --- |\n| 1 | 2 |";
    const html = renderMarkdown(md);
    expect(html).toContain("<table>");
    expect(html).not.toContain("<hr>");
  });

  it("preserves fenced code blocks", () => {
    const html = renderMarkdown("```\n| not | table |\n---\n```");
    expect(html).toContain('<pre><code class="hljs">| not | table |\n---</code></pre>');
    expect(html).not.toContain("<table>");
  });

  it("renders GFM features and highlights named code fences", () => {
    const md = [
      "> a quote",
      "",
      "- [x] done",
      "- [ ] next",
      "",
      "~~removed~~",
      "",
      "```bash",
      "echo hello",
      "```",
    ].join("\n");
    const html = renderMarkdown(md);
    expect(html).toContain("<blockquote>");
    expect(html).toContain('type="checkbox"');
    expect(html).toContain("<del>removed</del>");
    expect(html).toContain('class="hljs language-bash"');
    expect(html).toContain("hello");
    expect(html).toContain('data-markdown-action="copy"');
    expect(html).toContain('data-markdown-action="download"');
    expect(html).not.toContain('data-code="echo hello"');
  });

  it("escapes raw HTML", () => {
    const html = renderMarkdown('<script>alert("xss")</script>');
    expect(html).not.toContain("<script>");
    expect(html).toContain("&lt;script&gt;");
  });

  it("renders unordered lists with inline formatting", () => {
    const html = renderMarkdown("- **成员 ID**：`mb_abc`\n- 显示名称：reader");
    expect(html).toContain("<ul>");
    expect(html).toContain("<li><strong>成员 ID</strong>：<code>mb_abc</code></li>");
    expect(html).toContain("<li>显示名称：reader</li>");
  });

  it("normalizes CRLF so headings and tables still render", () => {
    const md = [
      "Intro line.",
      "",
      "## Summary",
      "",
      "| Name | Value |",
      "| --- | --- |",
      "| foo | 1 |",
    ].join("\r\n");
    const html = renderMarkdown(md);
    expect(html).toContain("<h2>Summary</h2>");
    expect(html).toContain("<table>");
    expect(html).toContain("<td>foo</td>");
  });
});
