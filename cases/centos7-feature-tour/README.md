# 案例：CentOS 7 特性导览

在 **CentOS 7（glibc 2.17）** 容器中运行 DAgents Node，用 **TUI** 逐项体验本地助手能力。默认 **`llm.mock=true`**，无需 API Key 即可演示 Mock 对话、Context、Skills 等。

> Docker 提供 **老企业 Linux 用户态**；`uname -r` 为宿主机内核。本案例侧重 **用户在 TUI 里输入什么、看到什么**。

---

## 快速开始

**1. 启动 Node（CentOS 7 容器）**

```bash
cd cases/centos7-feature-tour
cp .env.example .env
docker compose up --build -d
```

**2. 配置 Client 连接本案例 Node**

将 `client-config.snippet.yaml` 中的 `local.endpoint` 合并进 [`packaging/agent-client/config.yaml`](../../packaging/agent-client/config.yaml)：

```yaml
local:
  endpoint: http://127.0.0.1:18765
```

**3. 打开 TUI**

```bash
# Go Client（推荐）
go run ./client/cmd/dagents-client tui

# 或 Python Textual TUI
dagents chat
```

启动后应无连接错误；输入 **`/status`** 可确认已连上 `case-write-skill`。

停止环境：`docker compose down`

---

## TUI 斜杠命令速查

| 命令 | 用途 |
|------|------|
| `/status` | agent_id、session_id、队列深度 |
| `/context` | system_prompt、token、loaded_skills（Esc 返回） |
| `/skill` | 列出 skills |
| `/skill load NAME` | 加载 skill 正文 |
| `/skill unload NAME` | 卸载 skill 正文 |
| `/sessions` | 列出历史 session |
| `/switch <id>` | 切换到已有 session |
| `/new` | 新建 session |
| `/compress` | 手动触发上下文压缩 |
| `/tools verbose\|brief` | 工具输出展开/折叠 |
| `/reasoning on\|off` | 切换推理流显示 |
| `/quit` | 退出 |

---

## 特性导览（Mock 模式，无需 API Key）

以下步骤在 **`llm.mock=true`**（默认）下即可在 TUI 中完成。

### 1. 老 OS 上跑 Node

**说明**  
Release 用 **`CGO_ENABLED=0`** 静态编译 `dagents-node`，适合 RHEL 7 量级老系统。本案例在 CentOS 7 用户态容器中运行 Node。

**TUI 如何触发**  
完成「快速开始」后直接进入 TUI，无需额外输入。

**TUI 如何观察**  
- TUI 正常启动、无 `connection refused`  
- 输入 **`/status`**：显示 `agent_id` 为 `case-write-skill`，`session_id` 已分配  

> 若要确认容器 OS / glibc / 静态二进制，见文末 [附录：环境与自动化脚本](#附录环境与自动化脚本)。

---

### 2. Mock 对话（Turn 队列）

**说明**  
Mock 模式下 LLM **回显**用户消息，仍走完整入队 → turn loop → SSE 路径，与生产一致。

**TUI 如何触发**  
在输入框发送普通消息（非斜杠命令）：

```text
feature tour ping
```

**TUI 如何观察**  
- Transcript 先后出现 **user** 与 **assistant**，内容均含 `feature tour ping`  
- **`/status`**：队列深度先升后降，turn 结束后回到 idle  
- 再发一条不同内容，确认每轮都能 echo  

---

### 3. Context 可观测

**说明**  
`/context` 展示与 Node 一致的 **system_prompt**、**token 粗算**、**loaded_skills**、最近消息摘要等。

**TUI 如何触发**  
输入：

```text
/context
```

**TUI 如何观察**  
- 出现 **system_prompt** 区块（含 FS_ROOT 说明、skills 目录元数据等）  
- **messages_total_tokens** / **messages_count** 有数值  
- **loaded_skills** 初始为空或尚未 load  
- 在 system_prompt 中能看到 **`write-skill`** 的 **name / description**（目录元数据），**不含** skill 正文  
- 按 **Esc**（full 模式）返回对话界面  

---

### 4. Skills 加载 / 卸载

**说明**  
Skills 分两层：**目录元数据**始终在 system prompt；**正文**仅在 load 后注入。

**TUI 如何触发**

```text
/skill
/skill load write-skill
/context
/skill unload write-skill
/context
```

**TUI 如何观察**  
- **`/skill`**：列出可用 skill，含 `write-skill`  
- load 后 **`/context`**：`loaded_skills` 含 `write-skill`，**system_prompt** 出现 **`### write-skill`** 及正文段落  
- unload 后再 **`/context`**：正文段消失，`loaded_skills` 不再含该项  

---

### 5. Session 持久化

**说明**  
每个 TUI 对话对应一个 **session**；消息写入 **`workspace/data/`**（SQLite）。重启 Node 后仍可恢复。

**TUI 如何触发**

```text
feature tour session test
/sessions
```

记下当前 session id（`*` 标记为当前），然后另开终端：

```bash
docker compose restart dagents-node
```

回到 TUI，输入：

```text
/sessions
/switch <上一步记下的 session_id>
```

**TUI 如何观察**  
- restart 前发送的 `feature tour session test` 仍在 transcript 中  
- **`/sessions`** 列表在 restart 后仍含该 session  
- **`/switch`** 后历史消息完整恢复  

---

### 6. FS_ROOT 与工作区种子

**说明**  
文件工具仅允许访问 **`fs_root`**（本案例 `/workspace/.runtime`）。容器首次启动会写入 **`demo/hello.txt`**。Mock 模式**不会**自动调工具，此处主要确认工作区已就绪。

**TUI 如何触发**  
输入 **`/context`**，在 **system_prompt** 中查找 FS_ROOT / workspace 相关说明。

**TUI 如何观察**  
- system_prompt 指明可访问路径范围  
- 真实 LLM 模式下可用下一节「读取 demo/hello.txt」验证 **read_file**（Mock 下请跳过）

---

### 7. Policy 种子（HITL 配置基础）

**说明**  
首次启动将 policy 复制到 **`.runtime/policy/`**。其中 **`write_file=always`** 等规则是后续 HITL 审批的依据。TUI 不直接展示 policy 文件，但 turn 命中规则时会弹审批。

**TUI 如何触发**  
Mock 模式下无需操作；若要亲眼看到审批 UI，见下一节「真实 LLM」中的 HITL 项。

**TUI 如何观察**  
- Mock 模式下无 write 类工具调用，不会出现审批弹窗  
- 确认 policy 已就绪：见 [附录](#附录环境与自动化脚本) 中 `workspace/policy/tool.approval.txt`  

---

## 特性导览（真实 LLM，可选）

将 `.env` 设为 **`LLM_MOCK=false`** 并配置 **`OPENAI_API_KEY`**（或兼容端点），然后：

```bash
docker compose up -d --force-recreate
```

以下步骤需在 TUI 中发送**自然语言消息**（非斜杠命令）。

### 8. 工具循环（read_file）

**TUI 如何触发**

```text
请读取 demo/hello.txt，用一句话总结内容。
```

**TUI 如何观察**  
- Transcript 出现 **`read_file`** 工具行（可含 **call_purpose** 短标题）  
- 随后 tool 结果与 assistant 总结  
- **`/context`**：`tool_loop_count` 可能 > 0  
- 输入 **`/tools verbose`** 可展开工具输出细节  

---

### 9. HITL 工具审批

**TUI 如何触发**

```text
在 demo 目录下创建 notes.txt，内容为 ok。
```

**TUI 如何观察**  
- 出现 **审批** 界面，列出 `write_file`（或相关工具）  
- 选择 **Approve** 后 turn 续跑，文件落盘  
- 选择 **Reject** 则 tool 结果为 rejected，不会写入  

---

### 10. 手动压缩

**TUI 如何触发**  
先多轮对话拉长上下文，然后：

```text
/compress
```

**TUI 如何观察**  
- 压缩完成提示  
- 再输入 **`/context`**：`messages_count` 下降，history 中出现压缩摘要  
- 若 turn 仍在进行，可能提示 busy（稍后再试）  

---

### 11. Usage / Reasoning

**TUI 如何触发**  
发送较复杂问题；若模型支持 thinking，可先：

```text
/reasoning on
```

**TUI 如何观察**  
- 底栏或 transcript 出现 **token / usage** 统计  
- **`/reasoning on`** 时可见推理流（视模型与 Client 而定）  

---

### 12. Triggers（本案例默认关闭）

**说明**  
本案例 **`triggers.enabled: false`**。若改为 `true` 并重启 Node，可通过自然语言让 Agent 创建定时任务，或查阅 Node API 文档扩展。

---

## 附录：环境与自动化脚本

运维 / CI 可用脚本做等价检查（**非 TUI 操作**）：

```bash
./scripts/verify.sh    # 自动化冒烟（health、context、skills、mock turn 等）
./scripts/show-os.sh   # CentOS 7 / glibc 2.17 / static binary
```

| 路径 | 说明 |
|------|------|
| `scripts/verify.sh` | 特性自动化 |
| `scripts/show-os.sh` | OS / glibc / 二进制 |
| `workspace/` | 挂载为 `.runtime`（demo、policy、data） |
| `config.yaml` | 容器内 Node 配置 |
| `client-config.snippet.yaml` | Client `local.endpoint` 片段 |

---

## 延伸阅读

- [cases 总索引](../README.md)
- [Client 斜杠命令](../../client/README.md)
- [Agent Node API](../../docs/architecture/agent-node-api.md)
- [本地助手架构](../../docs/architecture/local-assistant.md)
- [Go Node 兼容矩阵](../../docs/architecture/go-node-compatibility.md)
