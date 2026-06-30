# externaltools

索引 **`.runtime/externaltools/`** 外置 CLI、编译二进制与 shell 脚本，并读取 **`externaltools_menu.md`** 注入 system prompt。

| 函数 | 说明 |
|------|------|
| `NewCatalog(runtimeDir)` | 绑定 `.runtime` 根 |
| `DirPath(runtimeDir)` | 返回 `<runtime>/externaltools` |
| `RenderPromptSection()` | 生成 system prompt 段落 |

配置路径：`shared/config.Config.ExternalToolsDir()` → `<fs_root>/externaltools`。
