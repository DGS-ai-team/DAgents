import { describe, expect, it } from "vitest";
import { approvalItemDisplayName } from "../utils/toolCalls.js";
import {
  buildApprovalOneResume,
  buildApprovalSelectionResume,
  buildUserInfoResumeFromSelection,
  resolveUserInfoSelectionIds,
  approvalQueueKey,
  clearHitl,
  dequeueHitlAt,
  enqueueHitl,
  expandHitlRequired,
  extractUserInfo,
  extractToolApprovals,
  hitlStore,
} from "./hitl.js";

const multiToolApprovalData = {
  approval_id: "hitl-1",
  approval_args: {
    tool_calls: [
      {
        id: "call-a",
        name: "read_file",
        arguments: {},
        raw_arguments: '{"path":"/tmp/a.txt"}',
      },
      {
        id: "call-b",
        name: "bash_run",
        arguments: {},
        raw_arguments: '{"command":"echo hi"}',
      },
    ],
  },
};

describe("extractToolApprovals", () => {
  it("parses nested function.name and function.arguments", () => {
    const items = extractToolApprovals({
      approval_args: {
        tool_calls: [
          {
            id: "call-1",
            function: { name: "write_file", arguments: '{"path":"/tmp/out.txt"}' },
          },
        ],
      },
    });
    expect(items).toHaveLength(1);
    expect(items[0].name).toBe("write_file");
    expect(items[0].arguments).toEqual({ path: "/tmp/out.txt" });
    expect(approvalItemDisplayName(items[0])).toBe("write_file(/tmp/out.txt)");
  });

  it("falls back to raw_arguments when arguments map is empty", () => {
    const items = extractToolApprovals(multiToolApprovalData);
    expect(items).toHaveLength(2);
    expect(approvalItemDisplayName(items[0])).toBe("read_file(/tmp/a.txt)");
    expect(approvalItemDisplayName(items[1])).toBe("bash(echo hi)");
  });
});

describe("buildApprovalSelectionResume", () => {
  it("covers every pending tool call in selection resume", () => {
    const resume = buildApprovalSelectionResume(multiToolApprovalData, {
      "call-a": true,
      "call-b": false,
    });
    expect(resume.type).toBe("selection");
    expect(resume.approved).toEqual(["call-a"]);
    expect(resume.rejected).toEqual(["call-b"]);
    expect(resume.approved.length + resume.rejected.length).toBe(2);
  });
});

describe("user_information multi-select", () => {
  const userInfoData = {
    user_information_args: {
      tool_call_id: "ask-1",
      question: "请选择",
      allow_multiple: true,
      options: [
        { id: "a", label: "选项A", value: "A" },
        { id: "b", label: "选项B", value: "B" },
      ],
    },
  };

  it("extracts allowMultiple from payload", () => {
    const req = extractUserInfo(userInfoData);
    expect(req.allowMultiple).toBe(true);
    expect(req.options).toHaveLength(2);
  });

  it("builds selected_options with multiple ids", () => {
    const resume = buildUserInfoResumeFromSelection(userInfoData, ["b", "a"]);
    expect(resume.type).toBe("user_information");
    expect(resume.tool_call_id).toBe("ask-1");
    expect(resume.selected_options).toEqual(["a", "b"]);
    expect(resume.answer).toBe("A, B");
  });
});

describe("resolveUserInfoSelectionIds", () => {
  const multiReq = {
    allowMultiple: true,
    options: [
      { id: "go", label: "继续", value: "go" },
      { id: "stop", label: "停止", value: "stop" },
      { id: "retry", label: "重试", value: "retry" },
    ],
  };
  const singleReq = {
    allowMultiple: false,
    options: multiReq.options,
  };

  it("maps single-select index to option id (not always first)", () => {
    expect(resolveUserInfoSelectionIds(singleReq, 0)).toEqual(["go"]);
    expect(resolveUserInfoSelectionIds(singleReq, 1)).toEqual(["stop"]);
    expect(resolveUserInfoSelectionIds(singleReq, 2)).toEqual(["retry"]);
  });

  it("returns empty for invalid single-select index (no silent first fallback)", () => {
    expect(resolveUserInfoSelectionIds(singleReq, [])).toEqual([]);
    expect(resolveUserInfoSelectionIds(singleReq, Number.NaN)).toEqual([]);
    expect(resolveUserInfoSelectionIds(singleReq, 99)).toEqual([]);
    expect(resolveUserInfoSelectionIds(singleReq, "1")).toEqual([]);
  });

  it("keeps multi-select ids and drops unknown ids", () => {
    expect(resolveUserInfoSelectionIds(multiReq, ["retry", "go"])).toEqual(["retry", "go"]);
    expect(resolveUserInfoSelectionIds(multiReq, ["retry", "nope"])).toEqual(["retry"]);
    expect(resolveUserInfoSelectionIds(multiReq, [])).toEqual([]);
  });
});

describe("expandHitlRequired", () => {
  it("preserves child_agent_id for temporary agent tool approval", () => {
    const { userInfos, approval } = expandHitlRequired({
      hitl_id: "hitl-child-1",
      child_agent_id: "child-abc",
      hitl_scope: "temporary_agent",
      child_purpose: "research",
      items: [
        {
          hitl_type: "execute_tool",
          id: "call-1",
          name: "bash_run",
          raw_arguments: '{"command":"ls"}',
        },
      ],
    });
    expect(userInfos).toHaveLength(0);
    expect(approval?.child_agent_id).toBe("child-abc");
    expect(approval?.hitl_scope).toBe("temporary_agent");
    expect(approval?.child_purpose).toBe("research");
    expect(buildApprovalOneResume(approval, "call-1", true).child_agent_id).toBe("child-abc");
  });
});

describe("buildApprovalOneResume", () => {
  it("approves one tool and rejects the rest", () => {
    const resume = buildApprovalOneResume(multiToolApprovalData, "call-a", true);
    expect(resume.approved).toEqual(["call-a"]);
    expect(resume.rejected).toEqual(["call-b"]);
  });

  it("reject-one rejects entire batch without approving siblings", () => {
    const resume = buildApprovalOneResume(multiToolApprovalData, "call-b", false);
    expect(resume.approved).toEqual([]);
    expect(resume.rejected).toEqual(["call-a", "call-b"]);
  });

  it("includes a2a_task_id in resume routing", () => {
    const data = { ...multiToolApprovalData, a2a_task_id: "task-xyz" };
    const resume = buildApprovalOneResume(data, "call-a", true);
    expect(resume.a2a_task_id).toBe("task-xyz");
  });
});

describe("approvalQueueKey", () => {
  it("distinguishes concurrent A2A tasks", () => {
    const a = approvalQueueKey({ a2a_task_id: "task-a", approval_args: { tool_calls: [] } });
    const b = approvalQueueKey({ a2a_task_id: "task-b", approval_args: { tool_calls: [] } });
    expect(a).not.toBe(b);
  });

  it("does not collapse missing ids to the same key", () => {
    const a = approvalQueueKey({
      approval_args: { tool_calls: [{ id: "call-1", name: "read_file", arguments: {} }] },
    });
    const b = approvalQueueKey({
      approval_args: { tool_calls: [{ id: "call-2", name: "bash_run", arguments: {} }] },
    });
    expect(a).not.toBe(b);
  });
});

describe("dequeueHitlAt", () => {
  it("removes the item at the given index", () => {
    clearHitl();
    enqueueHitl({ kind: "approval", data: { approval_id: "a", approval_args: { tool_calls: [] } } });
    enqueueHitl({ kind: "approval", data: { approval_id: "b", approval_args: { tool_calls: [] } } });
    const removed = dequeueHitlAt(1);
    expect(removed?.data?.approval_id).toBe("b");
    expect(hitlStore.queue).toHaveLength(1);
    expect(hitlStore.queue[0].data.approval_id).toBe("a");
    clearHitl();
  });
});
