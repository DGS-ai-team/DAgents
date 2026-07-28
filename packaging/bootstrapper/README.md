# DAgents Setup（Tauri 安装向导）

现代安装向导壳：**Vue UI + Tauri**，真正文件落地仍调用仓库已有的 **Inno Setup** 静默安装包（`/VERYSILENT`），保留升级、卸载与权限模型。

## 界面流程

1. 选择安装目录并安装
2. 进度条
3. 完成 → 打开托盘程序

非 Windows 或未嵌入 Inno 包时进入**演示模式**（写标记文件），便于 Linux 上预览 UI。

## 本地开发

```bash
cd packaging/bootstrapper
npm install
# UI only（浏览器）
npm run dev
# 桌面壳（需本机 Tauri 依赖；Windows 上可联调选目录对话框）
npm run tauri:dev
```

将 Inno 产物放到旁路目录以便联调：

```bash
mkdir -p src-tauri/resources
cp dist/dagents-local-assistant-windows-amd64-installer-*.exe \
  packaging/bootstrapper/src-tauri/resources/
```

## 打包（Windows）

先按现有流水线打出 Inno 安装包，再：

```bash
# 从仓库根目录
bash packaging/bootstrapper/scripts/package-with-inno.sh 0.8.4
# 或：VERSION=0.8.4 bash scripts/ci/build_windows_setup_bootstrapper.sh
```

脚本会：

1. 把 `dist/dagents-local-assistant-windows-amd64-installer-*.exe` 拷入 `src-tauri/resources/`
2. 执行 `npm run tauri build`（NSIS 外层包，内嵌 Inno payload）
3. 复制为 `dist/dagents-setup-windows-amd64-{VERSION}.exe`

**Release / manual-package CI** 会同时发布：

| 产物 | 用途 |
|------|------|
| `dagents-local-assistant-windows-amd64-installer-*.exe` | 纯 Inno（服务器/精简环境后备） |
| `dagents-setup-windows-amd64-*.exe` | Tauri 向导（内嵌 Inno） |

## 与 Inno 的关系

| 职责 | 实现 |
|------|------|
| 品牌向导 UI | Tauri + Vue（本目录） |
| 解压文件、写卸载信息、PATH、autostart | Inno `dagents-installer.iss` |
| 安装后 doctor / shell / 开 UI | Tauri（因 `/VERYSILENT` 会跳过 Inno `[Run]`） |

## 说明

- `src-tauri/resources/` 内的 `.exe` **不入库**（体积大）；CI/本地打包时注入。
- 图标沿用 create-tauri-app 默认占位；发版前可用 `npm run tauri icon path/to/icon.png` 替换。
