# `.runtime/scripts/`（发布包内）

Agent 可调用的**独立工具/二进制**目录（与 **skills** 区分；见 Node `BuildSystemPrompt` 中的 `.runtime` 说明）。

执行 **`install.sh`**（Linux）或 **Windows 安装包**后，本目录会加入 **`PATH`**，便于 Agent 与用户直接调用其中的工具（Windows 包内置 `officecli.exe`）。

## 内置工具（Windows 包）

| 名称 | 路径 | 说明 |
|------|------|------|
| **OfficeCLI** | `officecli.exe` | 读写 .docx / .xlsx / .pptx；配合内置 skills `officecli*` 使用 |

索引文件：**[`../scripts_menu.md`](../scripts_menu.md)**（Agent 维护脚本清单时参考）。

## 第三方组件

OfficeCLI 来自 **[iOfficeAI/OfficeCLI](https://github.com/iOfficeAI/OfficeCLI)**（AGPL-3.0）。详见 [`OFFICECLI.md`](OFFICECLI.md)。

**Windows** 打包时由 `scripts/ci/vendor_officecli.sh` 从上游 Release 拉取二进制，并从仓库 `skills/` 拷贝对应 Agent skills 至 **`.runtime/skills/`**。Linux tarball 不含 OfficeCLI。
