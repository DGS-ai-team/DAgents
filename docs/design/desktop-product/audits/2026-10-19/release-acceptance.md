# 发版验收复核

日期：2026-10-19（Asia/Shanghai）。结论：本轮代码与核心 Desktop 交互达到提交 `dev` 开发 PR 的条件；尚未达到稳定版正式发版标准。

## 范围与版本

- 最近正式版仍是 v0.11.0（2026-09-11）；本轮不修改 VERSION、不创建标签、不发布安装包。
- PR #347 已合入 dev，基线为 `44e5e9de`；该提交全量 CI 已通过。本轮使用新分支 `codex/resource-workbench-release-qa` 提交资源相关改动，需对新 head 重新跑 CI。
- 范围包含 Node 工作区条件读取、移除新调用的 show_image、资源引用/静态 HTML/SVG 协议、自动刷新与草稿保护、消息与资源切换过渡、紧凑消息卡及资源操作布局。历史工具媒体回放保留，数据库 schema 和依赖锁未改变。
- 本轮原生专项最后使用 polished Desktop，SHA-256 `E0BACC3A3140E61825BB2B93B16D250903F5404FA1B5103FAD4CF0C311B2C4BE`。验收时 Node 为 18977 的隔离实例。后续只有两处 lint 说明和 Go 等价前缀处理调整，不能把运行中进程说成最终发行包。

## 自动质量门槛

| 检查 | 结果与证据范围 |
| --- | --- |
| Go 1.26.9 五模块全量测试、vet、gofmt | 通过。覆盖 Node/Client 与三套 shared 模块。 |
| Go race | session、turn、queue 三包通过。 |
| Staticcheck | 首轮检出 ETag 前缀解析 S1017，改为等价 TrimPrefix 后全模块通过；API 包回归通过。 |
| Go 漏洞扫描 | 五模块无可达漏洞。Node 的 x/crypto 模块包含 GO-2026-5932，但该 OpenPGP 包未被导入或调用。 |
| 共享前端 | 91 个文件、534 项 Vitest 通过，图标 7 项通过，lint 通过。安全正则增加局部 lint 理由，未放宽控制字符限制。 |
| Desktop | UI 77 项、资源组件专项 39 项通过，Vite 构建通过；Windows Tauri release 候选已构建并用于原生专项。 |
| Rust | fmt 检查、locked 测试通过，47 项单元测试通过；RustSec 缓存库扫描 484 依赖无漏洞、7 警告。Windows 图不包含 glib。 |
| Manage / Python | Console build/lint、Python 3.13.14 全量 unittest 202 项、ruff、pyright 通过。 |
| 依赖与发布静态检查 | 三套 npm audit 为 0；Python 主锁为 0，Browser 锁在既有明确例外下无剩余项；版本元数据、打包/安装器契约、工作流 pin、npm 许可证检查通过。版本校验仅为 0.11.0 开发元数据一致。 |

独立复核检查了工作区所有权、304/ETag 语义、Agent/Node 切换代次、保存与刷新串行、草稿基线、旧 iframe 消息拒绝、opaque sandbox、固定脚本 CSP hash、SVG 与 HTML 清理、资源 URL 和 URL/监听回收；未发现新增代码阻断问题。

## 原生与 API 冒烟

此前本轮真实 Tauri 专项验证短/长 HTML、SVG、源码切换、资源链接、草稿恢复、告警详情、重载确认后取消、消息卡全宽和输入固定位置。修复了窄预览横溢出引入内层纵滚、重复冲突说明、图片尺寸覆盖三处实机问题，最终图片尺寸与间距已复验通过。详见[资源页记录](resource-layout-polish.md)。

本次发版复核追加：

- Node 通用、模型与连接、帮助反馈页面可加载；当前 QA 只有 Mock profile，没有可用 Manage。未保存配置或传送用户反馈。
- 在已确认的隔离 Mock Agent 通过真实输入框发送固定的 `Release QA 2026-10-19: READY`，出现一条用户消息、一条助手回包和空闲后新对话分隔；hydrate 核对各一条，`has_active_turn=false`。这是通信/持久化/终态冒烟，不代表实际 Provider 或 LLM 工具能力测试。
- 现有 QA 草稿不保存、不覆盖、不丢弃；没有改变浏览器能力、Linux 通道或全局系统设置。

构建告警仍有 Vite 大 chunk/混合导入、Manage eval、两条 Vue const v-model 编译说明及 Rust linker 信息，均非本轮测试失败。Rust 的 glib unsoundness 和未维护 crate 警告已保留，未新增忽略项。

## 稳定版放行前的剩余门槛

1. 新开发 PR 最新 head 的远端 CI、审查与合并。前一提交的绿灯不能替代本批代码。
2. 冻结源码后生成正式版本的 x64 完整安装包；在隔离标准用户 Windows 环境做初装、运行中升级、卸载保留数据、备份恢复及 v0.11.0 旧发行迁移。包含快捷方式、自启、路径与失败恢复，不把安装器编译视为安装成功。
3. 最终包连接实际 Provider / MCP、真实 SSH loopback Linux Node，覆盖远端文件与终端重连；Browser 实际任务与 Linux 发行包启动。当前只存在 Mock/合成服务和专项记录。
4. 最终候选至少 24 小时 Auto 耐久及运行/待审批/空闲重启恢复；既有短周期专项不能等同此门槛。
5. 正式版本、包内容、manifest、SHA256SUMS、许可证及更新选包/下载/安装链路核对。较早的 29fc308 安装候选不含当前全部资源功能。

完整放行列表见[发布收束清单](../../../../release-readiness.md)。Windows 本版仅 x64 完整包，x86 暂停；本轮开发 PR 不承担稳定版放行或自动合并。
