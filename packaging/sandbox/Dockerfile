# DAgents Agent 沙箱镜像
# 仅用于 backend=docker：bash_run 在容器内执行，工作区挂载为 /workspace。
#
# 构建：
#   docker build -t dagents-sandbox:latest -f packaging/sandbox/Dockerfile packaging/sandbox
#
# 默认以非 root（nobody）运行；镜像内提供 bash 与常用基础工具。

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
    && chown -R nobody:nobody /workspace

WORKDIR /workspace
USER nobody

CMD ["bash"]
