# Release Hub：版本检查、安装包托管与 Client 升级

> **状态**：M1 实现中  
> **对齐**：[manage-phase2-capabilities.md](./manage-phase2-capabilities.md) §2、[three-component-model.md](./three-component-model.md)

## 1. 目标

- **Manage** 托管 `dagents-local-assistant` 安装包（`MANAGE_RELEASES_DIR`），Console 上传/发布。
- **Node** 向 Manage 查询是否有新版本，经 `GET /v1/agent/update` 暴露给 Client。
- **三端 Client**（Python TUI / Go TUI / Web UI）支持 `/version`、`/update`；升级由 `dagents update` + `install.sh` 执行。
- **Manage 发版** 时将同版本 **linux-amd64 安装包** 打入 offline bundle 与 Docker 镜像 seed。

## 2. 原则

| 原则 | 说明 |
|------|------|
| Client 不连 Manage | 仅经 Node 本地 API |
| 单包单版本 | 全栈共用 `node/internal/version/version.go` |
| 上传可 draft | 默认 `draft`，发布与设 latest 分离 |
| 非静默升级 | 须用户确认；turn 进行中须拦截 |

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

## 7. Client `/update`

- Python/Go TUI：确认后 `exec dagents update --from-client`
- Web UI：展示 `dagents update` 可复制命令

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
