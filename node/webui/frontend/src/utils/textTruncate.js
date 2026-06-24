/** 按 grapheme 截断，避免在多字节字符中间切断导致乱码。 */
export function truncateGraphemes(text, maxLen) {
  const s = String(text ?? "");
  if (maxLen <= 0 || !s) return "";
  if (typeof Intl !== "undefined" && typeof Intl.Segmenter === "function") {
    const seg = new Intl.Segmenter(undefined, { granularity: "grapheme" });
    let count = 0;
    let out = "";
    for (const { segment } of seg.segment(s)) {
      if (count >= maxLen - 1) return `${out}…`;
      out += segment;
      count += 1;
    }
    return s;
  }
  const chars = [...s];
  if (chars.length <= maxLen) return s;
  return `${chars.slice(0, maxLen - 1).join("")}…`;
}
