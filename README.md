# DAgents

**本仓库为 DAgents 后端**：多 Agent / 工具调用场景下的 Python 运行时，提供 FastAPI 服务、注册中心（Register Center）与相关设计文档。  
**协议**：[MIT License](LICENSE)。**Web 前端**已独立，见 [DAgentsUI](https://github.com/DGS-ai-team/DAgentsUI)。

**当前版本：`v0.1.0`（2026-05-12）** · [变更记录](CHANGELOG.md)

- 会话化 Agent 运行时（工具调用、审批、流式输出）
- HTTP API、SQLite 可选持久化、Prometheus 指标（`/metrics`）
- 可独立部署的注册中心 **`register_center/`**（Agent 登记、发现、分组广播与中继）

> **0.x 预览**：在向 **1.0** 演进前，仍可能出现不兼容的 HTTP/OpenAPI 或配置项调整；**会写入 [CHANGELOG.md](CHANGELOG.md)**，并在 **GitHub Releases**（与本仓库 tag 对齐）中便于查阅。小版本内尽量保持行为可预期。

## 版本与兼容性

- **标记版本**：`v0.1.0`（与 Git tag **`v0.1.0`** 一致；预编译包请以 **Releases** 资产为准）。
- **Python**：**3.11+** 可尝试运行；**CI 与发布流水线在 Python 3.13 上验证**，生产环境建议与 CI 主版本对齐。若部署在 **较旧 glibc 的 Linux** 或需对照官方「最低 Windows」表述，见 **[doc/os-compatibility.md](doc/os-compatibility.md)**。
- **发布物**：源码即本仓库；二进制见 **`packaging/`**、**`startup_scripts/`** 与 **`.github/workflows/build-and-release.yml`**；发版时请同步更新 **Releases** 说明与 **CHANGELOG**。

## v0.1.0 范围与说明

- **已交付能力**：见下文「功能概览」及 [CHANGELOG.md](CHANGELOG.md) 中 **0.1.0** 条目（API/持久化/流式/指标/Register Center 等）。
- **单测**：默认 `python -m unittest discover -s tests -p "test_*.py" -v` **不**拉取实时 LLM；**`test_agent_service.py`** 在缺少完整依赖（如未 `pip install -r requirements.txt`）时部分用例会 **skip**。规划与覆盖矩阵见 **`tests/UNIT_TEST_CHECKLIST.md`**，运行说明见 **`tests/README.md`**。可选真机 LLM 冒烟见 **`tests/integration/README.md`**（`RUN_LIVE_LLM_TESTS` + `LLM_API_KEY`）。
- **前端联调**：OpenAPI 与 **DAgentsUI** 的最低兼容组合以前端仓库说明为准；后端导出命令见「与前端（DAgentsUI）对接」。

## 问题反馈与安全

- **问题与功能请求**：请通过本仓库 **GitHub Issues** 反馈（建议注明 **`v0.1.0`**、Python 版本、操作系统与最小复现步骤）。
- **安全漏洞**：请勿在公开 Issue 中张贴可利用细节；请按根目录 **[SECURITY.md](SECURITY.md)** 通过 **GitHub Security Advisories**（或其中约定的私密渠道）报告。

## 功能概览

- **消息编排**：按 `session` 串行处理，支持 `human_message`、`tool_result`、`resume` 等。
- **工具调用**：OpenAI tool calling、审批执行、异步工具结果回灌。
- **上下文压缩**：静默 / 阻塞双阈值压缩，降低长会话成本。
- **流式输出**：SSE 返回 assistant / reasoning / usage / tool 等事件。
- **持久化**：可选 SQLite 会话存储，重启后可恢复上下文。
- **多 Agent 协作**：通过配置连接 Register Center，支持发现、广播与点对点投递（见 `app/harness/tools/`）。

## 项目结构（根目录）

```text
DAgents/
├── LICENSE                      # MIT
├── CHANGELOG.md                 # 版本变更记录（与 tag / Releases 对齐）
├── requirements.txt
├── .env.example                 # 环境变量模板（复制为 .env）
├── run_agent.py                 # 仓库内脚本入口（能力未定，本文档不展开）
├── run_agent_api.py             # FastAPI（Agent API）
├── run_register_center.py       # Register Center
├── run_dev_stack.py             # 本地同时拉起 API + Register Center
├── export_openapi_schema.py     # 导出 OpenAPI（前后端契约）
├── app/                         # 核心代码（config / core / harness / …）
├── register_center/             # Register Center 实现（FastAPI + 内存表）
├── tests/                       # 单元测试（`unittest`）
├── startup_scripts/             # 预编译包用：`start.sh`、`start_register_center.*`
├── scripts/                     # 辅助脚本（含 CI 构建）
├── packaging/                   # 离线安装说明、**`runtime/`**（预编译包 **`.runtime`** 占位：空侧车 **prompt_context**、**scripts**、**data** 等）
├── doc/                         # 对外技术文档（含 **`roadmap.md`**、`cases/` 等；索引见 **`doc/README.md`**）
└── .github/workflows/           # CI（单测、PyInstaller 打包）
```

## 环境要求（概要）

| 安装方式 | Python | 其它 | 典型场景 |
|----------|--------|------|----------|
| **源码 + 在线 pip** | **3.11+**（**CI / 发布验证：3.13**） | pip、可访问 PyPI | 开发 / 有外网服务器 |
| **源码 + 离线 wheels** | 与下载 wheel 时 **次版本一致** | wheel 与 OS/架构绑定 | 内网、隔离环境 |
| **PyInstaller 发布包** | **无需** Python | Linux 上**实际**受 **构建环境 glibc** 约束（非「Python 3.11 官方一句话」）；见 **[doc/os-compatibility.md](doc/os-compatibility.md)** | 解压即用 |

离线 wheel 说明见 **`packaging/OFFLINE_INSTALL.md`**。

## 安装与运行

### A）源码 + 在线 pip（推荐开发）

```bash
python -m venv .venv
source .venv/bin/activate   # Windows: .venv\Scripts\activate
pip install -r requirements.txt   # 与 CI 一致；缺依赖时部分单测会 skip，见「开发说明」
cp .env.example .env
python run_agent_api.py
```

常用配置见 **`app/config/settings.py`**；监听地址可通过 **`API_HOST`** / **`API_PORT`**（默认 **`127.0.0.1:8000`**）等在 `.env` 中设置。

启动 **`run_agent_api.py`** 时会先采集 **`HostSnapshot`**（系统类型、登录名、有效 UID/GID 等）并写一条 **`logging` INFO**，后续 **`bash_run`**、启动提示、**`get_system_prompt`** 等均 **`get_host_snapshot()`** 读取缓存，避免重复探针。

### B）源码 + 离线依赖

在可联网且与目标机 Python 次版本 / OS / 架构一致的环境执行 `pip download -r requirements.txt`，将源码与 `wheels/` 拷至离线机后 `pip install --no-index --find-links=wheels -r requirements.txt`。步骤细节见 **`packaging/OFFLINE_INSTALL.md`**。

### C）预编译二进制（PyInstaller）

无需 Python；Linux 注意 glibc 兼容性。发版二进制建议从 **GitHub Releases** 获取 **`v0.1.0`** 对应资产（或与 tag 对齐的自建产物）。解压后配置 `.env`，运行：

- **`dagents-api`** / **`dagents-api.exe`**：Agent API  
- **`dagents_register_center`** / **`dagents_register_center.exe`**：Register Center  

可选用 **`startup_scripts/linux/`**、**`startup_scripts/windows/`** 下的启动脚本。

### 常用启动命令（源码）

| 用途 | 命令 |
|------|------|
| Agent API | `python run_agent_api.py` |
| Register Center | `python run_register_center.py`（默认约 **`0.0.0.0:8010`**，见 `.env` / `REGISTER_CENTER_*`） |
| 本地联调（API + Register Center） | `python run_dev_stack.py` |

### Linux：跨用户 shell 与 sudoers（可选）

Agent API 会通过 **`bash_run`** 等工具用 **`subprocess`** 执行 shell。若要在自动化场景下 **切换到其他 Unix 用户**（例如 **`su - <user> -c '...'`**），在 Linux 上常见痛点是：

- **`su`** 往往依赖 **密码** 或前置特权（如 pam_wheel），服务进程 **无交互终端**，密码难以注入；
- 因此 **非 root** 启动时，`su -` 一类命令容易阻塞或失败。

**运行时拦截**：**`bash_run`** 在 **非 Windows** 且进程 **非 root**、且实际 shell 为 **bash** 时：若命令切段后任一片段行首为 **`su - <user> -c ...`**（含 **`-l` / `--login`**），或行首为 **`sudo`/`sudoedit`** 且片段内 **没有** **`-n` / `--non-interactive`**，将 **直接返回 `ERROR:`**，不启动子进程。已配置 **sudoers NOPASSWD** 时，请使用 **`sudo -n`** / **`sudo --non-interactive`**，以便在无 TTY 时快速失败而非阻塞读密码。

**`python run_agent_api.py`** 在 **Linux** 下会根据当前有效 UID 打印启动提示（**stderr**，并写入 **`logging`**，级别为非 root 时 WARNING、为 root 时 INFO）：非 root 时提醒评估 **sudoers**；为 root 时提醒特权较高、建议仍优先使用低权限账户运行。

实践中更推荐 **不用交互式 `su`**，改用 **`sudo -u <user> -- <command>`**，并为「运行 API 的 Unix 用户」（下称 **`dagentsrunner`**，请换成你的真实用户名）配置 **最小权限** 免密规则。

#### sudoers 配置教程（概要）

1. **编辑 sudoers（必须使用安全编辑器）**  
   - `sudo visudo`  
   - 或单独文件：`sudo visudo -f /etc/sudoers.d/dagents`（便于撤销）。

2. **示例：允许 `dagentsrunner` 仅以用户 `deploy` 身份免密执行命令**

   ```text
   dagentsrunner ALL=(deploy) NOPASSWD: ALL
   ```

   生产环境应收紧为 **命令白名单**（路径一律用绝对路径），例如：

   ```text
   dagentsrunner ALL=(deploy) NOPASSWD: /usr/bin/git, /opt/app/bin/job.sh
   ```

3. **`visudo` 保存时会校验语法**，错误则拒绝写入，避免锁死 `sudo`。

4. **验证（非交互必须成功）**

   ```bash
   sudo -u deploy -n true && echo ok
   ```

5. **工具内调用形态**  
   - 使用：`sudo -n -u deploy -- /bin/bash -lc '你的命令'`（**`-n` 必填**，除非进程能以其它方式保证绝不读密码）  
   - **`su -`** 在无 TTY/密码通道时通常不可靠；若必须用 `su`，往往意味着要以 **root** 或额外组件（如 expect）承接更大风险。

**安全**：`NOPASSWD` 会扩大被攻破时的影响面，务必 **按调用用户、目标用户、命令** 最小授权；避免对高权限账户滥用 `NOPASSWD: ALL`。

## OpenAPI 契约同步

后端 FastAPI 应用是 API 契约源头。修改 `app/harness/api/app.py` 或相关 Pydantic schema 后，先导出 OpenAPI，再让前端重新生成类型：

```bash
python export_openapi_schema.py --output openapi.json
python scripts/ci/export_openapi_for_frontend.py --frontend ../DAgentsUI
```

前端仓库会更新：

- `openapi.json`
- `src/api/types.ts`

提交后端 API 变更时，请同时提交对应的前端契约文件变更，确保 `DAgentsUI` 的 `pnpm run ci` 能通过。

## API 说明（简版）

更完整的契约与路由说明见 **`doc/api-reference.md`**（与实现对齐）、**`app/harness/api/README.md`** 与 OpenAPI 导出。

| 能力 | 路径（示例） |
|------|----------------|
| 健康检查 | `GET /health` |
| 创建会话 | `POST /v1/sessions` |
| 提交消息 | `POST /v1/messages` |
| 全局 SSE（按 client_id） | `GET /v1/streams?client_id=...` |
| 取消当前 turn | `POST /v1/sessions/{session_id}/cancel` |
| 释放会话 | `DELETE /v1/sessions/{session_id}` |
| 指标 | `GET /metrics`（若启用） |

Register Center 的 REST 约定见 **`register_center/README.md`**。

## 开发说明

- **`app/`** 与各子目录维护 **`README.md`** / **`REFERENCE.md`**（目录说明与符号索引；随代码变更更新）。
- **`register_center/`** 由根目录 **`run_register_center.py`** 通过 `importlib` 加载 **`rc_app.py`**，便于不经安装直接运行。
- **运行测试**（与 **`.github/workflows/pr-tests.yml`** 一致）：

```bash
pip install -r requirements.txt
python -m unittest discover -s tests -p "test_*.py" -v
```

默认 discover **仅**包含 `tests/` 下 `test_*.py`：以本地单测为主，**不**依赖 `LLM_API_KEY`。**`test_agent_service.py`** 依赖 `AgentService` 完整导入链（含 `openai` 等），环境不完整时相关用例会 **skip**；安装 **`requirements.txt`** 后应与 CI 行为一致。可选联网 LLM 冒烟：`tests/integration/live_llm_smoke.py`（见 **`tests/integration/README.md`**）。覆盖规划见 **`tests/UNIT_TEST_CHECKLIST.md`**。

- 指标实现见 **`app/observability/metrics.py`**。

## 配置与安全

- 勿将 **`.env`**、密钥、令牌提交到版本库。
- 开发环境建议使用独立凭据与隔离的 API Key。

## 文档入口

- [CHANGELOG.md](CHANGELOG.md)（**`v0.1.0`** 起）
- [SECURITY.md](SECURITY.md)（漏洞报告与支持版本）
- [doc/README.md](doc/README.md)（`doc/` 技术文档索引；该目录下 Markdown 文件名为 **ASCII**）
- [doc/cases/README.md](doc/cases/README.md)（**落地案例**目录说明与索引；具体案例 Markdown 放在 **`doc/cases/`**）
- [doc/built-in-tools.md](doc/built-in-tools.md)（**内置工具**：`get_tools` 列表、异步与审批前提）
- [doc/roadmap.md](doc/roadmap.md)（**路线图**：已实现 / 待办 / 已知限制）
- [doc/architecture-and-flows.md](doc/architecture-and-flows.md)（架构分层与业务流程）
- [doc/agent-input-output.md](doc/agent-input-output.md)（HTTP 入队与 SSE 出站专题）
- [doc/context-compression-and-state.md](doc/context-compression-and-state.md)（上下文与压缩状态）
- [doc/agent-turn-loop.md](doc/agent-turn-loop.md)（Agent 编排循环：`run_turn`、工具入队与审批）
- [doc/a2a-and-register-center.md](doc/a2a-and-register-center.md)（A2A 与 Register Center：`agent_peer`、广播与中继）
- [doc/api-reference.md](doc/api-reference.md)（HTTP / SSE 契约）
- [doc/prometheus-metrics.md](doc/prometheus-metrics.md)（**`/metrics`** 与 Prometheus）
- [app/README.md](app/README.md)
- [app/REFERENCE.md](app/REFERENCE.md)
- [tests/README.md](tests/README.md)

## License

本项目采用 [MIT License](LICENSE) 开源许可。
