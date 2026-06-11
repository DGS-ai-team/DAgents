# 落地案例（索引）

**案例已迁至仓库根目录 [`cases/`](../../cases/)**，每案含说明文档与 Docker 复现环境。

| 类型 | 位置 | 侧重 |
|------|------|------|
| **规范与接口** | [`docs/`](../)（与本目录平级） | 与代码行为对齐的约定、流程与字段说明 |
| **场景案例** | **[`cases/`](../../cases/)** | 场景叙述、复现步骤、Docker 环境与效果 |

## 案例索引

| 目录 | 场景摘要 |
|------|----------|
| [`cases/centos7-feature-tour/`](../../cases/centos7-feature-tour/) | **CentOS 7** 静态 Node；TUI 特性导览（Mock / 真实 LLM） |
| [`cases/a2a-manage-docker/`](../../cases/a2a-manage-docker/) | **合规助手 + 运维助手** A2A 咨询（Agent Card + custom.md + TUI） |

新增案例时请更新 **[`cases/README.md`](../../cases/README.md)** 与本表。

## 历史说明

早期约定曾将案例放在 `docs/cases/` 正文目录；自 **v0.2.13** 起改为 **`cases/<name>/`**（文档 + Docker 同级），便于独立复现与 CI 集成。
