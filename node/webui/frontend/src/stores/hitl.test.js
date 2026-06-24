import { describe, expect, it } from "vitest";
import { approvalItemDisplayName } from "../utils/toolCalls.js";
import {
  buildApprovalOneResume,
  buildApprovalSelectionResume,
  expandHitlRequired,
  extractToolApprovals,
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

describe("expandHitlRequired", () => {
  it("preserves child_session_id for temporary agent tool approval", () => {
    const { userInfos, approval } = expandHitlRequired({
      hitl_id: "hitl-child-1",
      child_session_id: "child-abc",
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
    expect(approval?.child_session_id).toBe("child-abc");
    expect(approval?.hitl_scope).toBe("temporary_agent");
    expect(approval?.child_purpose).toBe("research");
    expect(buildApprovalOneResume(approval, "call-1", true).child_session_id).toBe("child-abc");
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
});
