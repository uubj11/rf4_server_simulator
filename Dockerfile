# ---------- 构建阶段 ----------
FROM golang:1.27.3-alpine3.24 AS builder
WORKDIR /app

# 国内 Go 模块代理，避免 go mod download 卡住。
ENV GOPROXY=https://goproxy.cn,direct

# 编译目标平台。
ENV CGO_ENABLED=0
ENV GOOS=linux

# 先复制依赖清单，利用 Docker 缓存。
COPY go.mod go.sum ./
RUN go mod download

# 根包通过 go:embed 提供 README，构建 admin 时需要。
COPY readme.go README.md ./
COPY cmd/ ./cmd/
COPY internal/ ./internal/

ARG VERSION=
ARG BUILD_TS=
ARG GIT_COMMIT=unknown
ARG SOURCE_DIR=docker
RUN build_ts="${BUILD_TS:-$(date -u +%Y%m%d%H%M%S)}" && \
    version="${VERSION:-$build_ts}" && \
    CGO_ENABLED=0 go build -trimpath \
    -ldflags="-s -w -X 'rf4server/internal/buildinfo.Version=${version}' -X 'rf4server/internal/buildinfo.BuildTs=${build_ts}' -X 'rf4server/internal/buildinfo.GitCommit=${GIT_COMMIT}' -X 'rf4server/internal/buildinfo.SourceDir=${SOURCE_DIR}'" \
    -o /app/ ./cmd/gateway ./cmd/gameserver ./cmd/admin ./cmd/seed

# ---------- 运行阶段 ----------
FROM alpine:3.24

RUN apk --no-cache add ca-certificates tzdata netcat-openbsd && \
    addgroup -S -g 10001 rf4 && \
    adduser -S -D -H -u 10001 -G rf4 rf4

WORKDIR /app

COPY --from=builder /app/gateway    ./
COPY --from=builder /app/gameserver ./
COPY --from=builder /app/admin      ./
COPY --from=builder /app/seed       ./
COPY config.example.yaml ./config.yaml
COPY server_data/ ./server_data/
COPY start.sh ./
RUN sed -i 's/\r$//' start.sh && chmod +x start.sh && \
    mkdir -p /app/data /app/certs && chown -R rf4:rf4 /app/data /app/certs

ENV RF4_GATEWAY_ADVERTISE_HOST=""
ENV RF4_GATEWAY_LOGIN_ADDR=":8443"
ENV RF4_GATEWAY_LOGIN_COMPAT_ADDR=":7515"
ENV RF4_ADMIN_PASSWORD=""
ENV RF4_ADMIN_JWT_SECRET=""
ENV RF4_TUNNEL_PSK=""
ENV RF4_GAME_TUNNEL_PSK=""

EXPOSE 8080 8443 7515 9341 9501 9502 9503
VOLUME /app/data /app/certs
STOPSIGNAL SIGTERM
HEALTHCHECK --interval=5s --timeout=3s --start-period=20s --retries=10 \
  CMD wget -q -O /dev/null http://127.0.0.1:9501/readyz && \
      wget -q -O /dev/null http://127.0.0.1:9502/readyz && \
      wget -q -O /dev/null http://127.0.0.1:9503/readyz || exit 1

USER 10001:10001
ENTRYPOINT ["/bin/sh", "/app/start.sh"]
