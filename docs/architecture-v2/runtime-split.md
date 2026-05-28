# Python-Go 功能划分

## 1. 划分原则

```
Python = 大脑层（Brain Layer）
  负责：思考、决策、协调
  特征：I/O bound、依赖 LLM SDK 生态、需要 pydantic 数据建模

Go = 身体层（Body Layer）
  负责：执行、交互、跨平台
  特征：静态编译、零依赖分发、原生 OS API 访问
```

边界判断规则：**凡是涉及 LLM 推理、Agent 决策、A2A 协议的路归 Python；凡是需要在宿主机上真正执行命令、操作文件、渲染 TUI 的路归 Go。**

## 2. 详细分工

### 2.1 Python 后端（大脑层）

```
组件                             状态        说明
────────────────────────────────────────────────────────────
app/core/main_agent/             不变        Agent 决策循环、ReAct 编排
app/core/main_agent/runtime_*.py 不变        LLM 调用运行时（OpenAI/Anthropic）
app/harness/queue/               不变        消息队列、优先级、背压
app/harness/service/             不变        AgentService 主循环
app/harness/history/             不变        SQLite 会话持久化
app/context/                     不变        上下文模型、压缩策略
app/schemas/                     不变        数据模型（AgentPeerEnvelope 等）
app/observability/               不变        Prometheus 指标
app/config/                      小幅调整    新增 proxy 相关配置项
app/harness/tools/               小幅调整    工具路由：本地 vs ProxyManager
app/harness/api/app.py           小幅调整    新增 /v1/proxy/* 路由
app/proxy/                       ★新增      ProxyManager、ProxyConnection
register_center/rc_models.py     小幅调整    AgentRecord 加 3 个可选字段
register_center/rc_app.py        不变        RC 核心逻辑不动
app/cli/tui/                     可保留      在能跑 Python 的机器上继续用 textual
```

### 2.2 Go 执行代理（身体层）

```
go-proxy/                        ★新增，独立仓库
├── main.go                      入口 + 参数解析 + HTTP server
├── env/scanner.go               环境扫描
│   ├── 读取 custom.md
│   ├── 解析 skills/ 目录
│   └── 检测可用工具链（kubectl, helm, mysql, docker, ...）
├── executor/shell.go            安全 shell 执行
│   ├── cmd.Run() + context.WithTimeout
│   ├── 命令白名单/黑名单
│   └── FS_ROOT 沙箱约束
├── executor/file.go             文件读写
│   ├── 文件内容读取（限于 FS_ROOT）
│   ├── 文件写入
│   └── 文件列表
├── client/register.go           向 Backend 注册
│   ├── POST /v1/proxy/register
│   ├── 心跳 POST /v1/proxy/heartbeat
│   └── 退出时注销
├── client/executor.go           Backend 指令接收
│   └── POST /execute（Backend → Proxy）
└── config/                      配置管理
    └── proxy.yaml               agent_id, backend_url, FS_ROOT, 命令白名单, schedulable
```

### 2.3 Go TUI 客户端（可选）

```
go-tui/                          ★新增，独立仓库
├── main.go                      入口

├── client/api.go                Python 后端 HTTP/SSE 客户端
│   ├── POST /v1/sessions        创建会话
│   ├── POST /v1/messages        发送消息
│   ├── GET /v1/streams          SSE 订阅（接收后端推送）
│   └── POST /v1/sessions/{id}/cancel  取消当前 turn

├── tui/app.go                   tview 主界面
│   ├── 聊天面板（TextView 滚动 + Markdown 渲染）
│   ├── 输入区（InputField）
│   └── 状态栏（Agent 名称、连接状态）

├── tui/approval.go              审批弹窗
│   ├── 工具审批模态框
│   ├── 批准/拒绝/按编号选择
│   └── POST resume 提交决策

├── tui/theme.go                 主题管理
│   └── 亮色/暗色切换

└── config/                      配置管理
    └── tui.yaml                 backend_url, agent_id, 主题
```

## 3. 功能重叠的处理

| 功能 | Python 端 | Go 端 | 谁做主 |
|------|-----------|-------|--------|
| 环境扫描 | — | ✅ Proxy 启动时扫描并上报 | Go |
| custom.md 读取 | ✅ server agent 本地读取 | ✅ terminal agent 由 Proxy 上报 | 各管各的 |
| 工具执行 | ✅ server agent 本地 subprocess | ✅ terminal agent 由 Proxy 执行 | 根据 agent_type 路由 |
| TUI 渲染 | ✅ textual（Python 能跑的机器） | ✅ tview（所有平台） | 并行提供，用户选择 |
| LLM 调用 | ✅ | ❌ | Python |
| A2A 协议 | ✅ | ❌ | Python |
| SSE 推送 | ✅ | ❌（Go TUI 是消费端） | Python |

## 4. Python vs Go 适用场景决策树

```
需要调用 LLM API 做推理？
  ├── 是 → Python（OpenAI/Anthropic SDK 生态不可替代）
  └── 否 → 继续

需要 pydantic 级数据建模和校验？
  ├── 是 → Python（Go struct tag 不够用）
  └── 否 → 继续

需要跨老旧 OS 分发、零依赖运行？
  ├── 是 → Go（CGO_ENABLED=0 静态编译）
  └── 否 → 继续

需要原生调用 Windows Console API / 内核 syscall？
  ├── 是 → Go（golang.org/x/sys 封装完善）
  └── 否 → 继续

需要直接在宿主机执行 shell 命令？
  ├── Python 能跑在目标机上 → Python subprocess 够用
  └── Python 不能跑 → Go Proxy

需要 TUI 交互界面？
  ├── 目标机能跑 Python + 现代终端 → textual
  └── 目标机是老 Windows / 不确定 → Go + tview
```

## 5. 通信协议

Python 与 Go 之间仅通过 HTTP/JSON 通信，无共享内存，无代码耦合：

```
Python Backend ←──HTTP/JSON──→ Go Proxy
                    │
                    │  POST /v1/proxy/register    注册
                    │  POST /v1/proxy/heartbeat   心跳
                    │  POST /execute              工具执行（Backend → Proxy）
                    │
Python Backend ←──SSE/HTTP──→ Go TUI
                    │
                    │  POST /v1/sessions          创建会话
                    │  POST /v1/messages          发送消息
                    │  GET /v1/streams            接收 SSE 推送
```

所有通信都是纯文本 JSON，无二进制协议依赖。同级跨地域延迟对比 LLM API 调用延迟可忽略。
