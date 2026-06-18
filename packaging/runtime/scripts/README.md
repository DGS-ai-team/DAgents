# `.runtime/scripts/`（发布包内）

Agent 可调用的**独立工具/二进制**目录（与 **skills** 区分；见 Node `BuildSystemPrompt` 中的 `.runtime` 说明）。

执行 **`install.sh`**（Linux）或 **Windows 安装包**后，本目录会加入 **`PATH`**，便于 Agent 与用户直接调用其中的工具。

**发布包不内置**第三方 CLI。推荐清单见 **[`../RECOMMENDED_CLI_TOOLS.md`](../RECOMMENDED_CLI_TOOLS.md)**（含 **[OfficeCLI](OFFICECLI.md)** 安装说明）。

索引文件：**[`../scripts_menu.md`](../scripts_menu.md)**（Agent 维护脚本清单时参考）。
