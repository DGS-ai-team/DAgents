# Manage Docker 镜像

将 **Manage 统一控制面**（Registry + A2A Task + **Vue Console**）打包为可发布镜像，供生产部署与 [A2A 联调案例](../../cases/a2a-manage-docker/) 复用。镜像构建含 **Console 前端 `npm run build`** 多阶段步骤。

## 快速开始（联网环境）

```bash
# 仓库根目录
docker build -f packaging/manage/Dockerfile -t dagents-manage:0.3.7 .

# 或使用 compose（在 packaging/manage/ 下）
cp .env.example .env
docker compose up -d --build
```

- 健康检查：`curl -sf http://127.0.0.1:8020/health`
- Console：<http://127.0.0.1:8020/console/>

## 离线安装

适用于目标机 **无法拉取 Docker Hub / GitHub Release**、但已安装 Docker 的内网环境。

### 1. 在联网机准备镜像包

**方式 A — 从 GitHub Release 下载（推荐）**

打 `v*` 标签后 CI 会发布 **`dagents-manage-<version>.tar.gz`**，例如 `dagents-manage-0.3.7.tar.gz`。

**方式 B — 本地构建并导出**

```bash
# 仓库根目录
VERSION=0.3.7 bash scripts/ci/build_manage_docker.sh
# 产物：dist/dagents-manage-0.3.7.tar.gz
```

也可手动导出已构建镜像：

```bash
docker build -f packaging/manage/Dockerfile -t dagents-manage:0.3.7 .
docker save dagents-manage:0.3.7 | gzip -9 > dagents-manage-0.3.7.tar.gz
```

将 `dagents-manage-<version>.tar.gz` 拷贝到离线机（U 盘、内网文件服务等）。

### 2. 在离线机导入镜像

```bash
docker load -i dagents-manage-0.3.7.tar.gz
docker image ls dagents-manage
# 应看到 REPOSITORY dagents-manage，TAG 0.3.7
```

### 3. 启动服务

**方式 A — `docker run`（单容器）**

```bash
docker run -d \
  --name manage \
  --restart unless-stopped \
  -p 8020:8020 \
  -v manage-data:/data \
  -e MANAGE_HOST=0.0.0.0 \
  -e MANAGE_PORT=8020 \
  -e MANAGE_DB_PATH=/data/manage.db \
  dagents-manage:0.3.7
```

**方式 B — `docker compose`（推荐）**

将 `packaging/manage/` 目录（至少含 `docker-compose.yml`、`.env.example`）一并拷贝到离线机：

```bash
cd packaging/manage
cp .env.example .env
# 确认 .env 中 MANAGE_IMAGE=dagents-manage:0.3.7（与 load 的 tag 一致）
docker compose up -d
```

> 离线环境 **不要** 使用 `docker compose up --build`（无法拉取基础镜像）；仅 `up -d` 启动已导入的本地镜像。

### 4. 验证

```bash
curl -sf http://127.0.0.1:8020/health
docker logs manage --tail 20
```

浏览器打开 **`http://<主机>:8020/console/`** 查看 Node 目录。

### 5. 日常运维

```bash
docker compose -f packaging/manage/docker-compose.yml stop    # 停止
docker compose -f packaging/manage/docker-compose.yml start   # 启动
docker compose -f packaging/manage/docker-compose.yml logs -f   # 日志
```

数据持久化在 Docker volume **`manage-data`**（Registry + A2A SQLite：`/data/manage.db`）。升级镜像时保留 volume 即可，无需迁移数据库。

## 环境变量

| 变量 | 默认 | 说明 |
|------|------|------|
| `MANAGE_HOST` | `0.0.0.0` | 监听地址 |
| `MANAGE_PORT` | `8020` | 监听端口 |
| `MANAGE_DB_PATH` | `/data/manage.db` | Registry + A2A SQLite（建议挂载 volume） |
| `MANAGE_A2A_EXPIRE_SWEEP_SECONDS` | `30` | A2A Task TTL 扫描间隔；`0` 关闭后台扫描 |

完整列表见 [manage/README.md](../../manage/README.md)。

## 发布

打 **`v*`** 标签时 CI 会：

1. 构建 `dagents-manage:<version>` 镜像
2. 导出 `dist/dagents-manage-<version>.tar.gz` 并附到 GitHub Release

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
