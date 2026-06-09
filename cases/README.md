# 落地案例（`cases/`）

可复现的场景实践：说明文档 + **Docker（CentOS 7）** 环境。

## 快速开始

```bash
cd cases/centos7-feature-tour
cp .env.example .env
docker compose up --build -d
./scripts/verify.sh
```

## 案例索引

| 目录 | 场景摘要 |
|------|----------|
| [`centos7-feature-tour/`](centos7-feature-tour/) | **CentOS 7** 上静态 Node；README 以 **TUI 步骤** 导览（Mock / 真实 LLM）；`verify.sh` 供 CI |

## 约定

- 运行时 **CentOS 7** + **`CGO_ENABLED=0`** 静态 `dagents-node`
- 构建 `context` 指向仓库根，`dockerfile` 在案例子目录
- 新增案例后更新本表与 [`docs/cases/README.md`](../docs/cases/README.md)

## 延伸阅读

- [Go Node 兼容矩阵](../docs/architecture/go-node-compatibility.md)
- [Agent Node API](../docs/architecture/agent-node-api.md)
