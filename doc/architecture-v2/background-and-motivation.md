# 重构背景与动机

## 1. 问题：Agent 的能力受限于 Python 运行时的可达范围

DAgents 当前是一个纯 Python 项目。Agent 的所有能力——LLM 推理、工具执行、文件系统访问——都在同一个 Python 进程内完成。这带来一个根本性限制：**Agent 只能部署在能运行 Python 3.11+ 的机器上。**

现实世界中，大量宿主机无法满足这个条件：

| 环境 | glibc / 终端 | Python 3.11+ | 结论 |
|------|-------------|:---:|------|
| Windows Server 2012 | conhost 无 ANSI | ⚠️ 官方不支持 | **Python 能装但 textual TUI 跑不了** |
| Windows Server 2012 R2 | conhost 无 ANSI | ✅ Python 3.12 最后支持 | **TUI 不可用** |
| RHEL 6 / CentOS 6 | glibc 2.12 | ❌ 无法安装 | **连 Python 都装不上** |
| RHEL 7 / CentOS 7 | glibc 2.17 | ⚠️ 需要 manylinux2014 | **勉强可用，部分包需源码编译** |
| 麒麟 V10 | glibc 2.28 | ✅ | **完美支持** |
| Ubuntu 20.04+ | glibc 2.31 | ✅ | **完美支持** |
| macOS | — | ✅ | **完美支持** |

但这些"不支持"的机器上，往往有独特且不可替代的环境能力：

- **Server 2012**：AD 域控、Windows 专属运维工具、遗留业务系统
- **RHEL 6**：老数据库实例、cron 定时任务、生产环境历史遗留
- **任意老机器**：特定的 `custom.md` 规则、`skills/` 目录、kubeconfig、本地密钥

**这些机器的环境能力不应该因为"装不上 Python"就被排除在 Agent 网络之外。**

## 2. 当前架构的瓶颈

### 2.1 TUI 绑定在 Python 进程

```
app/cli/tui/app.py           ← textual.app.App，DAgentsTuiApp
app/cli/tui/approval_screen.py ← textual.screen.ModalScreen
app/cli/tui/prompt_text_area.py
```

textual 依赖现代终端的 ANSI escape + 鼠标上报 + OSC 52 剪贴板。老 Windows conhost 全不支持。维护者明确不修 conhost 兼容问题。

### 2.2 工具执行绑定在本地进程

```python
# app/harness/tools/bash.py 等
# 工具通过 subprocess.run() 或本地文件操作执行
# Agent 无法使用另一台机器上的 kubectl / mysql / PowerShell 工具
```

即使 Python 能跑在某台机器上，这台机器也不一定有业务需要的工具链。

### 2.3 分发形态单一

DAgents 目前只有两种交付方式：源码 `pip install` 或 PyInstaller 单文件包。两者都依赖目标 OS 能运行编译好的 Python 运行时。对于 glibc 2.12 的 RHEL 6，连 `pip` 都找不到兼容的 wheel。

## 3. 核心洞察：Agent 的能力可以被拆分

回顾 Agent 的运行时行为：

```
一次 Agent turn 的完整流程：
  1. 接收消息 → 入队
  2. LLM 推理 → 决策
  3. 工具调用 → 执行
  4. 结果回写 → 再推理
  5. SSE 推送 → 返回用户
```

其中步骤 1、2、4、5 是纯"思考"——I/O bound，等 LLM 返回，不需要特殊环境。步骤 3 是"行动"——真正需要宿主机环境的部分。

**大脑（思考）和身体（行动）不需要在同一个进程，甚至不需要在同一台机器上。**

## 4. 设计目标

| 目标 | 说明 |
|------|------|
| **OS 全覆盖** | 任何能跑 Go 静态二进制的 OS 都能作为 Agent 的执行环境加入网络 |
| **Python 代码最小改动** | Agent 引擎、A2A 协议、Register Center 不动核心逻辑 |
| **向后兼容** | 现有 Agent（server 类型）行为完全不变 |
| **安全隔离** | 工具执行在宿主机侧隔离 |，Agent 之间通过 RC 做分组和 A2A token 鉴权 |
| **运维简单** | Go 端单二进制，无依赖；Python 端加一个模块 |
| **A2A 透明** | 终端 Agent 和非终端 Agent 对 Register Center 没有区别 |

## 5. 参考背景

- [os-compatibility.md](../os-compatibility.md)：各 OS 上 Python 的兼容性详细分析
- [a2a-and-register-center.md](../a2a-and-register-center.md)：现有 A2A 协议与 Register Center 设计
- [architecture-and-flows.md](../architecture-and-flows.md)：当前架构与业务流程
