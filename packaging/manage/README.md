# Manage Docker 镜像

将 **Manage 统一控制面**（Registry + A2A Task + **Vue Console**）打包为可发布镜像，供生产部署与 [A2A 联调案例](../../cases/a2a-manage-docker/) 复用。镜像构建含 **Console 前端 `npm run build`** 多阶段步骤。

## 快速开始（联网环境）

```bash
# 仓库根目录
docker build -f packaging/manage/Dockerfile -t dagents-manage:0.5.1 .

# 或使用 compose（在 packaging/manage/ 下）
cp .env.example .env
docker compose up -d --build
```

- 健康检查：`curl -sf http://127.0.0.1:8020/health`
- Console：<http://127.0.0.1:8020/console/>

## 离线安装

适用于目标机 **无法拉取 Docker Hub / GitHub Release**、但已安装 Docker 的内网环境。

### 推荐：离线 bundle（镜像 + 脚本）

打 **`v*`** 标签后 CI 会发布 **`dagents-manage-bundle-<version>.tar.gz`**，内含：

| 路径 | 说明 |
|------|------|
| `image/dagents-manage-<version>.tar.gz` | Docker 镜像 |
| `docker-compose.yml` | 离线 compose（无 build） |
| `.env.example` | 环境变量模板 |
| `scripts/import-image.sh` / `.bat` | 离线 `docker load` |
| `scripts/restart.sh` / `.bat` | 启动或重启容器 |

```bash
# 联网机构建 bundle
VERSION=0.5.1 bash scripts/ci/assemble_manage_bundle.sh
# 产物：dist/dagents-manage-bundle-0.5.1.tar.gz

# 离线机
tar -xzf dagents-manage-bundle-0.5.1.tar.gz
cd dagents-manage-bundle-0.5.1
cp .env.example .env
bash scripts/import-image.sh
bash scripts/restart.sh
```

Windows（Docker Desktop）：解压后执行 `scripts\import-image.bat`，再 `scripts\restart.bat`。

### 1. 在联网机准备镜像包

**方式 A — 从 GitHub Release 下载 bundle（推荐）**

例如 `dagents-manage-bundle-0.5.1.tar.gz`。

**方式 B — 仅镜像 tar.gz**

Release 亦附 **`dagents-manage-<version>.tar.gz`**（纯镜像，无脚本）。

**方式 C — 本地构建**

```bash
VERSION=0.5.1 bash scripts/ci/assemble_manage_bundle.sh
# 或仅镜像：VERSION=0.5.1 bash scripts/ci/build_manage_docker.sh
```

### 2. 在离线机导入镜像

使用 bundle 内脚本（推荐）：

```bash
bash scripts/import-image.sh
```

或手动：

```bash
docker load -i image/dagents-manage-0.5.1.tar.gz
docker image ls dagents-manage
```

### 3. 启动服务

```bash
cp .env.example .env
bash scripts/restart.sh
```

**方式 B — `docker compose`（开发/联网）**

### 4. 验证

```bash
curl -sf http://127.0.0.1:8020/health
docker logs manage --tail 20
```

浏览器打开 **`http://<主机>:8020/console/`** 查看 Node 目录。

### 5. 日常运维

```bash
bash scripts/restart.sh          # bundle 内：启动或重启
docker compose stop              # 开发目录 packaging/manage/
docker compose start
docker compose logs -f
```

数据持久化在 Docker volume **`dagents-manage-data`**（容器内挂载 `/data`；Compose 内别名 `manage-data`）。升级 bundle 版本**不会**再新建 volume。

| 路径 | 用途 |
|------|------|
| `/data/manage.db` | Registry、A2A、Skills、案例库等 SQLite |
| `/data/releases` | 本地助手安装包 |
| `/data/blobs` | Skills / External Tools / Plugins / 案例附件等内容寻址存储 |
| `/data/audit.jsonl` | 审计事件追加日志（内存环形缓冲仍保留最近 N 条） |

升级镜像时保留 volume 即可，**不要用** `docker compose down -v`。

## 环境变量

| 变量 | 默认 | 说明 |
|------|------|------|
| `MANAGE_HOST` | `0.0.0.0` | 监听地址 |
| `MANAGE_PORT` | `8020` | 监听端口 |
| `MANAGE_DB_PATH` | `/data/manage.db` | Registry + A2A SQLite（建议挂载 volume） |
| `MANAGE_RELEASES_DIR` | `/data/releases` | Release Hub 安装包目录 |
| `MANAGE_BLOB_DIR` | `/data/blobs` | Blob 根目录（Skills / 外置工具 / 案例附件等） |
| `MANAGE_AUDIT_PATH` | `/data/audit.jsonl` | 审计 JSONL 追加路径 |
| `MANAGE_BLOB_MAX_BYTES` | （空=不限制） | 单 Blob 上传上限（字节） |
| `MANAGE_AUDIT_MAX_ENTRIES` | `500` | 内存审计环形缓冲条数 |
| `MANAGE_A2A_EXPIRE_SWEEP_SECONDS` | `30` | A2A Task TTL 扫描间隔；`0` 关闭后台扫描 |

完整列表见 [manage/README.md](../../manage/README.md)。

## 发布

打 **`v*`** 标签时 CI 会：

1. 构建 `dagents-manage:<version>` 镜像
2. 组装 `dist/dagents-manage-bundle-<version>.tar.gz`（镜像 + 离线脚本）
3. 同时导出 `dist/dagents-manage-<version>.tar.gz` 并附到 GitHub Release

## 与 Node 联调

Go Node 配置示例：

```yaml
manage:
  enabled: true
  url: http://127.0.0.1:8020
  registration:
    base_url: http://<node-可达地址>:18765
    interval_seconds: 30
    ttl_seconds: 60
  a2a:
    enabled: true
```

双 Node A2A 场景见 [cases/a2a-manage-docker/README.md](../../cases/a2a-manage-docker/README.md)。
