import { marked } from "marked";
import DOMPurifyModule from "dompurify";
import hljs from "highlight.js/lib/common";

const renderer = new marked.Renderer();
const MARKDOWN_CACHE_LIMIT = 256;
const markdownCache = new Map();
const sanitizer =
  typeof DOMPurifyModule === "function"
    ? typeof window !== "undefined"
      ? DOMPurifyModule(window)
      : null
    : DOMPurifyModule;

function escapeHtml(value) {
  return String(value ?? "")
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&#39;");
}

function normalizeLanguage(language) {
  return String(language || "")
    .trim()
    .split(/\s+/, 1)[0]
    .toLowerCase();
}

renderer.code = ({ text, lang }) => {
  const language = normalizeLanguage(lang);
  const languageClass = language ? ` language-${escapeHtml(language)}` : "";
  let content = escapeHtml(text);

  if (language && hljs.getLanguage(language)) {
    content = hljs.highlight(text, { language }).value;
  }

  const label = language ? escapeHtml(language) : "文本";
  return [
    '<div class="markdown-code-block">',
    '<div class="markdown-code-block__toolbar">',
    `<span class="markdown-code-block__language">${label}</span>`,
    '<span class="markdown-code-block__actions">',
    '<button type="button" class="markdown-code-block__action" data-markdown-action="copy" aria-label="复制代码" title="复制代码">复制代码</button>',
    "</span>",
    "</div>",
    `<pre><code class="hljs${languageClass}">${content}</code></pre>`,
    "</div>",
  ].join("");
};

// Keep raw HTML inert even in non-browser test environments where DOMPurify
// cannot create a DOM instance.
renderer.html = ({ text }) => escapeHtml(text);

marked.setOptions({
  gfm: true,
  breaks: true,
  renderer,
});

export function renderMarkdown(text) {
  const source = String(text || "").replace(/\r\n?/g, "\n");
  const cached = markdownCache.get(source);
  if (cached !== undefined) return cached;
  const html = marked.parse(source);
  const rendered = sanitizer
    ? sanitizer.sanitize(html, { ADD_ATTR: ["target", "rel"] })
    : html;
  markdownCache.set(source, rendered);
  if (markdownCache.size > MARKDOWN_CACHE_LIMIT) {
    markdownCache.delete(markdownCache.keys().next().value);
  }
  return rendered;
}

export function formatNumber(n) {
  const value = Number(n);
  if (!Number.isFinite(value) || value < 0) return "—";
  return value.toLocaleString("en-US");
}
