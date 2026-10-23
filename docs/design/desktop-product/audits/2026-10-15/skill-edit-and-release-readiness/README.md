# Skill 编辑与 P0 交付检查（2026-10-15）

## 本轮范围与结果

先移除“加载 Skill 后禁止修改其文件”的限制，再推进 P0 交付与配置验收。编码由 Luna 完成，主代理负责范围、代码审查与验收。本轮不改变个人 Node 的产品定位，也未开始多用户或新的远程连接功能。

### 已加载 Skill 可以修改

- 删除内置 `loaded_skill_file_guard`、对应示例插件与 `write-skill` 自带构建材料，清除注册、发布检查和当前使用说明中的旧限制。
- `write_file`、`search_replace`、`bash_run` 不再因路径属于已加载 Skill 而被额外拒绝，仍由普通工具策略和其他现有 hooks 决定执行、审批或拒绝。
- 当前 Turn 使用的 Catalog 与已注入正文继续保持快照稳定；磁盘修改不会偷偷替换当前模型上下文，新 Turn 能读取更新后的正文。真实文件工具回归已覆盖这个生效边界。
- 通用 Hook 插件能力仍保留；本轮不扫描或删除用户安装目录中的自定义插件、Skill 和其他文件。

### P0：打包输入与版本一致性

- Assembler 在替换旧 staging 前检查所有实际使用的二进制输入，提前解析 Windows Shell 回退路径，拒绝指向即将被删除目录的输入和符号链接。Windows 目标路径比较不区分大小写。
- 隔离 contract 覆盖 Client、Browser、Shell 误指向 staging、Windows 大小写别名、缺少 Shell、跳过可选资源和正常组装。成功案例复制真实 `packaging/runtime`，校验 Skill 正文原样打包，并检查不再夹带已移除的保护插件材料。
- 手动打包复用 release validator，输入版本必须与 checkout 的源码版本及相关元数据一致。修正 validator 与 prepare helper 对 README 两种现有版本标记的识别，准备下一版时更新原标记而非插入第二个。
- 以上是源码与合成二进制的组装契约测试，不是最终安装包的运行验收。本轮没有修改 `VERSION` 或发布新版本。

### P0：MCP、Linux 配置与反馈

- 修复 MCP 配置 GET/PUT 响应返回明文环境变量或请求头字面量的问题，改为服务端脱敏。占位符仅能保留同一 server/key 已存储的值；无原值的占位符被拒绝。替换值可轮换密钥，删除字段则真正删除。
- MCP 配置投影显式包含 `enabled` 和 `enabled_tools`，避免保存编辑器文本时把原本禁用的服务重新启用；保留工具白名单。内部普通 formatter 继续保留无损序列化语义。
- HTTP 回归覆盖密钥保留、轮换、删除、未知占位符拒绝、禁用服务不启动、服务与工具启停。MCP 服务使用本地合成 JSON-RPC fixture，没有连接用户的外部服务。
- Linux 配置 HTTP 回归补充凭据/通道列表不泄露密钥或内部 SecretRef、Agent 绑定读取；没有发起真实 SSH 登录。
- 另行构建隔离 Node，连接本地实际 Manage FastAPI 与临时 SQLite：提交合成反馈，Node 的 `delivery_status` 为 `delivered`；Manage 管理员列表命中 1 条，详情的内容与 Node ID 一致。首轮请求被未完成首配门控拒绝后，通过该隔离 Node 的正式设置 API 完成首配并重测通过。[脱敏结果摘要](feedback-result-sanitized.json) 随报告保存；两端测试进程均已退出，数据与日志保留在忽略目录 `.runtime-test-desktop-real/feedback-e2e-2026-10-15b/`。

## 验证与边界

已通过 Skill hooks/Turn/文件工具回归、Node 全模块测试，以及 MCP/API/store 的后续针对性回归；发布元数据正例、版本不匹配负例、4 项 release helper unittest、assembler contract、API contracts、workflow pins 与 diff 检查通过。新增 MCP 测试按可观察行为检查服务刷新，不把后台刷新次数恰好等于某值当作契约。

本轮配置与反馈结果属于 API 层验收，不是原生 Desktop UI 验收。先前日常使用的原生证据见 [日常使用回归](../daily-use/README.md)。

## 后续优先级

1. **P0：最终 Windows 安装候选验收。** 安装、升级、卸载、数据保留和打包 Browser 的真实启动仍需要完成最终版本复测。旧版无安装 marker 的目录目前会被安全拒绝，只有人工备份、退出旧程序、使用旧卸载器再安装的路径；自动旧版迁移和应用级事务回滚没有实现，不能对外宣称已有。
2. **P0：原生配置页与用户外部集成。** API 回归之后仍需在最终 Desktop 构建操作配置页；真实 MCP/SSH 服务要在明确的测试环境继续验收。
3. **P1：远程与长期运行。** 预建立 SSH 隧道下的真实 Linux Node、断线恢复、多 Desktop 并发、Auto 多日运行与 Dreaming 中断恢复，以及小屏/DPI/性能。继续使用既定个人 Node 边界。

先前自动审批拒绝最终安装循环复跑、打包 Browser 启动和更新后 Desktop 重开命令，只返回 `blocked by policy`。本轮没有拆分或换工具重试这些动作；对应验收缺口仍保留，不能用契约测试代替。
