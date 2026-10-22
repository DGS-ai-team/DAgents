import { marked, Marked } from "marked";
import DOMPurifyModule from "dompurify";
import hljs from "highlight.js/lib/common";
import { parseResourceUrl } from "./resourceContext.js";
import { MAX_HTML_PREVIEW_CHARS_LIMIT, MAX_SVG_PREVIEW_CHARS_LIMIT } from "./markdownPreview.js";

const renderer = new marked.Renderer();
const MARKDOWN_CACHE_LIMIT = 256;
const MAX_PREVIEWS_PER_MESSAGE = 4;
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

const baseLink = renderer.link;
renderer.link = function (token) {
  if (String(token?.href || "").trim().toLowerCase().startsWith("dagents:")) {
    return this.parser.parseInline(token.tokens || []);
  }
  return baseLink.call(this, token);
};
const baseImage = renderer.image;
renderer.image = function (token) {
  if (String(token?.href || "").trim().toLowerCase().startsWith("dagents:")) return escapeHtml(token.text || "图片");
  return baseImage.call(this, token);
};

function renderCodeSource({ text, lang }) {
  const language = normalizeLanguage(lang);
  const languageClass = language ? ` language-${escapeHtml(language)}` : "";
  let content = escapeHtml(text);
  if (language && hljs.getLanguage(language)) content = hljs.highlight(text, { language }).value;
  return `<pre><code class="hljs${languageClass}">${content}</code></pre>`;
}

renderer.code = (token) => {
  const language = normalizeLanguage(token.lang);
  const label = language ? escapeHtml(language) : "文本";
  return [
    '<div class="markdown-code-block">',
    '<div class="markdown-code-block__toolbar">',
    `<span class="markdown-code-block__language">${label}</span>`,
    '<span class="markdown-code-block__actions">',
    '<button type="button" class="markdown-code-block__action" data-markdown-action="copy" aria-label="复制代码" title="复制代码">复制代码</button>',
    "</span>",
    "</div>",
    renderCodeSource(token),
    "</div>",
  ].join("");
};

// Keep raw HTML inert even in non-browser test environments where DOMPurify
// cannot create a DOM instance.
renderer.html = ({ text }) => escapeHtml(text);

marked.setOptions({ gfm: true, breaks: true, renderer });

export function renderMarkdown(text) {
  const source = String(text || "").replace(/\r\n?/g, "\n");
  const cached = markdownCache.get(source);
  if (cached !== undefined) return cached;
  const html = marked.parse(source);
  const rendered = sanitizer ? sanitizer.sanitize(html, { ADD_ATTR: ["target", "rel"] }) : html;
  markdownCache.set(source, rendered);
  if (markdownCache.size > MARKDOWN_CACHE_LIMIT) markdownCache.delete(markdownCache.keys().next().value);
  return rendered;
}

function tokenizeRenderWrapper(source) {
  const opening = /^( {0,3}<dagents-render\b[^>]*>)[ \t]*(?:\n|$)/i.exec(source);
  if (!opening) return undefined;
  const typeMatch = /^<dagents-render\s+type=(['"])(html|svg)\1\s*>$/i.exec(opening[1].trim());
  const rest = source.slice(opening[0].length);
  const lines = rest.match(/[^\n]*(?:\n|$)/g)?.filter((line) => line.length > 0) || [];
  let offset = 0;
  let fence = "";
  let depth = 1;
  let nested = false;

  for (const line of lines) {
    const content = line.replace(/\n$/, "");
    if (fence) {
      if (new RegExp(`^ {0,3}${fence[0]}{${fence.length},}[ \t]*$`).test(content)) fence = "";
    } else {
      const fenceOpen = /^ {0,3}(`{3,}|~{3,})/.exec(content);
      if (fenceOpen) fence = fenceOpen[1];
      else if (/^ {0,3}<dagents-render\b[^>]*>[ \t]*$/i.test(content)) {
        depth++;
        nested = true;
      } else if (/^ {0,3}<\/dagents-render>[ \t]*$/i.test(content)) {
        depth--;
        if (depth === 0) {
          const raw = source.slice(0, opening[0].length + offset + line.length);
          const body = rest.slice(0, offset).replace(/\n$/, "");
          if (!typeMatch || nested) return { type: "dagentsRender", raw, literal: true };
          return { type: "dagentsRender", raw, renderType: typeMatch[2].toLowerCase(), source: body };
        }
      }
    }
    offset += line.length;
  }

  // Preserve the whole incomplete region visibly so any trailing Markdown or
  // resource-looking text cannot become active before the wrapper is closed.
  return { type: "dagentsRender", raw: source, literal: true };
}

/** Assistant-only renderer. Resource placeholders originate only from Marked link/image tokens. */
export function renderResourceMarkdown(text) {
  const resources = [];
  const previews = [];
  let renderBlockIndex = 0;
  const source = String(text || "").replace(/\r\n?/g, "\n");
  const resourceRenderer = new marked.Renderer();
  Object.assign(resourceRenderer, renderer);
  resourceRenderer.link = function ({ href, title, tokens }) {
    const target = parseResourceUrl(href || "");
    const content = this.parser.parseInline(tokens || []);
    if (!target) {
      if (String(href || "").toLowerCase().startsWith("dagents:")) return content;
      return renderer.link.call(this, { href, title, tokens });
    }
    const index = resources.push(target) - 1;
    return `<span class="resource-placeholder" data-resource-index="${index}" data-resource-kind="${target.type}">${content}</span>`;
  };
  resourceRenderer.image = function ({ href, title, text }) {
    const target = parseResourceUrl(href || "");
    if (!target || target.type !== "image") {
      if (String(href || "").toLowerCase().startsWith("dagents:")) return escapeHtml(text || "图片");
      return renderer.image.call(this, { href, title, text });
    }
    const index = resources.push(target) - 1;
    return `<span class="resource-placeholder" data-resource-index="${index}" data-resource-kind="image">${escapeHtml(text || "图片")}</span>`;
  };
  const renderExtension = {
    name: "dagentsRender",
    level: "block",
    start(sourceText) {
      return /^(?= {0,3}<dagents-render\b[^>]*>[ \t]*$)/m.exec(sourceText)?.index;
    },
    tokenizer: tokenizeRenderWrapper,
    renderer(token) {
      if (token.literal) return escapeHtml(token.raw);
      const maxSize = token.renderType === "svg" ? MAX_SVG_PREVIEW_CHARS_LIMIT : MAX_HTML_PREVIEW_CHARS_LIMIT;
      if (previews.length >= MAX_PREVIEWS_PER_MESSAGE || token.source.length > maxSize) return escapeHtml(token.raw);
      const index = previews.push({ type: token.renderType, source: token.source }) - 1;
      const key = `render-${renderBlockIndex++}`;
      const label = escapeHtml(token.renderType.toUpperCase());
      const sourceHtml = `<div class="markdown-code-block"><div class="markdown-code-block__toolbar"><span class="markdown-code-block__language">${label}</span></div>${renderCodeSource({ text: token.source, lang: token.renderType })}</div>`;
      return [
        `<section class="markdown-render-block" data-render-key="${key}">`,
        '<div class="markdown-render-block__toolbar"><span class="markdown-code-block__actions">',
        '<button type="button" class="markdown-code-block__action" data-markdown-action="render-mode" data-render-mode="source" aria-label="显示源码">源码</button>',
        '<button type="button" class="markdown-code-block__action" data-markdown-action="copy">复制源码</button>',
        "</span></div>",
        `<div class="markdown-render-block__preview" data-markdown-preview-index="${index}"></div>`,
        `<div class="markdown-render-block__source" hidden>${sourceHtml}</div>`,
        "</section>",
      ].join("");
    },
  };
  const resourceParser = new Marked({ gfm: true, breaks: true, renderer: resourceRenderer, extensions: [renderExtension] });
  const html = resourceParser.parse(source);
  const safe = sanitizer
    ? sanitizer.sanitize(html, {
      ADD_ATTR: ["target", "rel", "data-resource-index", "data-resource-kind", "data-markdown-preview-index", "data-render-key", "data-markdown-action", "data-render-mode", "aria-label"],
    })
    : html;
  return { html: safe, resources, previews };
}

export function formatNumber(n) {
  const value = Number(n);
  if (!Number.isFinite(value) || value < 0) return "—";
  return value.toLocaleString("en-US");
}
