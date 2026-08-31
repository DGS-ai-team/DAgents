import { extractUserInfo } from "../stores/hitl.js";

function rawWorkgroupHitlItems(hitl) {
  const metadata = hitl?.metadata && typeof hitl.metadata === "object" ? hitl.metadata : {};
  const raw = metadata.items || hitl?.items || [];
  return Array.isArray(raw) ? raw : [];
}

export function workgroupUserInformationItems(hitl) {
  const seen = new Set();
  return rawWorkgroupHitlItems(hitl)
    .filter((item) => String(item?.hitl_type || "").trim() === "user_information")
    .map((item) => {
      const callId = String(item?.id || item?.tool_call_id || "").trim();
      const args =
        item?.user_information_args && typeof item.user_information_args === "object"
          ? item.user_information_args
          : {};
      const data = {
        ...item,
        tool_call_id: callId,
        user_information_args: {
          ...args,
          tool_call_id: String(args.tool_call_id || callId).trim(),
        },
      };
      return {
        callId,
        data,
        request: extractUserInfo(data),
      };
    })
    .filter((item) => item.callId)
    .filter((item) => {
      if (seen.has(item.callId)) return false;
      seen.add(item.callId);
      return true;
    });
}

export function workgroupMemberInformationRequests(hitls, memberNameById = {}) {
  const out = [];
  for (const hitl of Array.isArray(hitls) ? hitls : []) {
    const metadata = hitl?.metadata && typeof hitl.metadata === "object" ? hitl.metadata : {};
    if (String(metadata.source || hitl?.source || "").trim() !== "agent_ref") continue;
    const hitlId = String(hitl?.hitl_id || hitl?.id || "").trim();
    const memberId = String(metadata.member_id || hitl?.member_id || "").trim();
    const assignId = String(metadata.assign_id || hitl?.assign_id || "").trim();
    const memberLabel = String(memberNameById?.[memberId] || memberId || "成员").trim();
    for (const item of workgroupUserInformationItems(hitl)) {
      out.push({
        ...item,
        key: `${hitlId}:${item.callId}`,
        hitlId,
        memberId,
        memberLabel,
        assignId,
      });
    }
  }
  return out;
}
