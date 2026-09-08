export const DELIVERY_LABELS = Object.freeze({
  draft: "草稿",
  pending: "待发送",
  queued: "待发送",
  delivered: "已送达",
  failed: "发送失败",
  blocked: "已阻止转发",
});

export const PROCESSING_LABELS = Object.freeze({
  open: "待处理",
  in_progress: "处理中",
  pending: "待处理",
  processing: "处理中",
  resolved: "已解决",
  closed: "已关闭",
});

export function unwrapFeedback(value) {
  return value?.feedback || value?.item || value || null;
}

export function feedbackDelivery(value) {
  const item = unwrapFeedback(value);
  return DELIVERY_LABELS[String(item?.delivery_status || item?.delivery?.status || "pending")] || "待发送";
}

export function feedbackProcessing(value) {
  const item = unwrapFeedback(value);
  return PROCESSING_LABELS[String(item?.processing_status || item?.status || "pending")] || "待处理";
}

export function hasDestinationChanged(value) {
  const item = unwrapFeedback(value);
  return Boolean(item?.destination_changed || item?.manage_changed);
}

export function feedbackDraft(value) {
  const draft = value && typeof value === "object" ? value : {};
  return {
    type: draft.type === "suggestion" ? "suggestion" : "problem",
    title: String(draft.title || ""),
    body: String(draft.body || ""),
  };
}
