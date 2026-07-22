# DAgents Agent 沙箱镜像
#
# 操作系统：Alpine Linux 3.20（musl libc）
# 用途：backend=docker 时常驻容器；bash_run 经 docker exec 进入；
#       工作区 bind-mount 为 /workspace。
#
# 构建：
#   docker build -t dagents-sandbox:latest -f packaging/sandbox/Dockerfile packaging/sandbox
#
# 运行时由 Node 以宿主机 uid 启动 sleep infinity，保持容器常驻。

FROM alpine:3.20

RUN apk add --no-cache \
      bash \
      coreutils \
      findutils \
      grep \
      sed \
      gawk \
      curl \
      git \
      python3 \
    && mkdir -p /workspace \
    && chmod 1777 /workspace

WORKDIR /workspace

# 常驻进程由 docker create ... sleep infinity 覆盖；此处仅作镜像默认。
CMD ["sleep", "infinity"]
