# Docker 单容器部署

根目录 `Dockerfile` 把 `seed`、`gameserver`、`gateway` 和独立 Web 管理后台打包进同一个镜像。容器内使用非 root 用户，网关 HTTPS 监听 `8443`，宿主机通过端口映射对外提供 `443`。

## 构建

在仓库根目录执行：

```sh
docker build \
  --build-arg GIT_COMMIT="$(git rev-parse --short HEAD)" \
  --build-arg SOURCE_DIR="$PWD" \
  -t rf4-server-single .
```

构建时的 `VERSION` / `BUILD_TS` 默认由构建时间生成；也可以显式传入 UTC 时间戳 `BUILD_TS=yyyyMMddHHmmss` 和对应的 `VERSION`。这些值会出现在各进程的 `rf4_build_info` 指标中。

## 启动

以下环境变量必须提供：

- `RF4_GATEWAY_ADVERTISE_HOST`：客户端用于 realtime 连接的本机或局域网 IPv4。
- `RF4_ADMIN_PASSWORD`：Web 管理后台管理员密码。
- `RF4_ADMIN_JWT_SECRET`：管理后台 JWT 签名密钥。
- `RF4_TUNNEL_PSK` 与 `RF4_GAME_TUNNEL_PSK`：两者必须相同。

```sh
docker run -d --name rf4-server-single \
  -e RF4_GATEWAY_ADVERTISE_HOST=127.0.0.1 \
  -e RF4_ADMIN_PASSWORD='replace-with-strong-password' \
  -e RF4_ADMIN_JWT_SECRET='replace-with-long-random-secret' \
  -e RF4_TUNNEL_PSK='replace-with-shared-random-secret' \
  -e RF4_GAME_TUNNEL_PSK='replace-with-shared-random-secret' \
  -p 127.0.0.1:443:8443 \
  -p 127.0.0.1:7515:7515 \
  -p 127.0.0.1:9341:9341 \
  -p 127.0.0.1:8080:8080 \
  -v rf4-player-data:/app/data \
  -v rf4-gateway-certs:/app/certs \
  rf4-server-single
```

容器启动时先运行 `seed`，成功后启动 gameserver，再启动 gateway 和 admin。内置健康检查会同时验证三个服务的 `/readyz`。`docker ps` 显示 `healthy` 后再连接客户端。

管理后台地址为 `http://127.0.0.1:8080/`，默认用户名是 `admin`。客户端接入、hosts 和证书信任步骤见[部署指南](docs/deploy.md#客户端接入)。复制网关证书：

```sh
docker cp rf4-server-single:/app/certs/server.crt ./server.crt
```

玩家数据库和 HTTPS 证书分别保存在 `rf4-player-data`、`rf4-gateway-certs` 命名卷中。删除容器不会删除存档；只有执行 `docker volume rm rf4-player-data rf4-gateway-certs` 才会清空。
