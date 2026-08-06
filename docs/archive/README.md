# 文档归档策略

本目录（及文首标明 **历史 / SUPERSEDED** 的设计文）存放**不再作为现行产品说明**的材料，避免新人把过期叙事当现状。

## 原则

| 类别 | 处理 |
|------|------|
| **现行** | `docs/handbook/`、现行 design（workgroup 契约、agent-instance-model 中与代码一致的部分）、architecture API |
| **冻结契约** | `docs/design/workgroup-d05-contracts.md` + `fixtures/workgroup-d05/` — 仍有效，但是契约不是教程 |
| **SUPERSEDED** | 文首大字标注；保留链接供溯源（如 Placement） |
| **历史审核 / 一次性纪要** | 保留在原 design 文末「历史」节，或迁入本 archive；**不**链到 README 主路径 |
| **临时草稿** | 合并进正式文后删除；禁止长期放在仓库根目录 |

## 已知 superseded / 历史入口

| 文档 | 状态 |
|------|------|
| [`../design/remote-agent-placement.md`](../design/remote-agent-placement.md) | SUPERSEDED → Workgroup |
| [`../design/workgroup-and-node-gateway.md`](../design/workgroup-and-node-gateway.md) §15–§16 | 历史审核纪要（正文以 §0–§13 为准） |
| [`../future/`](../future/) | 未落地远期方案，非现行承诺 |

## 清理检查（发版前）

- [ ] 根 README / handbook 入口不把「可选沙箱」「Placement」「`/v1/sessions*` CRUD」写成现行能力
- [ ] design/README 表格状态与分期（D0.5–D5）一致
- [ ] 死链：从 handbook README 点开的链接均可访问

现行验收清单：[v0.9.1-smoke-checklist.md](../design/v0.9.1-smoke-checklist.md) §10。
