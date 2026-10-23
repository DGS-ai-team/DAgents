/** F-M6：为 Node media URL 附加 thumbnail=1；lightbox 用原图 URL。 */
export function mediaThumbnailUrl(url) {
  const base = String(url || "").trim();
  if (!base || base.startsWith("data:") || base.startsWith("blob:")) return base;
  if (/[?&]thumbnail=1(?:&|$)/.test(base)) return base;
  const sep = base.includes("?") ? "&" : "?";
  return `${base}${sep}thumbnail=1`;
}

export function mediaFullUrl(url) {
  const base = String(url || "").trim();
  if (!base) return "";
  if (base.startsWith("data:") || base.startsWith("blob:")) return base;
  try {
    const u = new URL(base, window.location.origin);
    u.searchParams.delete("thumbnail");
    if (u.origin === window.location.origin) {
      return `${u.pathname}${u.search}`;
    }
    return u.toString();
  } catch {
    return base.replace(/([?&])thumbnail=1(?:&|$)/, (_, prefix) => (prefix === "?" ? "?" : "")).replace(/[?&]$/, "");
  }
}

export function mediaDownloadName(item) {
  const label = String(item?.label || item?.caption || item?.alt || "").trim();
  if (label) return label.replace(/[^\w.-]+/g, "_");
  const src = mediaFullUrl(item?.src || item?.url || "");
  const tail = src.split("/").pop()?.split("?")[0] || "";
  return tail || "image";
}
