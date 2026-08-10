# Windows 安装包

## 文件

| 文件 | 说明 |
|------|------|
| `dagents-installer.iss` | Inno Setup 6 脚本（安装 + 可选 policy 覆盖；**须 UTF-8 BOM**，否则 `[Code]` 中文会编译失败） |
| `languages/ChineseSimplified.isl` | 简体中文向导文案（CI choco 安装的 Inno 不含此文件，仓库自带） |
| `dagents.cmd` | 安装目录入口 |
| `write-install-config.ps1` | 从 JSON 生成 `config.yaml` 的辅助脚本（仍随包分发；**安装器不再调用**） |

## 安装流程

安装程序**不再**在安装向导中配置 LLM / Manage / 功能开关。首次安装时：

- 若不存在 `config.yaml`，从 `config.example.yaml` **复制**一份（不覆盖已有配置）
- 可选覆盖 `policy.yaml` 种子
- 创建 `.runtime/browser/` 目录
- **注册 Shell 登录自启**（`HKCU\...\Run` → `dagents shell --background`）并可选立即启动托盘
- 将安装目录与 `.runtime/externaltools` 追加到用户 `PATH`
- 完成页提示打开 Web UI **「设置 › 连接」** 完成 LLM、Manage 与功能开关配置（不再弹出额外 MsgBox）

安装包提供 **双轨 Shell**（附加任务二选一，落地均为 `bin\dagents-shell.exe`）：

| 选项 | 产物 | 说明 |
|------|------|------|
| **推荐**（默认，有 WebView2 时） | `dagents-shell-tauri.exe` | [`desktop/tray-tauri/`](../../desktop/tray-tauri/)，内嵌 Web UI |
| **兼容模式** | `dagents-shell-legacy.exe` | [`desktop/tray/`](../../desktop/tray/) Go Shell，系统浏览器 |

静默：`/TASKS=shellmodern` 或 `/TASKS=shelllegacy`。zip 包默认 `dagents-shell.exe` 为 Tauri，并附带两套命名产物。

**API Key** 仍通过系统环境变量提供（如 `OPENAI_API_KEY`），Web UI 只配置环境变量名（`api_key_env`）。

升级安装：若已存在 `config.yaml`，不会被覆盖；policy 覆盖仍可选询问。

## 构建

```bash
PLATFORM=windows-amd64 VERSION=0.x.x scripts/ci/assemble_local_assistant_bundle.sh
VERSION=0.x.x scripts/ci/build_windows_installer.sh
```

需安装 [Inno Setup 6](https://jrsoftware.org/isinfo.php)，并设置 `ISCC` 指向 `ISCC.exe`。

## 界面

- `WizardStyle=modern`、简体中文语言包
- **Workbench 浅色主题**：侧栏/角标 BMP 与 Web UI `tokens.css` 浅色模式对齐（`#f5f6f8` / `#2563eb` 主色、`Segoe UI` 字体）
- 资源：`assets/wizard-sidebar.bmp`、`assets/wizard-small.bmp`（`scripts/generate-wizard-assets.py` 可从 `brand-icon.png` 重新生成；**须使用带 CJK 字形的字体**，否则副标题「本机智能助手」会显示为 □□）
- 自定义欢迎/完成页文案；开始菜单增加「打开 Web UI」快捷方式
