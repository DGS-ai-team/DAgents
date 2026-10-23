/** 工具结果 entry 上的 media[] 元数据（F-M0）。 */
export function entryMedia(entry) {
  const list = entry?.data?.media;
  return Array.isArray(list) ? list.filter((m) => m && (m.url || m.id)) : [];
}

export function hasToolMedia(entry) {
  return entryMedia(entry).length > 0;
}

export function isShowImageTool(name) {
  return String(name || "").trim() === "show_image";
}

export function showImageResultSucceeded(entry) {
  const content = String(entry?.data?.content || "");
  return content.includes("[SHOW_IMAGE]") && content.includes("status=ok");
}

export function showImageCaption(entry) {
  const args = entry?.data?.arguments || {};
  const fromArgs = String(args.caption || "").trim();
  if (fromArgs) return fromArgs;
  const media = entryMedia(entry);
  return String(media[0]?.caption || "").trim();
}
