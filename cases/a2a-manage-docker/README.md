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
Manage 控制台（查看已注册 Node、分配 `discovery_group`；**不含** Node 远程 session 代理）：<http://127.0.0.1:8020/console/>

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

若跳过 `init-groups` 或未分配分组，`agent_discover` 将返回空列表，`agent_invoke` 将被拒绝。

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

若出现 `401` / `Authentication Fails`，检查本目录 `.env` 的 `LLM_API_KEY` 与 `LLM_MODEL` 是否有效，并按 [§1.4](#14-修改-env-后必须重建容器) 重建容器。

> node-b 已接入 **`agent_invoke`** / **`agent_discover`** 工具：须先发现对端，再向目标 Agent（如 **node-a**）发起合规咨询并等待 `result_text`（**必须**传入 `to_agent_id`）。**前提**：双方在 Manage 上已分配**相同**的 `discovery_group`（见 [§1.3](#13-discovery_groupa2a-必需)）。

### 3.2 合规咨询 Task · node-a LLM 回复（原样）

当 Manage 上存在 **node-b → node-a** 的咨询 Task 时，**node-a** Inbox 经 **LLM turn** 生成回复，并以 **`status=completed`**、`result_text=<模型原文>` 回写 Manage（**不**解析 APPROVED/DENIED，**不**置 `failed`）。调用方自行阅读 `result_text`。

| 咨询正文（示例） | 说明 |
|------------------|------|
| `【合规咨询】拟将脱敏后的日活统计…变更单 CHG-2026-0142…` | 典型「可放行」场景；模型可能回复含 R-ANON-01 |
| `【合规咨询】…含手机号、姓名的客户明细…境外 SaaS…无变更单` | 典型「应拒绝」场景；模型可能回复含 R-PII-01 |
| `【合规咨询】…生产环境发布…暂无变更单` | 典型「缺 CHG」场景；模型可能回复含 R-CHG-01 |

**前提**：`.env` 中 **`LLM_MOCK=false`** 且 node-a 已 [重建容器](#14-修改-env-后必须重建容器)。可在 Manage Console 或 `docker compose logs node-a` 查看 turn / 流式过程。

### 3.3 A2A · `bash_run` 审批中继（node-b → node-a 查时间）

本场景验证 **跨 Agent HITL 中继**：node-a 启用 `bash_run=always`，调用方在 **node-b TUI** 审批 callee 的工具调用。

| 项 | 说明 |
|----|------|
| **node-a 策略** | `policy/node-a/tool.approval.txt` 中 `bash_run=always`；**每次启动**覆盖写入 runtime（见 entrypoint） |
| **node-a 行为** | `custom.md` R-TIME-01：收到查时咨询时 **必须** `bash_run` 执行 `date` |
| **node-b 行为** | `custom.md` 指引用 `agent_invoke` 向 node-a 发起查时咨询 |

**手工步骤**（[§2](#2-启动-tui) 已连 node-b TUI，`LLM_MOCK=false`，`discovery_group` 已分配）：

1. 在 **node-b TUI** 输入，例如：  
   `请向合规助手 node-a 查询当前系统时间，并把结果告诉我`
2. **预期**：node-b 调用 `agent_invoke` → Manage 投递 Task → node-a Inbox turn → node-a 拟调用 `bash_run`（`date`）。
3. **node-b TUI** 出现 **A2A 审批中继**（relay 来自 node-a 的 `requires_input`）；在 **调用方** 确认 `bash_run`。
4. 批准后 node-a 完成命令，Task `completed`，node-b 收到含 `date` 输出的 `result_text` 并转述给你。

排障：`docker compose exec node-a grep bash_run /workspace/.runtime/policy/tool.approval.txt` 应为 `bash_run=always`；Task 卡在 `delivered` 且日志 `(no handler)` 见 [§1.3](#13-discovery_groupa2a-必需) 与 Agent Card `metadata.role=compliance`。

**自动化验证（不经 TUI）**：栈启动后执行：

```bash
bash scripts/verify-bash-hitl.sh
```

脚本经 Manage API 模拟 `caller_notify` + `caller_resume`，并轮询 node-a session 是否写入 `bash` tool 结果、Task 是否 `completed`。

> `scripts/verify.sh` **不包含** TUI 人工审批场景；`verify-bash-hitl.sh` 覆盖查时 + bash HITL 中继。改 `policy/` 或 Node 代码后执行 `docker compose up --build -d`。

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
| `/workspace/agent-card.json` | 注册 Manage 时上报的 **Agent Card**（固定路径，相对 Node 工作目录） |
| `/etc/dagents/custom.md.seed` | 案例 `prompt_context` 种子；**每次启动**复制到 runtime |
| `/workspace/.runtime/` | **`fs_root`**（volume，运行时状态） |
| `/workspace/.runtime/prompt_context/custom.md` | 生效中的侧车指令（经 turn 注入 system prompt） |
| `/workspace/.runtime/skills/` | Skill 目录（首次从种子复制） |
| `/workspace/.runtime/policy/` | 工具审批策略 |
| `/opt/dagents/seed/skills/`、`/opt/dagents/seed/policy/` | 内置种子，仅首次填充 runtime |
| `/opt/dagents/case-policy-root/node-{a,b}/` | 案例角色 policy；存在 `tool.approval.txt` 时 **每次启动覆盖** runtime policy |

启动流程见 `scripts/entrypoint-node.sh`：创建 runtime 子目录 → 种子 skills/policy → **覆盖** case 角色 policy（如 node-a `bash_run=always`）→ **覆盖写入** `custom.md` → 若 `LLM_MOCK=false` 则将 config 中 `mock: true` 改为 `false` → 启动 `dagents-node`。

### 4.3 仓库内案例源文件（修改后需 `docker compose up --build`）

| 路径 | 内容摘要 |
|------|----------|
| `prompt_context/node-a/custom.md` | 用户自定义提示词（规章、R-TIME-01 查时须 `bash_run` 等）；经 turn 注入 system prompt |
| `prompt_context/node-b/custom.md` | 用户自定义提示词（运维门禁、A2A 查时/HITL 中继指引等） |
| `policy/node-a/tool.approval.txt` | node-a **`bash_run=always`**（A2A HITL 联调） |
| `agent-card/node-a.json` | 名称「合规助手」；`metadata.role=compliance`；能力 `compliance_review` / `policy_lookup` |
| `agent-card/node-b.json` | 名称「运维执行助手」；`metadata.role=ops`；能力 `deployment` / `data_export` / `shell` |
| `config/node-a.yaml` | 开启 `manage.a2a` Inbox；`registration` 仅含 `base_url` / `team` 等（**无** `agent_card_path`）；LLM 支持 `${LLM_*}` |
| `config/node-b.yaml` | A2A Inbox 关闭（作调用方）；同样无 `agent_card_path` |
| `local-run/node-{a,b}.yaml` | **宿主机 Client/TUI** 连 `127.0.0.1` 映射端口；本地联调 Node 时工作目录须含 `agent-card.json`（见 `scripts/start-local.sh`） |

---

## 5. node-a A2A 合规（真实 LLM turn + HITL 中继）

**node-a** 收到 Manage Inbox Task 后，**不走硬编码规则**，而是与 TUI 相同地进入 **session turn loop**：

1. Inbox poller 拉取 Task → `ComplianceExecutor`（`node/internal/manage/compliance_executor.go`）。
2. **ack** 后调用 `session.RunInboxTurn`：为每个 Task 创建/清空 `a2a-<task_id>` session，入队 user 消息（咨询正文）；若带 **resume** 则续跑 HITL。
3. 经 **流式 LLM**、**工具循环** 与可选 **HITL**（`bash_run=always` 等）跑完一步；若需 caller 审批则 `reply(requires_input)`，否则聚合 assistant 全文并 `reply(completed)`。
4. **caller** 在本地 TUI 完成中继 HITL 后，callee 经 `GET .../caller_input` 取 resume，再次 `RunInboxTurn` 直至 `completed`。

`prompt_context/node-a/custom.md` 进入 **system prompt**，建议模型按 `APPROVED | rule=R-xxxx | …` 格式作答；**Node 不强制解析** APPROVED/DENIED 字符串。

### 5.1 为何要真实 LLM

| 目的 | 说明 |
|------|------|
| **端到端** | 覆盖 Inbox sidecar → session 队列 → 流式编排 → Manage reply 全链路 |
| **暴露隐藏问题** | 流式分片、超时、HITL、工具误调用、模型自由表述等 |
| **职责仍分离** | node-b 运维对话；node-a 合规回复，均由 LLM + custom.md 驱动 |

### 5.2 联调注意

- **node-a / node-b 均需** `LLM_MOCK=false` 与有效 `LLM_API_KEY`（改 `.env` 后 [§1.4](#14-修改-env-后必须重建容器) 重建容器）。
- 查询 Task 时直接阅读 **`result_text`**；是否采纳由 node-b / 人工判断。
- **HITL 自动化**：`bash scripts/verify-bash-hitl.sh`（见 [§3.1](#31-a2a-查时--bash_run-hitl-联调)）。
- **单测**：`go test ./node/internal/session/... ./node/internal/manage/... ./node/internal/a2aclient/...`；`pytest tests/test_cli_a2a_relay.py tests/test_manage_m2_a2a.py`。

---

## 延伸阅读

- [Manage 通信架构](../../docs/manage-communication.md)
- [A2A 经 Manage](../../docs/future/a2a-via-manage.md)
- [Cases 文档约定](../README.md#case-readme-写法)
