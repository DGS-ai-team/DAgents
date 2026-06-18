# 构建上下文：仓库根目录（见 docker-compose.yml）
#
# 运行时 CentOS 7（glibc 2.17）；Node 为 CGO_ENABLED=0 静态二进制。
# syntax=docker/dockerfile:1

FROM golang:1.25-bookworm AS builder
WORKDIR /src
COPY go.work go.work.sum ./
COPY node/go.mod node/go.sum ./node/
COPY shared/config/go.mod shared/config/go.sum ./shared/config/
RUN cd node && go mod download
COPY node/ ./node/
COPY shared/config/ ./shared/config/
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-s -w" -o /out/dagents-node ./node/cmd/dagents-node

FROM centos:7
RUN sed -i 's/mirrorlist/#mirrorlist/g' /etc/yum.repos.d/CentOS-*.repo \
    && sed -i 's|#baseurl=http://mirror.centos.org|baseurl=http://vault.centos.org|g' /etc/yum.repos.d/CentOS-*.repo \
    && yum -y install curl bash ca-certificates file \
    && yum clean all
WORKDIR /workspace
COPY --from=builder /out/dagents-node /usr/local/bin/dagents-node
COPY packaging/runtime/skills /opt/dagents/seed/skills
COPY packaging/runtime/policy /opt/dagents/seed/policy
COPY cases/centos7-feature-tour/config.yaml /etc/dagents/config.yaml
COPY cases/centos7-feature-tour/scripts/entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod +x /usr/local/bin/entrypoint.sh /usr/local/bin/dagents-node
EXPOSE 18765
VOLUME ["/workspace/.runtime"]
ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
