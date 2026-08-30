/** 从 paste/drop 解析 Windows/Linux 本机文件路径。 */

/**
 * 是否应尝试 Shell clipboard API（避免纯文本粘贴误读旧 CF_HDROP）。
 * @param {{ text?: string, files?: FileList|File[] }} param
 */
export function shouldResolvePathsViaShell({ text, files }) {
  const trimmed = String(text || "").trim();
  const fileArr = Array.from(files || []);
  if (trimmed && fileArr.length === 0) return false;
  return true;
}

/** @param {FileList|File[]|null|undefined} files */
export function pathsFromFileList(files) {
  return Array.from(files || [])
    .map((f) => f.path)
    .filter(Boolean);
}

/**
 * Normalize a path without changing its meaning on Windows or Linux.
 * Windows paths are compared case-insensitively; Unix paths keep `/`.
 */
export function normalizeFilePath(rawPath) {
  let path = String(rawPath || "").trim().replace(/^['"]|['"]$/g, "");
  if (!path) return "";
  const isWindows = /^[A-Za-z]:[\\/]/.test(path) || path.startsWith("\\\\");
  if (isWindows) return path.replaceAll("/", "\\");
  if (path.length > 1) path = path.replace(/\/+/g, "/");
  return path;
}

export function fileNameFromPath(path) {
  const normalized = normalizeFilePath(path).replace(/[\\/]$/, "");
  return normalized.split(/[\\/]/).filter(Boolean).pop() || normalized;
}

export function fileReferenceKey(path) {
  const normalized = normalizeFilePath(path);
  return /^[A-Za-z]:[\\/]/.test(normalized) || normalized.startsWith("\\\\")
    ? normalized.toLowerCase()
    : normalized;
}

/** @param {string} path @param {string} source */
export function createFileReference(path, source = "unknown") {
  const normalized = normalizeFilePath(path);
  if (!normalized) return null;
  return {
    path: normalized,
    name: fileNameFromPath(normalized),
    displayPath: normalized,
    source,
    status: "ready",
  };
}

export function normalizeFileReferences(fileRefs) {
  const refs = [];
  const seen = new Set();
  for (const item of fileRefs || []) {
    const ref = createFileReference(item?.path ?? item, item?.source || "unknown");
    if (!ref) continue;
    const key = fileReferenceKey(ref.path);
    if (seen.has(key)) continue;
    seen.add(key);
    refs.push({ ...ref, name: String(item?.name || ref.name).trim() || ref.name });
    if (refs.length >= 8) break;
  }
  return refs;
}

/**
 * Convert legacy prompt markers into structured UI data. This is only for
 * history written before file_refs existed; new messages never use markers.
 */
export function extractFileReferencesFromMessage(text) {
  const source = String(text || "");
  const paths = [];
  const marker = /'''引用文件：<file>([\s\S]*?)<\/file>'''/g;
  const cleanText = source.replace(marker, (_match, path) => {
    const normalized = normalizeFilePath(path);
    if (normalized) paths.push(normalized);
    return "";
  }).replace(/^\s+|\s+$/g, "");
  const refs = [];
  const seen = new Set();
  for (const path of paths) {
    const key = fileReferenceKey(path);
    if (seen.has(key)) continue;
    seen.add(key);
    refs.push(createFileReference(path, "legacy"));
  }
  return { text: cleanText, fileRefs: refs.filter(Boolean) };
}
/**
 * @param {string} uriList text/uri-list 或 plain
 */
export function pathsFromUriList(uriList) {
  const paths = [];
  for (const line of String(uriList || "").split(/\r?\n/)) {
    const trimmed = line.trim();
    if (!trimmed || trimmed.startsWith("#")) continue;
    try {
      const url = new URL(trimmed);
      if (url.protocol !== "file:") continue;
      let p = decodeURIComponent(url.pathname);
      if (/^\/[A-Za-z]:/.test(p)) {
        p = p.slice(1).replace(/\//g, "\\");
      } else if (url.hostname) {
        p = `\\\\${url.hostname}${p.replace(/\//g, "\\")}`;
      }
      paths.push(p);
    } catch {
      /* skip invalid URI */
    }
  }
  return paths;
}
