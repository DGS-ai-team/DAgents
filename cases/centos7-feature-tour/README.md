# CentOS 7 特性导览（Docker）

在 **CentOS 7（glibc 2.17）** 用户态容器中运行 **静态编译** 的 `dagents-node`，通过 Node 内嵌的 **Web UI** 体验 Mock 对话、Context、Skills、Session 持久化等本地助手能力；可选切换 **真实 LLM** 验证工具循环与 HITL 审批。

> Docker 提供老企业 Linux **用户态**；`uname -r` 仍为宿主机内核。Release 二进制为 **`CGO_ENABLED=0`**，适合 RHEL 7 量级环境。

| 组件 | Agent ID | 对外端口 |
|------|----------|----------|
| `dagents-node` | `case-write-skill` | 18765（`DAGENTS_PORT` 可改） |

---

## 1. 快速开始

### 1.1 准备 `.env`

在 **本目录** 操作（只读 `cases/centos7-feature-tour/.env`）：

```bash
cd cases/centos7-feature-tour
cp .env.example .env
```

编辑 `.env`：

| 变量 | 作用 | Mock 导览（默认） | 真实 LLM 联调 |
|------|------|-------------------|---------------|
| `LLM_MOCK` | `true` 时回显用户消息；`false` 时调用 API | **`true`** | **`false`** |
| `OPENAI_API_KEY` | 写入容器，对应 `config.yaml` 的 `api_key_env` | 留空 | 填入有效密钥 |
| `DAGENTS_PORT` | 宿主机映射端口 | 默认 `18765` | 默认即可 |

Mock 模式下 **无需** API Key 即可完成大部分导览。

### 1.2 启动 Docker

```bash
cd cases/centos7-feature-tour
docker compose up --build -d
```

真实 LLM：在 `.env` 设 **`LLM_MOCK=false`** 并填写 **`OPENAI_API_KEY`** 后再启动（或改完后 `docker compose up -d --force-recreate`）。

停止环境：

```bash
docker compose down
```

### 1.3 修改 `.env` 后必须重建容器

`.env` 仅在容器 **创建 / 启动** 时注入；修改 `OPENAI_API_KEY` 或 `LLM_MOCK` 后，在本目录执行：

```bash
docker compose up -d --force-recreate
```

> `scripts/verify.sh`、`scripts/show-os.sh` 供 **CI / 运维** 使用，手工联调不必运行。

---

## 2. 打开 Web UI

容器启动后，在宿主机浏览器打开：

```text
http://127.0.0.1:18765/ui/
```

进入页面后确认连接的 Agent ID 为 **`case-write-skill`**，即可在任务区发送消息并观察 Context、Skills、Session 与工具调用。

### 访问说明

| 项目 | 说明 |
|------|------|
| 地址 | `http://127.0.0.1:18765/ui/`；修改 `DAGENTS_PORT` 后替换端口 |
| 连接检查 | 页面可加载，Agent ID 为 **`case-write-skill`** |

---

## 3. 发什么消息、预期什么结果

### 3.1 Mock 模式（`LLM_MOCK=true`，默认）

| 操作 | Web UI 操作 | 预期结果 |
|------|---------------|----------|
| 连通性 | 打开 `http://127.0.0.1:18765/ui/` | 页面可加载，Agent ID 为 **`case-write-skill`** |
| Mock 对话 | 发送 `feature tour ping` | 任务卡片中出现 user / assistant，内容均含该句 |
| Context / Skills | 使用页面中的 Context、Skills 面板 | 可查看 system prompt 与已加载的 **`write-skill`** 元数据；按页面操作加载或卸载 skill |
| Session 持久化 | 发送 `feature tour session test`，重启 `dagents-node` 后从 Session 列表重新打开 | 历史消息仍在任务卡片中 |
| 工作区说明 | 在 Web UI 中询问工作区范围 | 可见 **FS_ROOT** / workspace 说明；`demo/hello.txt` 已在容器内就绪（Mock 不自动调工具） |

### 3.2 真实 LLM（`LLM_MOCK=false` + 有效 `OPENAI_API_KEY`）

容器内默认 **`provider: deepseek`**、`base_url: https://api.deepseek.com`、`model: deepseek-chat`（见 `config.yaml`）。密钥通过 **`OPENAI_API_KEY`** 环境变量注入（历史命名，兼容 OpenAI 格式网关）。

| Web UI 操作 | 预期结果 |
|---------------|----------|
| 发送“请读取 `demo/hello.txt`，用一句话总结内容。” | 出现 **`read_file`** 工具调用与结果；assistant 给出摘要 |
| 发送“在 demo 目录下创建 `notes.txt`，内容为 ok。” | 首次 **`write_file`** 弹出 **HITL 审批**（默认 `write_file=rule`）；Approve 后文件落盘，Reject 则不写入；详见[内置工具参考](../../docs/handbook/附录/内置工具参考.md) |
| 在页面中查看 Context / usage | 可查看消息数量、token 或 usage 等运行信息（具体字段随 UI 版本变化） |

若出现 **401 / Authentication Fails**，检查本目录 `.env` 的 `OPENAI_API_KEY`，并按 [§1.3](#13-修改-env-后必须重建容器) 重建容器。

### 3.3 本案例未启用的能力

| 项 | 说明 |
|----|------|
| **Triggers** | `config.yaml` 中 **`triggers.enabled: false`**，本案例不演示定时触发 |
| **Manage / A2A** | 未接入 Manage；单 Node 本地助手导览 |
| **Agent Card** | 未启用 Manage 注册；无需 `agent-card.json` |

---

## 4. 容器内目录与关键文件

### 4.1 `dagents-node` 容器

| 路径 | 说明 |
|------|------|
| `/etc/dagents/config.yaml` | Node 配置（构建时从案例 `config.yaml` 打入） |
| `/usr/local/bin/dagents-node` | **CGO_ENABLED=0** 静态二进制 |
| `/workspace/.runtime/` | **`fs_root`**（volume 挂载到宿主机 `./workspace/`） |
| `/workspace/.runtime/skills/` | Skill 目录（首次从种子复制，含 **`write-skill`**） |
| `/workspace/.runtime/policy/` | 工具审批策略（种子含 **`write_file=rule`** + Agent 自有文件信任链；显式 **`write_file=always`** 可关闭信任链） |
| `/workspace/.runtime/demo/hello.txt` | 演示只读文件（entrypoint 首次创建） |
| `/workspace/.runtime/data/` | Session SQLite 等持久化数据 |
| `/opt/dagents/seed/skills/`、`/opt/dagents/seed/policy/` | 内置种子 |

启动流程见 `scripts/entrypoint.sh`：创建 runtime 子目录 → 种子 skills/policy → 写入 **demo/hello.txt** → 若 `LLM_MOCK=false` 则将 config 中 `mock: true` 改为 `false` → 启动 Node。

### 4.2 仓库内案例源文件

| 路径 | 内容摘要 |
|------|----------|
| `config.yaml` | `agent_id: case-write-skill`；`fs_root: /workspace/.runtime`；skills 开启；**`llm.mock: true`**（entrypoint 可覆盖）；DeepSeek 兼容端点 |
| `local-run/client.yaml` | 可选的宿主机 Client 运维命令配置，连接 `http://127.0.0.1:18765` |
| `workspace/` | 挂载为容器 `.runtime`；本地可查看 demo、policy、session 数据 |

---

## 延伸阅读

- [Cases 文档约定](../README.md#case-readme-写法)
- [内置工具参考](../../docs/handbook/附录/内置工具参考.md)
- [Client 运维命令](../../client/README.md)
- [Agent Node API](../../docs/architecture/agent-node-api.md)
- [本地助手架构](../../docs/architecture/local-assistant.md)
- [Go Node 兼容矩阵](../../docs/architecture/go-node-compatibility.md)
