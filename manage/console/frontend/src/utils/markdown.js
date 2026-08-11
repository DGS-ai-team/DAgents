import { marked } from "marked";
import DOMPurifyModule from "dompurify";
import hljs from "highlight.js/lib/common";

const renderer = new marked.Renderer();
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

  return `<pre><code class="hljs${languageClass}">${content}</code></pre>`;
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
  const html = marked.parse(source);
  return sanitizer
    ? sanitizer.sanitize(html, { ADD_ATTR: ["target", "rel"] })
    : html;
}
