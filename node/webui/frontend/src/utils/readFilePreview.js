import { renderMarkdown } from "./markdown.js";

const MARKDOWN_EXT = new Set(["md", "markdown", "mdx"]);
const HTML_EXT = new Set(["html", "htm", "xhtml"]);
const JSON_EXT = new Set(["json"]);
const CSV_EXT = new Set(["csv", "tsv"]);
const CODE_EXT = new Set([
  "js",
  "jsx",
  "ts",
  "tsx",
  "py",
  "go",
  "rs",
  "java",
  "c",
  "cpp",
  "h",
  "hpp",
  "cs",
  "rb",
  "php",
  "sh",
  "bash",
  "zsh",
  "yaml",
  "yml",
  "toml",
  "xml",
  "sql",
  "vue",
  "css",
  "scss",
  "less",
]);

/** 拆分 read_file 工具输出：元数据头 + 正文（--- 分隔）。 */
export function splitReadFileOutput(raw) {
  const text = String(raw || "");
  const marker = "\n---\n";
  const idx = text.indexOf(marker);
  if (idx >= 0) {
    return { meta: text.slice(0, idx), body: text.slice(idx + marker.length) };
  }
  return { meta: "", body: text };
}

export function fileExtension(path) {
  const base = String(path || "").trim().split(/[/\\]/).pop() || "";
  const dot = base.lastIndexOf(".");
  if (dot < 0) return "";
  return base.slice(dot + 1).toLowerCase();
}

/** @returns {"markdown"|"html"|"json"|"csv"|"code"|"plain"} */
export function readFilePreviewMode(path) {
  const ext = fileExtension(path);
  if (MARKDOWN_EXT.has(ext)) return "markdown";
  if (HTML_EXT.has(ext)) return "html";
  if (JSON_EXT.has(ext)) return "json";
  if (CSV_EXT.has(ext)) return "csv";
  if (CODE_EXT.has(ext)) return "code";
  return "plain";
}

export function parseCsvRows(text, ext) {
  const delim = ext === "tsv" ? "\t" : ",";
  const rows = [];
  for (const line of String(text || "").split("\n")) {
    if (!line.trim()) continue;
    rows.push(line.split(delim).map((c) => c.trim()));
  }
  return rows;
}

export function formatJsonBody(text) {
  const trimmed = String(text || "").trim();
  if (!trimmed) return "";
  try {
    return JSON.stringify(JSON.parse(trimmed), null, 2);
  } catch {
    return trimmed;
  }
}

export function buildReadFilePreview(path, rawContent) {
  const { meta, body } = splitReadFileOutput(rawContent);
  const mode = readFilePreviewMode(path);
  const filePath = String(path || "").trim();
  return {
    mode,
    meta,
    body,
    path: filePath,
    html: mode === "markdown" ? renderMarkdown(body) : "",
    jsonText: mode === "json" ? formatJsonBody(body) : "",
    csvRows: mode === "csv" ? parseCsvRows(body, fileExtension(path)) : [],
  };
}

export function isReadFileTool(name) {
  return String(name || "").trim() === "read_file";
}
