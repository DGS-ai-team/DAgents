# Manage A2A 合规咨询场景（Docker）

三容器栈：**Manage** + **合规助手 node-a** + **运维助手 node-b**。  
运维侧在 TUI 中与真实 LLM 对话；合规侧 **node-a** 通过 Manage **Inbox** 跑 LLM turn，将 **`result_text` 原样** 回写 Task（`completed`），由调用方自行解读。

| 容器 | Agent ID | 对外端口 |
|------|----------|----------|
| `manage` | — | 8020 |
| `node-a` | 合规助手 | 18765 |
| `node-b` | 运维执行助手 | 18766 |

---

## 1. 快速开始

### 1.1 准备 `.env`

在 **本目录** 操作（只读 `cases/a2a-manage-docker/.env`，不读仓库根 `.env`）：

```bash
cd cases/a2a-manage-docker
cp .env.example .env
```

编辑 `.env`，至少关注下表：

| 变量 | 作用 | 真实 LLM 联调建议 |
|------|------|-------------------|
| `LLM_MOCK` | `true` 时 Node 用 Mock；`false` 时走真实 API | 设为 **`false`** |
| `LLM_API_KEY` | 写入 node 容器，供 `api_key_env: LLM_API_KEY` 读取 | 填入有效密钥 |
| `LLM_API_BASE` | OpenAI 兼容 API 根地址 | 如 `https://api.deepseek.com` |
| `LLM_MODEL` | 模型名 | 账号可用的模型，如 `deepseek-chat` |
| `LLM_PROVIDER` | 厂商适配（`openai` / `deepseek`） | 与网关一致即可 |
| `MANAGE_PORT` / `NODE_*_PORT` | 宿主机映射端口 | 默认即可 |

> **说明**：`LLM_*` 注入 **node-a 与 node-b 两个容器**。真实联调时 **`LLM_MOCK=false`**：`node-b` TUI 对话与 **`node-a` A2A 合规裁决** 均走同一套 LLM API（完整 turn loop + 流式 SSE）。

### 1.2 启动 Docker（真实 LLM）

```bash
cd cases/a2a-manage-docker
docker compose up --build -d
```

确认 `.env` 中 `LLM_MOCK=false` 且 `LLM_API_KEY` 已填写后再启动。  
Manage 控制台（查看已注册 Node）：<http://127.0.0.1:8020/console/>

`docker compose up` 会在 Node 就绪后自动运行 **`init-groups`** 服务，为 **node-a / node-b** 分配默认分组 **`a2a-lab`**（可通过环境变量 `DISCOVERY_GROUP` 覆盖）。

停止环境：

```bash
docker compose down
```

### 1.3 discovery_group（A2A 必需）

Manage **不会**在 Node 注册时自动填 `discovery_group`；该字段由 Manage Console 或 API 分配。

| 能力 | 规则 |
|------|------|
| **`agent_discover`** | 按分组过滤；默认使用 config 中 `manage.registration.team`（本案例为 `a2a-lab`） |
| **`agent_invoke`** | caller 与 target **须至少共享一个** `discovery_group`，否则 Manage 返回 `403 discovery_group_mismatch` |

**Console 设置**：打开 <http://127.0.0.1:8020/console/> → 顶栏 **「discovery_group 分配」** 批量保存，或点 Node 行在抽屉内编辑。

**命令行**（与 `init-groups` / `verify.sh` 相同）：

```bash
./scripts/assign-discovery-groups.sh
# 或
DISCOVERY_GROUP=a2a-lab MANAGE_URL=http://127.0.0.1:8020 ./scripts/assign-discovery-groups.sh
```

若跳过 `init-groups` 或未分配分组，`agent_discover` 将返回空列表，`agent_invoke` 将被拒绝（即使 Agent Card 配置了 `compliance_peer: node-a`）。

### 1.4 修改 `.env` 后必须重建容器

`.env` 仅在容器 **创建 / 启动** 时通过 `env_file` 注入；**正在运行的容器不会自动读取** 你刚改好的 API Key 或 `LLM_MOCK`。

修改 `LLM_API_KEY`、`LLM_MOCK`、`LLM_MODEL` 等任意项后，在本目录执行：

```bash
docker compose up -d --force-recreate
```

仅改 `config/`、`Dockerfile` 或案例源文件时，才需要带 `--build`：

```bash
docker compose up --build -d
```

可选确认（node-b 示例）：

```bash
docker compose exec node-b sh -c 'echo LLM_MOCK=$LLM_MOCK; grep "mock:" /etc/dagents/config.yaml'
```

应看到 `LLM_MOCK=false` 且 config 中为 `mock: false`（真实 LLM 时）。

> `scripts/verify.sh` 供 **CI / 冒烟** 使用，手工联调不必运行。

---

## 2. 启动 TUI

在 **仓库根目录**、**容器外**执行，连接 **运维助手 node-b**：

```bash
go run ./client/cmd/dagents-client \
  --config cases/a2a-manage-docker/local-run/node-b.yaml \
  tui
```

注意：`--config` 必须写在子命令 **`tui` 之前**。

`local-run/node-b.yaml` 仅配置 Client 连 `http://127.0.0.1:18766`；LLM 请求在 **node-b 容器内** 发起，密钥来自本 case 的 `.env`。

可选：连合规助手查看角色设定（无 inbox 对话需求时可跳过）：

```bash
go run ./client/cmd/dagents-client \
  --config cases/a2a-manage-docker/local-run/node-a.yaml \
  tui
```

---

## 3. 发什么消息、预期什么结果

### 3.1 TUI · 运维助手（node-b，真实 LLM）

在 **node-b TUI** 中输入自然语言。`custom.md` 会进入 system prompt，助手应体现 **运维执行** 角色，并在涉及高风险操作时 **提醒须先向 node-a 做合规咨询**。

| 你在 TUI 输入（示例） | 预期助手行为 |
|------------------------|--------------|
| `我们打算今晚做生产发布，还没有变更单，可以先发版吗？` | 识别为高风险操作；**不建议直接执行**；提示须向合规助手发起 `【合规咨询】` 并等待 `APPROVED`；可引用变更单要求 |
| `要把含手机号的客户明细导出到境外 SaaS 做分析，没有 CHG，怎么办？` | 强调 PII + 出境风险；**不得**给出绕过合规的步骤；建议停止或升级值班长 |
| `脱敏日活统计要发给已备案 vendor，变更单 CHG-2026-0142，流程上还要注意什么？` | 说明须走 A2A 合规咨询；可列出变更单、脱敏范围等需写进咨询正文的信息 |

若出现 `401` / `Authentication Fails`，检查本目录 `.env` 的 `LLM_API_KEY` 与 `LLM_MODEL` 是否有效，并按 [§1.3](#13-修改-env-后必须重建容器) 重建容器。

> node-b 已接入 **`agent_invoke`** / **`agent_discover`** 工具：`agent_invoke` 向 **node-a** 发起合规咨询并等待 `result_text`（默认目标为 Agent Card `metadata.compliance_peer`）。**前提**：双方在 Manage 上已分配**相同**的 `discovery_group`（见 [§1.3](#13-discovery_groupa2a-必需)）。

### 3.2 合规咨询 Task · node-a LLM 回复（原样）

当 Manage 上存在 **node-b → node-a** 的咨询 Task 时，**node-a** Inbox 经 **LLM turn** 生成回复，并以 **`status=completed`**、`result_text=<模型原文>` 回写 Manage（**不**解析 APPROVED/DENIED，**不**置 `failed`）。调用方自行阅读 `result_text`。

| 咨询正文（示例） | 说明 |
|------------------|------|
| `【合规咨询】拟将脱敏后的日活统计…变更单 CHG-2026-0142…` | 典型「可放行」场景；模型可能回复含 R-ANON-01 |
| `【合规咨询】…含手机号、姓名的客户明细…境外 SaaS…无变更单` | 典型「应拒绝」场景；模型可能回复含 R-PII-01 |
| `【合规咨询】…生产环境发布…暂无变更单` | 典型「缺 CHG」场景；模型可能回复含 R-CHG-01 |

**前提**：`.env` 中 **`LLM_MOCK=false`** 且 node-a 已 [重建容器](#13-修改-env-后必须重建容器)。可在 Manage Console 或 `docker compose logs node-a` 查看 turn / 流式过程。

---

## 4. 容器内目录与关键文件

### 4.1 `manage` 容器

| 路径 | 说明 |
|------|------|
| `/app/manage/` | Manage 服务代码 |
| `/app/run_manage.py` | 进程入口 |
| `/data/manage.db` | Registry + A2A Task SQLite（volume `manage-data`） |

### 4.2 `node-a` / `node-b` 容器（结构相同，种子文件按角色不同）

| 路径 | 说明 |
|------|------|
| `/etc/dagents/config.yaml` | Node 配置（构建时从 `config/node-{a,b}.yaml` 打入） |
| `/etc/dagents/agent-card.json` | 注册 Manage 时上报的 **Agent Card** |
| `/etc/dagents/custom.md.seed` | 案例 `prompt_context` 种子；**每次启动**复制到 runtime |
| `/workspace/.runtime/` | **`fs_root`**（volume，运行时状态） |
| `/workspace/.runtime/prompt_context/custom.md` | 生效中的侧车指令（经 turn 注入 system prompt） |
| `/workspace/.runtime/skills/` | Skill 目录（首次从种子复制） |
| `/workspace/.runtime/policy/` | 工具审批策略 |
| `/opt/dagents/seed/skills/`、`/opt/dagents/seed/policy/` | 内置种子，仅首次填充 runtime |

启动流程见 `scripts/entrypoint-node.sh`：创建 runtime 子目录 → 种子 skills/policy → **覆盖写入** `custom.md` → 若 `LLM_MOCK=false` 则将 config 中 `mock: true` 改为 `false` → 启动 `dagents-node`。

### 4.3 仓库内案例源文件（修改后需 `docker compose up --build`）

| 路径 | 内容摘要 |
|------|----------|
| `prompt_context/node-a/custom.md` | 用户自定义提示词（规章、输出建议等）；经 turn 注入 system prompt |
| `prompt_context/node-b/custom.md` | 用户自定义提示词（运维门禁、A2A 咨询格式等） |
| `agent-card/node-a.json` | 名称「合规助手」；`metadata.role=compliance`；能力 `compliance_review` / `policy_lookup` |
| `agent-card/node-b.json` | 名称「运维执行助手」；`metadata.compliance_peer=node-a`；能力 `deployment` / `data_export` / `shell` |
| `config/node-a.yaml` | 开启 `manage.a2a` Inbox；LLM 字段支持 `${LLM_*}` 环境变量展开 |
| `config/node-b.yaml` | A2A Inbox 关闭（作调用方）；同样支持 `${LLM_*}` |
| `local-run/node-{a,b}.yaml` | **宿主机 Client/TUI** 连 `127.0.0.1` 映射端口（与容器内 `config/` 分离） |

---

## 5. node-a A2A 合规（真实 LLM turn）

**node-a** 收到 Manage Inbox Task 后，**不走硬编码规则**，而是与 TUI 相同地进入 **session turn loop**：

1. Inbox poller 拉取 Task → `ComplianceExecutor`（`node/internal/manage/compliance_executor.go`）。
2. **ack** 后调用 `session.RunInboxConsultation`：为每个 Task 创建/清空 `a2a-<task_id>` session，入队 user 消息（咨询正文）。
3. 经 **流式 LLM**（SSE `assistant` 增量 → `done`）与可选 **工具循环** 跑完 turn，聚合 assistant 全文。
4. 将 LLM 聚合文本 **原样** 写入 `result_text`，Task 状态恒为 **`completed`**（不做 APPROVED/DENIED 解析，不因 turn 异常置 `failed`）。

`prompt_context/node-a/custom.md` 进入 **system prompt**，建议模型按 `APPROVED | rule=R-xxxx | …` 格式作答，但 **Node 不校验**。

### 5.1 为何要真实 LLM

| 目的 | 说明 |
|------|------|
| **端到端** | 覆盖 Inbox sidecar → session 队列 → 流式编排 → Manage reply 全链路 |
| **暴露隐藏问题** | 流式分片、超时、HITL、工具误调用、模型自由表述等 |
| **职责仍分离** | node-b 运维对话；node-a 合规回复，均由 LLM + custom.md 驱动 |

### 5.2 联调注意

- **node-a / node-b 均需** `LLM_MOCK=false` 与有效 `LLM_API_KEY`（改 `.env` 后 [§1.3](#13-修改-env-后必须重建容器) 重建容器）。
- 查询 Task 时直接阅读 **`result_text`**；是否采纳由 node-b / 人工判断。

---

## 延伸阅读

- [A2A 经 Manage](../../docs/future/a2a-via-manage.md)
- [Cases 文档约定](../README.md#case-readme-写法)
