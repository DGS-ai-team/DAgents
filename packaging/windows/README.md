# Windows 安装包

## 文件

| 文件 | 说明 |
|------|------|
| `dagents-installer.iss` | Inno Setup 6 脚本（分步配置向导 + 安装；**须 UTF-8 BOM**，否则 `[Code]` 中文会编译失败） |
| `languages/ChineseSimplified.isl` | 简体中文向导文案（CI choco 安装的 Inno 不含此文件，仓库自带） |
| `dagents.cmd` | 安装目录入口 |
| `write-install-config.ps1` | 根据向导 JSON 从 `config.example.yaml` 生成 `config.yaml`（**须 UTF-8 BOM**，供 Windows PowerShell 5.1 解析中文） |

## 安装向导（三批）

1. **LLM (1/3)**：Provider、Base URL、Model、API Key 环境变量名；可选 Mock
2. **Manage (2/3)**：是否启用 Manage、URL、team、registration base_url
3. **功能开关 (3/3)**：Skills / Triggers / Child Agents / Web UI / Browser / 多模态 / expose_to_peers / A2A / tools.enabled_groups

安装结束时：

- 写入 `config.yaml`（保留 `config.example.yaml` 中的注释块；按选项展开 browser/multimodal/manage 等）
- 按选项创建 `.runtime/browser/`（启用 Browser 时）
- **注册 Shell 登录自启**（`HKCU\...\Run` → `dagents shell --background`）并可选立即启动托盘
- 弹出后续步骤提示（API Key、Browser 启动等）

升级安装：若已存在 `config.yaml`，会询问是否覆盖。

## 构建

```bash
PLATFORM=windows-amd64 VERSION=0.x.x scripts/ci/assemble_local_assistant_bundle.sh
VERSION=0.x.x scripts/ci/build_windows_installer.sh
```

需安装 [Inno Setup 6](https://jrsoftware.org/isinfo.php)，并设置 `ISCC` 指向 `ISCC.exe`。

## 界面

- `WizardStyle=modern`、简体中文语言包
- 自定义欢迎/完成页文案
- 开始菜单增加「打开 Web UI」快捷方式
