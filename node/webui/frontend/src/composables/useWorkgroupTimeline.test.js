import { ref } from "vue";
import { describe, expect, it } from "vitest";
import { useWorkgroupTimeline } from "./useWorkgroupTimeline.js";

function timeline(options = {}) {
  return useWorkgroupTimeline({
    events: ref([]),
    memberNameById: ref({ member: "Worker" }),
    memberApprovalByAssign: ref({}),
    selfNodeId: ref("node"),
    selfNodeName: ref("本机"),
    liveUser: ref(null),
    showLiveAssistant: ref(false),
    liveAssistant: ref({ id: "live", text: "" }),
    sending: ref(false),
    streamMode: ref("member"),
    streamActorId: ref("member"),
    streamPhase: ref("tool"),
    streamToolName: ref(""),
    statusWatermarkSeq: ref(0),
    expandedMemberReports: ref({}),
    expandedAssignTasks: ref({}),
    ...options,
  });
}

describe("useWorkgroupTimeline", () => {
  it("renders one approval batch for a direct member assignment", () => {
    const assignId = "as_direct_1";
    const events = ref([
      {
        type: "assign_started",
        event_id: "assign-started",
        assign_id: assignId,
        actor_id: "member",
        direct_member_id: "member",
        text: "@Worker\n检查项目",
      },
      {
        type: "tool_started",
        event_id: "tool-start-1",
        assign_id: assignId,
        actor_id: "member",
        direct_member_id: "member",
        tool_name: "read_file",
        tool_call_id: "call-1",
        text: "read_file",
      },
      {
        type: "tool_finished",
        event_id: "tool-finish-1",
        assign_id: assignId,
        actor_id: "member",
        direct_member_id: "member",
        tool_name: "read_file",
        tool_call_id: "call-1",
        status: "succeeded",
      },
      {
        type: "tool_started",
        event_id: "tool-start-2",
        assign_id: assignId,
        actor_id: "member",
        direct_member_id: "member",
        tool_name: "bash_run",
        tool_call_id: "call-2",
        text: "bash_run",
      },
      {
        type: "assistant_content",
        event_id: "assistant-preview",
        assign_id: assignId,
        actor_id: "member",
        direct_member_id: "member",
        text: "流式预览不应重复显示",
      },
      {
        type: "actor_final_text",
        event_id: "assistant-final",
        assign_id: assignId,
        actor_id: "member",
        direct_member_id: "member",
        text: "最终结论只显示一次",
      },
    ]);
    const memberApprovalByAssign = ref({
      [assignId]: {
        hitl_id: "hitl-1",
        metadata: {
          items: [
            { id: "call-1", name: "read_file", hitl_type: "execute_tool" },
            { id: "call-2", name: "bash_run", hitl_type: "execute_tool" },
          ],
        },
      },
    });
    const { eventGroups } = timeline({ events, memberApprovalByAssign });

    const items = eventGroups.value.flatMap((group) => group.items);
    const toolItems = items.filter((item) => item.kind === "tool");

    expect(toolItems).toHaveLength(2);
    expect(toolItems.filter((item) => item.approval)).toHaveLength(1);
    expect(toolItems.find((item) => item.approval)?.toolCallId).toBe("call-1");
    expect(toolItems.find((item) => item.approval)?.approval.items).toHaveLength(2);
    const messages = items.filter((item) => item.kind === "message");
    expect(messages).toHaveLength(1);
    expect(messages[0].ev.text).toBe("最终结论只显示一次");
  });
});
