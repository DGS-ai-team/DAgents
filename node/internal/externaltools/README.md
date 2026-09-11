# externaltools

索引 **`<runtime_root>/externaltools/`** 外置 CLI、编译二进制与 shell 脚本，并读取 **`<runtime_root>/externaltools_menu.md`** 注入 system prompt。

| 函数 | 说明 |
|------|------|
| `NewCatalog(runtimeDir)` | 绑定 `.runtime` 根 |
| `DirPath(runtimeDir)` | 返回 `<runtime>/externaltools` |
| `RenderPromptSection()` | 生成 system prompt 段落 |

配置路径：`shared/config.Config.ExternalToolsDir()` → `<runtime_root>/externaltools`；该目录属于 Node 运行时，不属于 Agent workspace。
