# 工作区写操作协调设计

日期：2026-09-09。状态：目标设计与验收约束，已有部分实现，尚未完成全链路验收。本文落实 Auto 方案 E：同一 Node 上，解析到同一 canonical 工作区的写操作必须串行；读操作可以并行。目录分组、Agent ID 和 UI busy 标记都不是锁，也不构成安全沙箱。

当前源码已有 `node/internal/workspacecoord`、文件写入与替换、shell、terminal 的租约接入点，以及对应集成测试。以下接入清单用于核对最终约束，不表示这些位置均尚未编码，也不表示现有实现均已满足。仍需以真实并发操作、后台进程退出和 Node 实际装配共享协调器的证据验收。

## 目标和边界

协调器是 Node 本地的 best-effort 并发控制。它只保证同一进程内、同一 Node 上通过本 Node 工具路由的操作不会同时持有重叠工作区的写租约。不同 Node、SSH/Linux channel 的远端主机或未接入该路由的外部进程不作跨节点保证，接口和 UI 必须明确显示这一点。

读操作首版完全不获取租约，因此可与读和写并行；系统不承诺读操作得到一致快照。会修改文件或可能修改文件的操作获取写租约；shell 命令无法可靠静态判断副作用，`bash_run`、本地 `terminal_command` 和 Linux channel 写入按写操作处理。`read_file`、`read_image`、`show_image`、`glob_files`、`grep_file`、`grep_files`、`terminal_read`、`terminal_list` 属于无租约读操作。`write_file`、`search_replace`、`terminal_upload`、`terminal_input` 属于写操作。`terminal_open` 本身只创建进程，但若 shell 会话进入工作区，应持有该会话的租约，直到退出、显式关闭或取消。

## 租约模型

新增 Node 级 `WorkspaceCoordinator`，由所有 Agent Registry 共享；Registry 只保存 coordinator 引用，不按 Agent 建立独立锁。两个写请求仅在 canonical 工作区相同，或一方是另一方的祖先/后代时冲突：`/repo` 与 `/repo/pkg` 冲突，`/repo-a` 与 `/repo-b` 不冲突。读操作不取租约，不阻塞任何写操作。首版建议使用每个 coordinator 一个 mutex 和 condition/channel 等待，避免锁顺序反转。

租约记录至少包含 lease ID、Agent ID、session ID、tool call ID、operation kind、canonical root/target、Node ID、开始时间、后台进程 ID（如有）和状态。获取失败不能写文件或启动 shell，返回结构化 `workspace_busy`，同时包含当前占用的 operation kind 和有限的 Agent/session 摘要。不得返回命令文本、环境变量或凭据。等待应有 context 取消和上限；取消等待返回 `workspace_coordination_cancelled`，超时返回 `workspace_busy_timeout`。UI 显示“工作区忙碌：另一个写操作正在运行”，可附目标 Agent 和剩余/已用时间；跨 Node 只显示“本 Node 协调”，不能伪装成全局锁。

后台进程的租约生命周期绑定 provider-neutral `Process`：在真正 `Process.Start` 之前获取，Start 失败立即释放；进程正常 `Wait`、`Terminate` 后确认退出、context 取消并完成输出采集后释放。不能在工具函数返回时释放后台进程租约，也不能只依赖 goroutine 返回；释放点应集中在 process `Wait`/exit 回调和取消清理的共同路径。长期本地 Terminal 同理，在 `OpenTerminal`/Start 成功后把 lease 放入 terminal session，`Wait`、`Close`、Terminate 和启动失败均幂等释放。Node 重启会丢失内存租约；恢复的孤儿远程进程不能被声称已协调，标为 unknown 并按现有远程 termination 语义处理。

## canonical 路径规则

工作区根沿用 `agentruntime.EffectiveWorkspaceRoot` 的解析结果。应把其当前私有 `canonicalWorkspacePath` 提升为共享、可测试的 canonical helper，供 Agent 创建、Registry 初始化和协调器使用；不能由 tools 再实现一套规则。已有目录先 `EvalSymlinks`，不存在的目标逐级解析已存在父目录，再拼接剩余部分；创建与实际打开之间仍有 TOCTOU，本文不把它宣称为 OS sandbox。

比较前统一绝对路径、清理分隔符和 `.`/`..`，Windows 使用大小写不敏感比较并保留 UNC/长路径语义；大小写折叠不能在 Unix 启用。符号链接、junction 和 mount point 必须按 real path 比较，链接到工作区外的目标被拒绝或归入真实目标租约，不能只按词法前缀放行。一个不存在的文件以其 canonical parent 加文件名形成键；目录和文件目标都加入祖先集合。无法解析、权限不足或路径在检查后消失时返回 `workspace_path_unresolved`，不启动命令、不写文件。

## 实际接入位置

1. `node/internal/agentruntime/workspace.go` 的 `canonicalWorkspacePath` 是现有 canonical 化源头；将其抽成共享导出函数，并补 Windows case-fold、symlink/junction、父子目录和不存在目标测试。
2. `node/internal/tools/registry.go` 的 `Registry` 增加共享 coordinator；`NewRegistry` 注入 Node 级实例，不能在每个 Registry 内 `New` 一个锁。`registry_path.go` 的 `resolvePath` 返回 canonical target/lease key 所需信息，保留现有 workspace 边界错误。
3. `fs_write.go` 和 `fs_search_replace.go` 在解析并确认目标是文件后、`MkdirAll`/`os.WriteFile` 前获取写租约，并用 defer 释放；编码读取阶段可拆为读租约，最终写入必须重新持有写租约并重新读取/检查目标，避免 stale replacement。
4. `bash_runner.go` 的 `runShellSync` 在 `startShellProcess` 成功、`Process.Start` 前后接入 lease；更可靠的实现是在 provider-neutral execution seam 统一包装 `Process`，使所有本地 provider 都经过同一生命周期。shell cwd 采用工作区目录键，保守地视为写。
5. `terminal_command.go`、`terminal_tools.go` 和 `local_terminal.go` 在命令执行/输入及 Terminal session 生命周期接入。`terminal_read` 不获取写租约；Linux channel 的 lease 只能标注 Node 本地路由或 provider 返回的 target identity，不能宣称远端跨 Node 协调。
6. `node/internal/agentruntime/build.go` 或 Node server wiring 创建一个 Node 级 coordinator，并将它传入每个 Registry；Agent 重载不得丢失同一 coordinator。取消、session 清理和 server shutdown 要调用 coordinator 的 lease release/mark-unknown 钩子。
7. tool 执行错误协议和 UI activity/terminal 状态消费 `workspace_busy`、`workspace_busy_timeout`、`workspace_coordination_cancelled`，展示 busy reason；成功、拒绝、超时和释放都写 execution audit/event，但不记录敏感命令内容。

## 一致性和失败语义

租约只保护接入的工具调用，不能回滚已经写入的文件。获取租约后任何准备失败都释放；保存/写入失败也释放并返回原操作错误，租约状态本身不作为成功依据。重复释放必须安全，lease ID 必须防止旧进程释放新租约。协调器内存耗尽、路径解析失败或状态不确定时 fail closed，对写操作返回明确错误；读操作仍可执行但不能借此绕过路径权限。

首版不做公平性承诺；应避免同一 Agent 无限占用，记录排队时长并允许 context 取消。后续可加入队列优先级和租约最长时间，但后台 shell 的最大时间仍由现有 hard limit 控制。任何跨 Node 的分布式锁、共享数据库锁或远端 helper 都属于后续方案，不能在本设计或 UI 中暗示已经存在。

## 验收用例

- 两个 Agent 对相同 canonical 目录执行 `write_file`/`search_replace`/shell 写入时，一个持有租约，另一个得到 `workspace_busy`；目录父子路径也冲突，前缀相似路径不冲突。
- 两个 Agent 读取同一目录可并行；写操作只与重叠写租约互斥，不因读操作等待或返回 busy。无锁读取不承诺一致快照；需要原子替换的文件工具应另行验证其写入保证，不能由协调器推导。
- symlink、Windows 大小写、UNC、junction、父目录和不存在文件的 canonical key 与实际 workspace 规则一致。
- shell 正常退出、超时、context 取消、Start 失败和输出采集失败都释放租约；后台进程在退出前租约仍存在。
- Terminal 输入/关闭/退出释放对应 session lease；Node 重启后不报告旧远端进程仍受本地协调。
- UI 和 API 对 busy、路径无法解析、等待取消和跨 Node 限制分别给出稳定错误分类。
