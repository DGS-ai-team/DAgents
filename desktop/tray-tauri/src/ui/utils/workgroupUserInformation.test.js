import { describe, expect, it } from "vitest";
import {
  workgroupMemberInformationRequests,
  workgroupUserInformationItems,
} from "./workgroupUserInformation.js";

const hitl = {
  hitl_id: "hitl-1",
  metadata: {
    source: "agent_ref",
    member_id: "member-1",
    assign_id: "assign-1",
    items: [
      {
        hitl_type: "user_information",
        id: "call-question",
        content: "部署到哪个环境？",
        user_information_args: {
          options: [{ id: "prod", label: "生产", value: "production" }],
          required: true,
        },
      },
      { hitl_type: "execute_tool", id: "call-bash", name: "bash_run" },
    ],
  },
};

describe("workgroup user information", () => {
  it("extracts only member user-information items with Node resume routing", () => {
    const items = workgroupUserInformationItems(hitl);
    expect(items).toHaveLength(1);
    expect(items[0].callId).toBe("call-question");
    expect(items[0].request).toMatchObject({
      toolCallId: "call-question",
      question: "部署到哪个环境？",
      required: true,
    });
  });

  it("projects a refresh-safe card model with the member display name", () => {
    const items = workgroupMemberInformationRequests([hitl], { "member-1": "部署成员" });
    expect(items).toHaveLength(1);
    expect(items[0]).toMatchObject({
      hitlId: "hitl-1",
      memberId: "member-1",
      memberLabel: "部署成员",
      assignId: "assign-1",
    });
  });
});
