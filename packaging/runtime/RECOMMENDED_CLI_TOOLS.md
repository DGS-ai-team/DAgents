# 推荐 CLI 工具清单

DAgents **发布包不内置**第三方 CLI。下列工具经社区验证，适合与本机 Agent 配合使用；请自行下载安装，并确保可执行文件在 **`PATH`** 或 **`<运行根>/.runtime/externaltools/`** 中。

安装后可将 **`.runtime/externaltools`** 加入 `PATH`（Linux **`install.sh`** / Windows 安装包已默认配置），便于 Agent 通过 `bash_run` 调用。

| 工具 | 用途 | 平台 | 上游 | Agent 技能 | 许可证 |
|------|------|------|------|------------|--------|
| **[OfficeCLI](https://github.com/iOfficeAI/OfficeCLI)** | 创建、分析与修改 `.docx` / `.xlsx` / `.pptx` | Windows / Linux / macOS | [iOfficeAI/OfficeCLI](https://github.com/iOfficeAI/OfficeCLI) | 上游 `skills/` 目录；安装后拷贝至 `.runtime/skills/` 并 `/skill load officecli` | [AGPL-3.0](https://github.com/iOfficeAI/OfficeCLI/blob/main/LICENSE) |

## OfficeCLI 安装摘要

详见 [`externaltools/OFFICECLI.md`](externaltools/OFFICECLI.md)。

1. 从 [Releases](https://github.com/iOfficeAI/OfficeCLI/releases) 下载对应平台二进制（Windows：`officecli-win-x64.exe` → 重命名为 `officecli.exe`）。
2. 放入 **`<运行根>/.runtime/externaltools/`** 或系统 `PATH` 目录。
3. 将上游仓库 **`skills/`** 下 `officecli*` 目录复制到 **`<运行根>/.runtime/skills/`**。
4. 在 Node Web UI 的 Skills 面板中加载 **`officecli`**（或 `officecli-docx` / `officecli-xlsx` / `officecli-pptx` 等子 skill），再让 Agent 处理 Office 文档。

```bash
# 验证（示例）
officecli --version
# 或
.runtime/externaltools/officecli --version
```

**合规**：OfficeCLI 以 AGPL-3.0 发布；修改或再分发须自行评估 AGPL 义务。DAgents 本体与 OfficeCLI 为独立组件。

## 扩展本清单

- 用户自行安装的工具：放入 `.runtime/externaltools/` 并更新 **[`externaltools_menu.md`](externaltools_menu.md)**（Agent 查阅索引）。
- 需要 Agent 遵循专用规则时：在 **`.runtime/skills/<name>/SKILL.md`** 编写技能，或通过 **`write-skill`** 技能了解约定。
