import {
  approvalItemToolLabel,
  formatApprovalRawArguments,
  parseToolArguments,
  toolCallPurpose,
} from "@shared-frontend/utils/toolCalls.js";
import { truncateGraphemes } from "@shared-frontend/utils/textTruncate.js";

export function approvalArguments(item = {}) {
  if (
    item.arguments
    && typeof item.arguments === "object"
    && !Array.isArray(item.arguments)
    && Object.keys(item.arguments).length
  ) {
    return item.arguments;
  }
  const parsed = parseToolArguments(item.rawArgs || item.raw_arguments || item.arguments);
  if (Object.keys(parsed).length) return parsed;
  return item.arguments && typeof item.arguments === "object" && !Array.isArray(item.arguments)
    ? item.arguments
    : {};
}

export function approvalPurpose(item = {}) {
  const args = approvalArguments(item);
  const callPurpose = toolCallPurpose(args);
  if (callPurpose) return callPurpose;
  const purpose = String(args.purpose || "").replace(/\s+/g, " ").trim();
  return purpose ? truncateGraphemes(purpose, 48) : "";
}

export function approvalSummary(item = {}) {
  const purpose = approvalPurpose(item);
  if (purpose) return purpose;
  return approvalItemToolLabel({ ...item, arguments: {}, rawArgs: "" }) || "工具操作";
}

export function approvalRawJson(item = {}) {
  return formatApprovalRawArguments(item.rawArgs, approvalArguments(item));
}
