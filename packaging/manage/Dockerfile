# Manage 统一控制面镜像（构建上下文：仓库根目录）
#   docker build -f packaging/manage/Dockerfile -t dagents-manage:0.3.0 .
FROM python:3.12-slim-bookworm

ARG VERSION=dev
LABEL org.opencontainers.image.title="dagents-manage" \
      org.opencontainers.image.description="DAgents Manage control plane (Registry + A2A)" \
      org.opencontainers.image.version="${VERSION}"

RUN apt-get update \
    && apt-get install -y --no-install-recommends curl ca-certificates \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

COPY manage/ ./manage/
COPY run_manage.py .

ENV MANAGE_HOST=0.0.0.0 \
    MANAGE_PORT=8020 \
    MANAGE_DB_PATH=/data/manage.db

EXPOSE 8020

HEALTHCHECK --interval=10s --timeout=3s --start-period=8s --retries=3 \
    CMD curl -sf http://127.0.0.1:8020/health || exit 1

CMD ["python", "run_manage.py"]
