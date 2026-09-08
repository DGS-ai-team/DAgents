# Auto 数字员工实施进度与验收台账

依据：[开发方案](2026-09-08-auto-employee-implementation-plan.md)。开始日期：2026-09-08。状态：实施中，完整目标尚未完成。

## 分阶段状态

| 阶段 | 状态 | 当前证据/下一门槛 |
| --- | --- | --- |
| A 触发器归属与权限 | 编码中 | Luna 负责 owner/controller/revision、原子授权、迁移及回归；主 Agent 审查管理 HTTP 与模型身份边界 |
| B 员工与业务周期 | 现状梳理中 | 已确认 autonomy API/tool 按首个 managed Goal 查找，需替换为显式 current_goal_id；等待契约冻结 |
| C 收尾与调度意图 | 未开始 | 依赖 B；Goal 与 Trigger 当前不同文件，需单事实来源和可恢复投影 |
| D UI | 独立子包编码中 | 先做 Auto 标识与列表分组；总览、详情和新设置依赖 B/C API |
| E 工作区与事件 | 未开始 | 同目录展示不作为并发控制；写租约上线前不开放新并发承诺 |
| F 授权与维护 | 未开始 | 沿用既有 policy；后续扩展岗位授权、版本验证与回滚 |
| G Manage 汇总 | 未开始 | 依赖 Node 汇总契约；先只读且明确数据新鲜度 |

## 初始核对

- 开始实施时 Git 工作区干净，方案提交为 a8081baf。
- 当前触发器工具已有目标 Agent 过滤，但没有独立 owner 与存储原子授权。
- Server.Handler 当前为 access log 与 onboarding gate，不存在可直接复用的 Node 多用户认证。管理 HTTP 属于现有本地可信宿主边界，不接受客户端自报 Agent ID 作为身份。
- Goal 当前使用文件快照持久化；lookupGoalSession 基于专用 Session ID，历史查询必须保留，不能一律改成只查当前周期。
- 基础回归命令：`go test ./node/internal/goals ./node/internal/triggers ./node/internal/tools ./node/internal/api -timeout 120s`，2026-09-08 完成通过。此结果仅为初始基线，不证明正在修改的实现通过验收。

## 验收记录要求

每阶段记录实际提交、测试命令与结果、人工审查结论及未解决限制。最终审计逐项覆盖方案 T01–T10、真实 LLM 两条链、跨周期/每日维护、重启故障注入、Node/Manage 视觉验收，不用单元测试替代全部产品验收。未完成项保持可见。


## 第一轮实施审查

- UI 已在隔离前端 18768 验证显式 Auto 类型分组、普通筛选空状态、目录组折叠及不改变当前聊天；桌面 1280 和移动 390 像素截图检查通过初轮布局。搜索控件已改为独占一行，类型/分组放下一行，避免半行留空。后续仍需总览/详情与最终全页验收。
- 独立运行 `npm test --prefix node/webui/frontend -- --run src/components/NavRail.test.js src/utils/agentGrouping.test.js`，3 项通过；偏好 Node 隔离与更多交互测试仍在补充，不能据此宣称 D 完成。
- A 首轮仅元数据，退回补原子权限。随后发现恢复和 fire 存在检查后解锁再操作的问题，继续要求在实际 claim/恢复同锁校验，并增加真实竞争窗口测试。
- B 首轮新增 Profile/Cycle 存储，审查要求修复 map 遍历导致幂等随机失败、当前周期引用被任意修改、累计用量写盘回滚与越界计账问题。实际消耗即使超过预算也必须完整记账，再阻断后续运行；不能因预算耗尽拒绝记账。

## D 独立 UI 子包验收（2026-09-08）

AutoBadge 已用于侧栏、设置列表及 Auto 聊天提示；侧栏和设置列表支持类型分组、搜索与筛选，侧栏支持目录分组/折叠；私有工作目录按 Agent 独立分组。偏好以 bootstrap 的 Node ID 为作用域，包含实际 A→B→A 挂载恢复测试。

主 Agent 独立执行全量前端测试：62 个文件、329 项通过；ESLint 和 Vite build 通过。截图及交互检查覆盖隔离 Node 的桌面/390 像素列表、Auto 类型筛选、目录折叠及设置列表。当前子包可提交；Auto 总览、员工详情和新周期设置仍未实现，不代表 D 阶段完成。
