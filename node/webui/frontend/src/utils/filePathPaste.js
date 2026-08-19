/** 从 paste/drop 解析 Windows 文件绝对路径（F-P1 / F-P3 / F-X4）。 */

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
      if (/^\/[A-Za-z]:/.test(p)) p = p.slice(1);
      paths.push(p.replace(/\//g, "\\"));
    } catch {
      /* skip invalid URI */
    }
  }
  return paths;
}

/** @param {string[]} paths */
export function formatPathsForComposer(paths) {
  return paths.filter(Boolean).join("\n");
}

/**
 * 构建发送给 Agent 的文件引用前缀；路径不再直接写入输入框。
 * @param {string[]} paths
 */
export function formatFileReferences(paths) {
  return Array.from(new Set((paths || []).map((path) => String(path || "").trim()).filter(Boolean)))
    .map((path) => `'''引用文件：<file>${path}</file>'''`)
    .join("\n");
}

/** @param {string} text @param {string[]} paths */
export function buildMessageWithFileReferences(text, paths) {
  const message = String(text || "").trim();
  const references = formatFileReferences(paths);
  return [references, message].filter(Boolean).join("\n\n");
}

/**
 * @param {string} current
 * @param {string} insertion
 * @param {{ start: number, end: number }} selection
 */
export function mergePathInsertion(current, insertion, { start, end }) {
  const before = current.slice(0, start);
  const after = current.slice(end);
  const needsSep = before.length > 0 && !/[\n\s]$/.test(before);
  const sep = needsSep ? "\n" : "";
  const next = before + sep + insertion + after;
  const cursor = (before + sep + insertion).length;
  return { value: next, cursor };
}
