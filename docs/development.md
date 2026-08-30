# 开发与验证

本文是 DAgents 的开发者入口。当前发布基线为 v0.10.5；机器可读版本唯一来源是根目录 `VERSION`，变更说明以 `CHANGELOG.md` 为准。

## 1. 环境

| 组件 | 要求 |
|---|---|
| Go | 1.25+（Node/Client） |
| Node.js | 用于构建 Node Web UI 和 Manage Console |
| Python | 3.11+（Manage；建议命令使用 `python3`） |
| Docker | 仅在构建/运行 Manage 镜像时需要 |

Web UI 静态资源不提交 Git。首次构建 Node 或 Manage 前必须先构建对应前端。

## 2. 从源码启动

```bash
cp packaging/agent-client/config.example.yaml packaging/agent-client/config.yaml
npm run build --prefix node/webui/frontend
go run ./node/cmd/dagents-node -config packaging/agent-client/config.yaml
```

打开 `http://127.0.0.1:18765/ui/`。LLM、Agent、policy、skills 等运行时配置优先通过 Web UI 设置；无 API Key 的结构联调可启用 Mock LLM。

可选启动 Manage：

```bash
npm run build --prefix manage/console/frontend
python3 run_manage.py
```

Console 地址为 `http://127.0.0.1:8020/console/`。Node 侧须启用 Manage/Workgroup 配置；Node 通过出站 WS 连接 Manage，Manage 不主动访问 Node。

## 3. 测试矩阵

| 范围 | 命令 | 说明 |
|---|---|---|
| Node Web UI | `npm test --prefix node/webui/frontend -- --run` | Vitest |
| Node UI 构建 | `npm run build --prefix node/webui/frontend` | 生成 Go embed 静态资源 |
| Manage Console 构建 | `npm run build --prefix manage/console/frontend` | 生成 Console 静态资源 |
| Go | `go test ./shared/config/... ./shared/logfiles/... ./shared/update/... ./shared/workgroup/... ./node/... ./client/... ./desktop/tray/...` | 全部 Go 模块 |
| Go 静态检查 | `go vet ./shared/config/... ./shared/logfiles/... ./shared/update/... ./shared/workgroup/... ./node/... ./client/... ./desktop/tray/...` | 全部 Go 模块 |
| Python | `python3 -m unittest discover -s tests -p "test_*.py" -v` | 默认不发现 `tests/integration/` |
| Python 质量 | `python3 -m ruff check manage scripts tests && python3 -m pyright --project pyrightconfig.json` | 错误与类型门禁 |
| API/Fixture 契约 | `python3 scripts/ci/check_contracts.py` | OpenAPI 路由与 Workgroup Schema/fixture |
| 差异检查 | `git diff --check` | 提交前必须通过 |

快速运行全部门禁：`scripts/verify.sh`；Windows 使用 `powershell -ExecutionPolicy Bypass -File scripts/verify.ps1`。

涉及工作组、HITL、消息队列或恢复协议时，应同时运行对应 Go/Python 单测和浏览器清单。真实 LLM、真实远程终端、WSS、断线恢复属于环境验收，不能用 Mock 结果替代。

## 4. 代码导航和修改边界

先阅读 [架构](architecture.md)，再进入对应子系统。每个 Go 包的职责见包内 `README.md`，类型/函数/字段见 `REFERENCE.md`；不要把包级细节重复抄进总架构。

新增能力按以下顺序判断落点：

1. 需要改变模型请求或工具循环：`node/internal/turn/`，并补 Step/集成测试；
2. 需要改变消息来源、优先级或恢复：`node/internal/queue/` + `session/`；
3. 需要增加工具：Registry、Schema、policy/HITL、result contract 和 UI 展示一起设计；
4. 需要增加跨机协作：优先扩展现有 Node→Manage WS/Workgroup 契约，不新增 Manage→Node HTTP；
5. 需要增加模型可见输入：先定义 ContextInjection/source/provenance 和大小上限，再决定是否持久化；
6. 需要增加对外接口：同步 OpenAPI、API 文档、事件速查和回归清单。

## 5. 前端状态与事件

前端展示的运行态必须来自权威事件或持久化快照：

- 普通 Agent：SSE + hydrate；`turn_state` 是生命周期权威，`turn_finished`、`failed`、`cancelled` 等终态负责收敛临时状态；
- Workgroup：Timeline 是可恢复事实，`workgroup.realtime` 只承担临时流式状态；
- Terminal：WebSocket terminal snapshot/output 是终端权威来源；
- MCP/配置/skills：使用对应 revision/event 触发刷新，不根据 UI 轮询先后顺序猜测状态。

新增状态时要写清楚来源、生命周期、覆盖关系、断线后的恢复方式，并为旧事件或重复事件定义幂等行为。

## 6. 文档规则

文档分为四类：

| 类型 | 放置位置 | 规则 |
|---|---|---|
| 当前行为 | `docs/architecture.md`、`docs/user/`、`docs/reference/` | 不用日期命名；必须能由代码或契约验证 |
| 活跃设计 | `docs/design/` | 文首标注状态、范围、非目标和验证门槛 |
| 对标/实验 | `docs/comparative-analysis/` | 标明来源、检查日期、事实/推测和结论 |
| 历史材料 | `docs/archive/` | 只保留追溯价值；不得作为现行入口 |

不要在多个文件复制同一份协议正文；总览只讲边界，包 README 讲实现，Schema/OpenAPI 讲字段，CHANGELOG 讲版本变化。

## 7. 发布前检查

1. 更新根目录 `VERSION`，再由 `scripts/release/prepare_release.py` 同步包元数据和 `CHANGELOG.md`；
2. 构建两套前端并确认静态产物未被意外提交；
3. 运行 `scripts/verify.sh`（Windows 使用 `scripts/verify.ps1`）；
4. 对新增接口检查旧客户端、重连、重复消息和错误路径；
5. 真实 UI 测试至少覆盖对话、工具、HITL、取消、刷新恢复；工作组变更还要覆盖 Agent 注册、成员加入、任务分派、审批和断线；
6. PR 描述列出行为变化、数据迁移、验证命令和未覆盖环境。
