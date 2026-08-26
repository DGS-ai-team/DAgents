# 落地案例（`cases/`）

可复现的场景实践：说明文档 + Docker 环境。每个案例 README **面向手工联调**，`scripts/verify.sh` 仅作 CI / 冒烟，不作为用户主路径。

## Case README 写法

每个 `cases/<name>/README.md` 建议按以下顺序组织（**不要**在正文给 `curl` 探针示例）：

1. **场景简介** — 一两段说明角色、容器/组件与要验证的能力  
2. **快速开始** — `cp .env.example .env`、**.env 各字段含义**、**真实 LLM 时如何设 `LLM_MOCK=false`**、`docker compose up --build -d`；**修改 `.env` 后须 `docker compose up -d --force-recreate`**（**不**写「先跑 verify.sh」）  
3. **打开 Web UI** — 容器外访问地址、端口与连接的 Node
4. **发什么消息、预期什么结果** — 用表格列 Web UI 操作与预期行为/输出
5. **容器内目录与关键文件** — 各容器路径、`custom.md` / Agent Card / config 等源文件摘要  
6. **延伸阅读** — 架构文档链接；`verify.sh` 可在附录一笔带过

## 案例索引

| 目录 | 场景摘要 |
|------|----------|
| [`centos7-feature-tour/`](centos7-feature-tour/) | **CentOS 7** 静态 Node；Web UI 特性导览（Mock / 真实 LLM） |

## 通用约定

- 构建 `context` 指向仓库根，`dockerfile` 在案例子目录  
- 环境变量优先读 **`cases/<name>/.env`**（各案 `.env.example` 为准）  
- 新增案例后更新本表与 [`docs/cases/README.md`](../docs/cases/README.md)

## 延伸阅读

- [Go Node 兼容矩阵（历史）](../docs/archive/architecture-go-node-compatibility.md)
- [Agent Node API](../docs/architecture/agent-node-api.md)
