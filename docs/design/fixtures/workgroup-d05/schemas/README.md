# Workgroup D0.5 JSON Schemas

Draft 2020-12 schemas for cross-language validation.

| File | Purpose |
|------|---------|
| `defs.json` | Shared ID/hash/error defs |
| `WorkGroup.json` / `WorkGroupACL.json` / `WorkGroupMember.json` | Manage group entities |
| `MemberSpec.json` / `NodeExecutionGrant.json` | Spec + execution grant |
| `TimelineEvent.json` / `ToolCommand.json` / `HITLRequest.json` | Runtime events/commands |
| [`manage/workgroup/schemas/assign_workgroup_task.openai.json`](../../../../../manage/workgroup/schemas/assign_workgroup_task.openai.json) | Manage runtime Leader tool parameters + result schema |
| `FixtureMeta.json` | Fixture envelope shape |

Normative prose remains in `docs/design/workgroup-d05-contracts.md`; schemas implement that prose.
