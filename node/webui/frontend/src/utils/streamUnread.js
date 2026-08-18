/** Return the number of stream items that were added since the previous render. */
export function countNewStreamItems(items = [], previousItems = []) {
  if (!Array.isArray(items) || !Array.isArray(previousItems)) return 0;
  const previousKeys = new Set(previousItems.map((item) => item?.key).filter(Boolean));
  return items.filter((item) => item?.key && !previousKeys.has(item.key)).length;
}
