# Manage Docker 镜像

将 **Manage 统一控制面**（Registry + A2A Task + **Vue Console**）打包为可发布镜像，供生产部署与 [A2A 联调案例](../../cases/a2a-manage-docker/) 复用。镜像构建含 **Console 前端 `npm run build`** 多阶段步骤。

## 快速开始

```bash
# 仓库根目录
docker build -f packaging/manage/Dockerfile -t dagents-manage:0.3.1 .

# 或使用 compose（在 packaging/manage/ 下）
cp .env.example .env
docker compose up -d --build
```

- 健康检查：`curl -sf http://127.0.0.1:8020/health`
- Console：<http://127.0.0.1:8020/console/>

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

离线加载：

```bash
docker load -i dagents-manage-0.3.1.tar.gz
docker run -d --name manage -p 8020:8020 -v manage-data:/data dagents-manage:0.3.1
```

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
