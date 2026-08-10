# 工作组协作（Workgroup）

**适用版本**：v0.9.1 预览起  
**读者**：要用「多 Node / 多成员」协作的用户与联调同学  
**契约与分期**：[`../design/workgroup-and-node-gateway.md`](../design/workgroup-and-node-gateway.md) · [`../design/workgroup-d05-contracts.md`](../design/workgroup-d05-contracts.md)  
**验收**：[`../design/v0.9.1-smoke-checklist.md`](../design/v0.9.1-smoke-checklist.md)

---

## 1. 是什么

工作组把 **Manage** 当作 Supervisor（Leader），把各 **Node** 上的 **Member** 当作 Worker：

- 人类在 Console 或 Node Web UI 里对话。
- Supervisor 用工具 `assign_workgroup_task` **编排**成员，或用户用 **`@成员显示名`** **直达**成员。
- 成员在各自 **工作区目录** 内执行工具（默认文件系统；可选 Shell），结果经 Timeline / RunHistory 回灌。
- **组级工作区**（Supervisor / 组资产预留）由 **Manage** 在创建工作组时落盘：默认 `{MANAGE_DB_PATH 同级}/workgroup-workspaces/{workgroup_id}/`（可用 `MANAGE_WORKGROUP_WORKSPACES_DIR` 覆盖）。当前**不**挂 Supervisor FS 工具；成员工作区仍在 Home Node 的 `.runtime/workgroup-workers/…`。

成员 **不是** 侧栏里的本地 Agent：不能走 `/v1/agents` 本地消息 API，也不继承本机 Agent 的 skills / browser 会话。

---

## 2. 最小路径（单 Node）

1. 启动 Manage（`python run_manage.py`）与 Node（启用 `manage` + workgroup）。
2. 完成 Node 首配（未完成时 Dialer 不启动）。
3. Console 或 Node UI：**创建工作组** → **发布**（如需要）→ **添加成员**（Home = 本 Node）。
4. 等待成员 `ready`。
5. 在工作组对话中：
   - 让 Supervisor「让某某读一下 README」；或
   - 输入 `@某某 请总结工作区里的说明`。

---

## 3. 编排 vs `@` 直达

| 模式 | 谁在跑 LLM | 典型用途 |
|------|------------|----------|
| **编排** | Supervisor 决定何时 `assign_workgroup_task` | 多步分工、汇总 |
| **`@member`** | 跳过编排，直接对该成员跑 turn | 点名某成员干活 |

两种模式都受成员 **工具白名单** 与工作区路径约束。

---

## 4. 成员工具

权威目录：仓库 `shared/workgroup/member_tool_catalog.json`（Node `go:embed`；Manage/Console 读同一文件）。
HTTP：`GET /v1/workgroups/meta/member-tools`（Node 本地提供，**不依赖** Manage 连接；Manage 同路径供 Console）。

| 组 | 工具 | 默认勾选 |
|----|------|----------|
| **fs** | `read_file` `write_file` `glob_files` `grep_*` `search_replace` `show_image` `read_image` | ✅ |
| **bash** | `bash_run` `background_job_*` | ❌ 需显式勾选 |

- 路径必须是 **成员工作区相对路径**（禁止 `..` / 主机绝对路径）。
- bash **无额外沙箱**，cwd 默认工作区根；预览环境请谨慎勾选。
- Console / Node UI 均按 **工具组**（fs / bash）勾选；提交时展开为 `allow_tool_names`。

---

## 5. HITL 与取消

- Supervisor / 成员可触发 **询问用户**（信息型 HITL）；在 Console 或 Node 工作组 UI 回答。
- **取消当前 turn**：停止进行中的编排/成员循环；进行中的 `tool.command` 会尝试 `tool.cancel`，迟到结果不脏写。
- **多人同时发言**：后到的消息在 Manage 侧 **排队**（不并行开 Leader）；上一轮 assistant 收尾后按 FIFO 自动消费。Composer 上方显示排队位次，可修改或取消排队项。

---

## 6. 双 Node

1. 两台 Node 均注册到同一 Manage，并进入工作组 ACL。
2. 成员 **Home Node** 选实际执行机。
3. 在任意已订阅 Node 或 Console 上对话；工具在 Home 上执行。
4. 撤销 ACL / 退订后，该 Node 不再接收 Timeline / command（fencing）。

---

## 7. 调试

- **成员一直「配置中」（`provisioning`）**：先看 Manage 是否支持 WebSocket（`pip install websockets`，或 `uvicorn[standard]`）。缺库时 WS 握手会 404，Node 日志出现 `workgroup dialer stopped`；本版起 Dialer 会自动重连，但仍须 Manage 具备 WS。再查 `GET /v1/workgroups/{id}/outbox?unacked_only=true` 是否积压 `member.provision`。

- Timeline：公开进度（工具名级，不含 raw 参数）。
- **RunHistory**：`GET /v1/workgroups/{id}/runs` 与 `.../runs/{run_id}/history`（含 `llm.mode`）；UI「调试」侧栏可查看。

---

## 8. 明确不做（预览）

- 成员侧 browser / skills / triggers / wecom / 临时子 Agent
- 把成员当作本地 Agent 列表项去「打开对话」

更多产品决策见工作组设计正文 §0–§13。
