export function workgroupApprovalItemsFromMetadata(metadata) {
  const value = metadata && typeof metadata === "object" ? metadata : {};
  const approvalArgs =
    value.approval_args && typeof value.approval_args === "object"
      ? value.approval_args
      : {};
  const raw = value.items || approvalArgs.tool_calls || [];
  if (!Array.isArray(raw)) return [];
  return raw.filter((item) => !item?.hitl_type || item.hitl_type === "execute_tool");
}

export function workgroupApprovalItems(hitl) {
  const metadata = hitl?.metadata && typeof hitl.metadata === "object" ? hitl.metadata : {};
  const approvalArgs =
    metadata.approval_args && typeof metadata.approval_args === "object"
      ? metadata.approval_args
      : {};
  const raw = metadata.items || approvalArgs.tool_calls || hitl?.items || [];
  if (!Array.isArray(raw)) return [];
  const seen = new Set();
  return raw
    .filter((item) => !item?.hitl_type || item.hitl_type === "execute_tool")
    .map((item) => ({
      callId: String(item?.id || item?.tool_call_id || "").trim(),
      name: String(item?.name || item?.tool_name || "").trim() || "unknown",
      arguments: item?.arguments && typeof item.arguments === "object" ? item.arguments : {},
      rawArgs: String(item?.raw_arguments || ""),
      reason: String(item?.approval_reason || "").trim(),
      risk: String(item?.risk_level || "").trim().toLowerCase(),
      duplicateWindowSec: Number(
        item?.duplicate_meta?.window_seconds || item?.duplicate_meta?.window_sec || 0,
      ) || 0,
      duplicatePreview: String(item?.duplicate_meta?.result_preview || "").trim(),
    }))
    .filter((item) => item.callId)
    .filter((item) => {
      if (seen.has(item.callId)) return false;
      seen.add(item.callId);
      return true;
    });
}
