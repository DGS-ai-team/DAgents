export function isCurrentWorkgroupRequest(request, current) {
  return request?.epoch === current?.epoch && request?.workgroupId === current?.workgroupId && current?.mounted !== false
}

export function nextWorkgroupRequestState(current, workgroupId) {
  return { epoch: (Number(current?.epoch) || 0) + 1, workgroupId: String(workgroupId || ''), mounted: true }
}
