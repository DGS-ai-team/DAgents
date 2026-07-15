# Release Hub：版本检查、安装包托管与 Client 升级

> **状态**：**v0.5.1+ 已落地**（Manage Release Hub + Node `update_checker`）；Windows Shell 见 [windows-desktop-shell.md](./windows-desktop-shell.md) §10（**v0.6.0+**）。
> **对齐**：[manage-phase2-capabilities.md](./manage-phase2-capabilities.md) §2、[three-component-model.md](./three-component-model.md)、[windows-desktop-shell.md](./windows-desktop-shell.md) §3.11（Windows Shell 更新路径）

## 1. 目标

- **Manage** 托管 `dagents-local-assistant` 安装包（`MANAGE_RELEASES_DIR`），Console 上传/发布。
- **Node** 向 Manage 查询是否有新版本，经 `GET /v1/agent/update` 暴露给 Client（**Linux / headless / 无 Shell 路径**）。
- **Windows Desktop Shell**（自 v0.6.x 规划）：**Shell** poll Manage 并 orchestrate apply；见 §10。
- **三端 Client**（Python TUI / Go TUI / Web UI）支持 `/version`、`/update`；升级由 `dagents update` + `install.sh` / Shell apply 执行。
- **Manage 发版** 时将同版本 **linux-amd64 安装包** 打入 offline bundle 与 Docker 镜像 seed。

## 2. 原则

| 原则 | 说明 |
|------|------|
| Client 不直连 Manage | **默认**经 Node 本地 API；**Windows + Shell** 经 Shell localhost API（§10） |
| 单包单版本 | 全栈共用 `node/internal/version/version.go` |
| 上传可 draft | 默认 `draft`，发布与设 latest 分离 |
| 非静默升级 | 须用户确认；turn 进行中须拦截（Shell apply 前问 Node `upgrade-readiness`） |

## 3. Manage 目录布局

```text
{MANAGE_RELEASES_DIR}/
└── dagents-local-assistant/
    └── stable/
        └── linux-amd64/
            ├── 0.5.1/
            │   ├── dagents-local-assistant-linux-amd64-0.5.1.tar.gz
            │   └── manifest.json
            └── latest.json          # 指向当前 latest 版本
```

## 4. 包状态

| status | is_latest | Node check |
|--------|-----------|------------|
| draft | false | 不可见 |
| published | false | 可见，非 latest |
| published | true | 作为 latest |

API：`POST /v1/releases/packages`（默认 draft）→ `POST .../publish` → `POST .../promote`（设 latest）。

## 5. Manage API

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/v1/releases/packages` | Admin 上传（multipart） |
| GET | `/v1/releases/packages` | 列表（`?status=draft\|published\|all`） |
| POST | `/v1/releases/packages/{artifact}/{channel}/{platform}/{version}/publish` | draft→published |
| POST | `/v1/releases/packages/{artifact}/{channel}/{platform}/{version}/promote` | 设为 latest |
| DELETE | `/v1/releases/packages/{artifact}/{channel}/{platform}/{version}` | 删除 |
| GET | `/v1/releases/packages/.../{version}/download` | 下载 |
| GET | `/v1/releases/packages/.../latest/download` | 下载 latest |
| GET | `/v1/releases/check` | Node 版本检查 |

## 6. Node

- `UpdateChecker` sidecar：`GET {manage}/v1/releases/check?current=&platform=&channel=`
- `GET /v1/agent/update`：返回缓存结果供 Client
- **Windows + Shell 安装（规划）**：`UpdateChecker` **迁移至 Shell**；Node 保留 **`GET /v1/agent/upgrade-readiness`**（apply 前空闲判定）；`/v1/agent/update` **deprecated**（见 §10、[windows-desktop-shell.md](./windows-desktop-shell.md) D36–D38）

## 7. Client `/update`

- Python/Go TUI：确认后 `exec dagents update --from-client`
- Web UI：**Linux/无 Shell** 展示 `dagents update` 可复制命令；**Windows+Shell** 读 Shell `GET /v1/desktop/update`（[F-X8](./windows-desktop-shell.md)）
- Windows `dagents.cmd update`：**规划**委托 Shell orchestrate（[F-I12](./windows-desktop-shell.md)）；Shell 未运行时回退 `dagents-client update`

## 8. Manage 打包 seed

- CI `assemble_manage_bundle.sh` 拷贝 `dagents-local-assistant-linux-amd64-{VERSION}.tar.gz` 到 bundle `releases/`
- Docker 镜像 `/app/bundled/releases/`；启动时 `seed_bundled_releases()` 导入（不覆盖已有 latest）

## 9. 配置

| 变量 | 默认 |
|------|------|
| `MANAGE_RELEASES_DIR` | `{dirname(MANAGE_DB_PATH)}/releases` |
| `MANAGE_RELEASE_MAX_BYTES` | `536870912` (512MB) |
| `MANAGE_SEED_BUNDLED_RELEASES` | `1` |
| `MANAGE_BUNDLED_RELEASES_DIR` | `/app/bundled/releases` |

## 10. Windows Shell 路径例外

**决策**：[windows-desktop-shell.md](./windows-desktop-shell.md) **D36–D38**（2026-07）。

### 10.1 动机

| 现状问题 | Shell 接管后 |
|----------|--------------|
| 查更新须 **Node 在跑** | Shell 常驻，可读 `VERSION` 并 poll Manage |
| `dagents update` 停/启 Node 与 **Shell 监护** 职责重叠 | Shell 统一 stop → 换文件 → start（D29） |
| Node 兼 **运行时 + 安装管家** | Node 专注 turn；安装态归 Shell |

### 10.2 数据流（Windows 桌面）

```text
Manage ──GET /v1/releases/check──► Shell（UpdateChecker）
                                      │
                    ┌─────────────────┼─────────────────┐
                    ▼                 ▼                 ▼
              Toast F-N9      localhost update API   dagents.cmd update
                    │           (Web UI F-X8)              │
                    └─────────────────┬─────────────────┘
                                      ▼
                            用户确认 → upgrade-readiness?
                                      ▼
                            stop Node → 下载/覆盖 bin → start Node
```

### 10.3 职责划分

| 组件 | 查版本 | 暴露给 Client | 执行 apply |
|------|--------|---------------|------------|
| **Shell** | ✅ poll Manage | ✅ `GET /v1/desktop/update`（localhost） | ✅ orchestrate |
| **Node** | ❌（迁移后） | ⚠️ `/v1/agent/update` deprecated | ❌ |
| **Node** | — | ✅ `/v1/agent/upgrade-readiness` | —（仅判定能否升） |
| **Linux / SSH** | Node UpdateChecker | `/v1/agent/update` | `dagents update` + `install.sh` |

### 10.4 共享实现

- check URL、manifest、下载校验抽到 **`shared/update`**（D38），Shell 与 Node（Linux 路径）共用。
- 单包单版本不变：apply 仍覆盖整包 `bin/*` + `VERSION`（与现 `dagents.cmd update` PowerShell 段一致）。

### 10.5 分期

| 版本 | 内容 |
|------|------|
| **v0.6.0** | 文档定 D36–D38；`dagents update` 的 **stop/start 改由 Shell**（若 Shell 在跑） |
| **v0.6.x** | Shell UpdateChecker + Toast + F-X8 + F-ND1 |
| **v0.7** | Windows 默认路径 **移除** Node `UpdateChecker`；`/v1/agent/update` 返回 delegate ✅ |

### 10.6 不变量

- **非静默升级**：须用户确认；Node 报告 busy 时 Shell **不得** apply。
- **Manage API** 与目录布局（§3–§5）**不变**；变的只是 **谁 poll、谁 orchestrate**。
- **Web UI 运行时**仍直连 Node SSE/API；仅 **Update 面板**在 Windows 上改读 Shell（窄 API，同 D24 混合模型）。
