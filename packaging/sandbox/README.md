# Agent 沙箱镜像（backend=docker）

**操作系统**：Alpine Linux 3.20（`FROM alpine:3.20`，musl）。

用于 `sandbox.backend: docker`：

1. Agent **装入内存**时预创建常驻容器（`docker create` + `start`，`sleep infinity`）
2. `bash_run` 经 `docker exec` 进入容器
3. **空闲 15 分钟**无命令则回收容器（Agent 仍可在内存；下次 bash 再 Ensure）
4. Agent **卸出内存 / 删除**时立即 `docker rm -f`

工作区挂载：宿主机 `agents/<id>/data` → 容器 `/workspace`。

## 构建

```bash
docker build -t dagents-sandbox:latest -f packaging/sandbox/Dockerfile packaging/sandbox
```

## Agent 配置示例

```yaml
sandbox:
  enabled: true
  backend: docker
  fs_root_isolation: true
  allow_bash: true
  image: dagents-sandbox:latest
  network: none
  memory: 512m
  cpus: "1.0"
```

无 Docker 时创建/启用返回 `docker_unavailable`。
