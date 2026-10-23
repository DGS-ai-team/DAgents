# Desktop 生命周期与 Windows 安装验收

日期：2026-10-13。原生窗口与本机生命周期已验收；安装器编译、实际安装与升级仍未通过。

## 产品行为

- 关闭窗口隐藏到托盘；再次启动同一安装的 Desktop 应恢复既有窗口。
- 顶部 DAgents 菜单中的“退出 Desktop”只结束桌面应用，Node 继续运行，以保留 Auto 的后台任务。
- 停止 Node 是独立操作。归属记录包含 PID、可执行文件、配置路径、Node ID 与进程创建时间；Windows 停止使用验证过身份的同一进程句柄。
- 外部启动的 Node 可以连接，但 Desktop 不认领、不停止。旧裸 PID 文件不构成停止依据。
- 升级前要求受管 Node 空闲。下载后再次检查，避免只在长下载前判断状态。当前尚无 Node 原子维护锁，检查与停止之间仍可能开始新回合。

## 验收环境

独立目录 `.runtime-test-lifecycle/managed` 与 `external`，端口分别为 18966、18967，独立 Desktop JSON；不启停用户主 Node，不修改实际用户配置、自启或安装记录。

使用内嵌前端的 debug Desktop 可执行文件，运行时不依赖 Vite。本轮模型能力不是测试对象；真实模型与 Auto 场景见此前的原生验收记录。

## 已实测

| 场景 | 结果与证据 |
| --- | --- |
| 独立原生启动 | `http://tauri.localhost/#/chat`，内嵌前端，Node 18966 在线；Vite 1422 未监听。 |
| 中文、空格配置路径 | `node 配置.yaml` 与 `desktop 设置.json` 启动成功；归属记录保留实际配置路径。 |
| 关闭并再次启动 | Desktop 和 Node 进程仍在；安装目录改为大写、传入不同配置文件后再次启动，恢复相同窗口 ID，仍只有一个 Desktop 和一个 Node。 |
| 顶部菜单正常退出 | 点击“退出 Desktop”，Desktop PID 消失；Node 保持 `/health=ok` 且 PID 不变。入口没有放回侧栏底部。见 [原生菜单截图](native/desktop-quit-menu.png)。 |
| 命令行正常退出 | 同时启动两个独立安装目录的 Desktop，退出 managed 安装后 external 安装的窗口与 Node 都继续运行；退出 external Desktop 后该 Node 仍健康且没有被认领。 |
| 进程身份变化 | 仅修改测试归属记录的 `process_start`，停止命令返回 1 并提示进程身份变化；原 Node 仍运行。还验证了不同配置路径停止被拒绝。归属记录随后恢复。 |
| 空闲与重复停止 | `--stop-owned-node --require-idle` 成功停止受管 Node 并删除归属记录；Node 已停止时重复命令返回 0。 |
| 未首配的安全检查 | 新构建 Node 在 `node_profile_completed=false` 时，本机 readiness 返回 `ready=true/active_turn_count=0`；require-idle 停止成功。远程拒绝、活跃回合与待审批仍 busy 由回归测试验证。 |
| 外部 Node 只读状态 | 直接启动 Node 后，`owned_process=false/managed=false/endpoint_ready=true`，未创建归属记录。Desktop 可以连接并正常退出。外部 Node 的停止/升级拒绝实测未通过，见下方审批限制。 |
| Windows 包装命令 | 实测 `help=0`、未知命令 `2`、Node 在线状态 `0`、Node/GUI 离线状态 `1`；后台启动、停止均 `0`，重启后 Node ID 保持。最初发现的错误分派、状态命令误启动第二 Node、退出码被清零均已修复后复验。 |

测试进程均已清理，18966、18967 与 1422 未监听；没有启停主 Node，也没有修改用户安装记录。

## 自动检查

- Rust `cargo test --offline` 28/28，格式检查通过；仅 Windows 实例名测试有明确平台限定。
- Desktop `npm run test:ui` 46/46；内嵌前端的 debug Tauri 构建通过。
- Node readiness 的 API 和 session 回归通过；覆盖未首配 loopback、远程拒绝、活跃回合及待审批。
- Node API 全包 `go test ./internal/api -count=1 -timeout=120s` 通过（30.293 秒）；readiness session 定向回归通过（0.766 秒）。
- API/Workgroup 契约检查通过；本轮没有新增路由。
- `git diff --check` 通过。Vite 既有动态导入及大 chunk 提示仍存在。

自动审批拒绝了外部 Node 停止测试的组合命令，仅返回 `blocked by policy`，未提供更具体原因。本轮改为只读验证归属，并正常退出测试 Desktop；未将外部停止保护实测标记为通过。直接启动的测试 Node 最后按本轮创建的 PID 与精确可执行路径核验后清理。

## Windows 包装边界

安装器改为用户范围，检查 WebView2 Runtime 与目标目录写权限；拒绝自动迁移旧机器范围安装。升级、卸载先请求本安装 Desktop 正常退出，再验证受管 Node 的归属和空闲状态。未知生命周期协议的旧 Shell 不执行新参数，避免误启动旧 GUI 或无限等待。

卸载保留 Node bootstrap、整个 `.runtime` 与独立 Desktop 设置，只清理匹配当前安装的自启项和 PATH 条目。禁止通过 Restart Manager 或进程名结束外部 Node。

## 尚未通过的发布门槛

- 本机没有 ISCC，尚未编译 Inno 安装器；静态检查不能证明 Pascal Script 编译成功。
- 尚未在隔离 Windows 用户/虚拟机执行首次安装、原地升级与卸载。
- 尚未验证 x86 安装包、缺少 WebView2 的机器、签名、升级失败回滚与多平台安装。

可执行清单见 [Windows 安装与升级验收](../../../../../../packaging/windows/install-upgrade-acceptance.md)。
