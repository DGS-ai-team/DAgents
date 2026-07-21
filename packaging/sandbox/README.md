# Agent 沙箱镜像（backend=docker）

用于 `sandbox.backend: docker`：Node 将 Agent 工作区 bind-mount 到容器 `/workspace`，
`bash_run` 经 `docker run --rm` 在容器内执行。

## 构建

```bash
docker build -t dagents-sandbox:latest -f packaging/sandbox/Dockerfile packaging/sandbox
```

## Agent 配置示例

```yaml
sandbox:
  enabled: true
  backend: docker
  fs_root_isolation: true   # docker 时强制开启
  allow_bash: true
  image: dagents-sandbox:latest
  network: none             # 默认无网络
  memory: 512m              # 可选
  cpus: "1.0"               # 可选
```

无 Docker 时创建/启用 `backend: docker` 的 Agent 会返回 `docker_unavailable`；请改用 `process` 或安装 Docker。
