# 显式 HTML / SVG 展示协议验收

实现日期：2026-10-18，原生复验：2026-10-19（Asia/Shanghai）。状态：展示协议及交互验收完成；HTML 固定视窗产生的留白记录为后续布局优化。本记录取代此前自动识别 HTML/SVG 的产品规则，不改写早先验收历史。

## 本轮规则

助手需要丰富展示时，使用正文中独立的 `<dagents-render type="html">` 或 `<dagents-render type="svg">` 完整块。只有显式声明的内容触发预览；普通 HTML/SVG 代码块仍是代码，未包裹的原始 HTML/SVG 显示原文。工作区 SVG 资源图片的读取与展示不变。

两种展示块统一默认预览，工具栏可切换源码、复制原始内部源码。源码复用通用代码样式；预览外层不加边框或卡片背景，内容决定自己的视觉样式。HTML 仍在空权限 sandbox iframe 中展示；SVG 仍清理后作为图片，不进入宿主 DOM。

完整协议见[资源引用与展示协议](../../../../development/resource-references.md)。

## 验收范围

- [x] 仅完整、受支持的显式标签激活；标签后的普通 Markdown 保留（自动回归及独立 fixture）。
- [x] 普通 HTML/SVG 围栏、raw 标签、inline code 以及围栏内的展示标签均不激活（自动回归）。
- [x] 未知类型、错误属性、嵌套、未闭合输入不执行预览，也不吞掉后续文字（自动回归；未闭合区直至末尾整体转义，保留内容）。
- [x] HTML/SVG 统一默认预览，可双向切换，复制得到原始内部源码；多个块独立切换（组件自动回归）。
- [x] 同条消息继续流式输出时保留已选视图，切换消息或 Agent 时恢复默认（组件自动回归）。
- [x] 主消息区与紧凑卡片无外框；工具栏可操作，源码横向滚动限于块内（2026-10-19 原生窗口复验）。
- [x] HTML sandbox/CSP、SVG allowlist、块数量/大小限制、资源所有权规则保留（自动回归）。
- [x] 移除旧的 raw/fence 自动识别分支及无调用的识别 helpers，更新提示词和测试。

## 自动验证及构建

- 完整共享前端：91 个文件、531 项测试通过，共享图标另 6 项通过。专项 4 个文件、42 项通过，最后补强用户/系统消息用例后组件文件 15 项通过。
- 专项覆盖 4 个展示块上限、HTML 64 Ki 字符限制、额外属性拒绝、四空格缩进代码不激活、完整标签在用户消息中不激活。SVG 清理、HTML CSP 和资源归属继续有回归。
- 根任务独立解析 QA fixture，确认恰好 3 个显式块、1 个合法文件引用；普通示例、未知类型、未闭合尾部和结束标记保留；普通 Markdown renderer 不生成预览节点。
- Go 1.26.9：`go test ./node/internal/turn` 通过，Windows x64 Node 构建成功。
- Windows x64 Desktop：`npm run tauri -- build --no-bundle` 成功。现有 Vite 大 chunk、Tauri 混合导入及 linker 提示仍为非阻断警告；未制作或发布安装包。
- Node 候选 `.runtime-test-explicit-render-20261018/bin/dagents-node.exe`，SHA-256 `61F5DDE1BDEBA3D1D37DBD3DA1295BD1BF953A53078A399A95899CF890AF069D`。
- Desktop 候选 `.runtime-test-explicit-render-20261018/bin/dagents-shell.exe`，SHA-256 `A16A5EF6B2494708F9382888186CE67D34A5632D59F41FB75AAE630A14EFBE6D`。
- `start-explicit-qa.ps1` 复用已验证的长路径冷备份 helper，检查精确进程路径、18977 监听归属和两个 QA Agent 空闲状态；备份落到此前 refresh 目录内的新时间戳子目录，不覆盖已有备份。已通过 Windows PowerShell 5.1 语法检查；构建阶段尚未执行，用户后续启动结果见下节。此前自动替换进程被执行策略以 `blocked by policy` 拒绝，未提供具体规则原因，本轮未换通道绕过。

## 实机准备

已准备 gitignored `.runtime-test-explicit-render-20261018/explicit-preview.md`，包含两个 HTML 展示块、一个 SVG 展示块、普通代码及 raw 标签、标签语法示例、未知类型和未闭合尾部。只使用隔离 QA Agent 与确定性 Mock 输出；不得据此宣称真实 LLM 生成质量已验收。

原有 QA 文件的本地草稿应保留，Node `18976` 不在本轮切换范围。前述历史记录中的自动识别测试数据会按新协议显示为原文/代码，这是本轮有意改变的行为。

## 2026-10-19 原生窗口复验

用户执行切换脚本后，已核对 Node PID 24624、Desktop PID 25188 的实际路径与上述候选哈希；18977 健康状态为 ok，18976 原实例 PID 16900 仍运行且健康。冷备份位于 gitignored `.runtime-test-resource-refresh-20261018/backups/explicit-20261019-003626-479`，加密密钥与当前 runtime 的本地哈希比较一致，未输出密钥。

在真实 Tauri 窗口使用 Computer Use 验证，未用浏览器页面替代：

- 确认 QA Agent 使用 Mock，向该隔离 Agent 提交已知 fixture。hydrate 确认存在一条对应助手输出且无活跃轮次；这是确定性渲染测试，不是付费模型的生成质量验收。
- 只有两个显式 HTML 块与一个显式 SVG 块默认预览。普通 HTML/SVG 围栏仅显示代码；raw HTML/SVG、行内标签、代码围栏内的展示标签、未知类型及未闭合末尾均以原文保留，结束标记可见。
- 完整对话中切换 HTML A 与 SVG 的源码/预览，HTML B 保留自己的预览状态。HTML/SVG 源码使用同一代码样式。分别点击复制后，将剪贴板与 fixture 的标签内部源码做精确比较，均为 true；不包含展示 wrapper，未输出剪贴板正文。
- HTML 与 SVG 预览无外层卡片框，SVG 渐变、箭头与文字正常，点击 SVG 可放大，Escape 正常关闭。放大关闭后的键盘焦点轮廓是可访问性状态，不是常驻预览边框。
- 通过样例文件链接进入资源页，原有本地 QA 草稿完整恢复；没有保存、丢弃或覆盖草稿。展开紧凑消息卡，HTML/SVG 可双向切换，工具栏完整可见，SVG 适应卡片宽度，源码只在代码块内横向滚动。紧凑卡片复制 HTML 也与原始内部源码精确相等。
- 验收后返回完整对话，保留资源页与草稿，候选 Node/Desktop 继续运行。

仍有一个布局限制：隔离 HTML 视窗沿用固定高度（完整对话 280px、紧凑卡片 190px），短内容会留下空白。这不是展示识别或复制失败；本轮未放宽 sandbox、加入测量脚本或增加协议参数来处理高度，后续应单独设计安全的尺寸策略。SVG 按图片尺寸展示，不受此固定高度限制。

该限制随后进入独立的[HTML 预览高度适配](../2026-10-19/html-preview-height.md)修改；后续实现和验收状态以该记录为准，以上保留本轮验收时的事实。
