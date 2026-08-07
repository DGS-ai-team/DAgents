# Manage 统一控制面镜像（构建上下文：仓库根目录）
#   docker build -f packaging/manage/Dockerfile -t dagents-manage:0.3.1 .
FROM node:22-bookworm-slim AS frontend
WORKDIR /src/manage/console/frontend
COPY manage/console/frontend/package.json manage/console/frontend/package-lock.json ./
RUN npm ci
COPY manage/console/frontend/ ./
RUN npm run build

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
# 与 Node go:embed 同源；member_tools 在 import 时读取（缺则 Manage 无法启动）
COPY shared/workgroup/member_tool_catalog.json ./shared/workgroup/member_tool_catalog.json
COPY --from=frontend /src/manage/console/static /app/manage/console/static
COPY packaging/manage/bundled-releases/ /app/bundled/releases/
COPY run_manage.py .

ENV MANAGE_HOST=0.0.0.0 \
    MANAGE_PORT=8020 \
    MANAGE_DB_PATH=/data/manage.db \
    MANAGE_RELEASES_DIR=/data/releases \
    MANAGE_BLOB_DIR=/data/blobs \
    MANAGE_AUDIT_PATH=/data/audit.jsonl \
    MANAGE_SEED_BUNDLED_RELEASES=1 \
    MANAGE_BUNDLED_RELEASES_DIR=/app/bundled/releases

EXPOSE 8020

HEALTHCHECK --interval=10s --timeout=3s --start-period=8s --retries=3 \
    CMD curl -sf http://127.0.0.1:8020/health || exit 1

CMD ["python", "run_manage.py"]
