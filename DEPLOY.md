# Mast Self-host 运维手册

只想在家里尽快跑起来、接到 Pixel Telo，请先看 [`README.md`](README.md)。

本文面向已经熟悉 Linux、容器、DNS、TLS 或反向代理的部署者，说明如何把 Self-host 配成可长期运行的实例。本文不提供反馈、管理、Prometheus 或官方实时查询代理。

发布页：<https://github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/releases>。仓库里的 `docker-compose.yml` 使用 `latest`，方便家庭用户跟随发布。需要可复现部署时，把镜像标签改成完整版本，例如 `v0.1.5`。

可用镜像（内容相同）：

```text
mystery0/pixel-telo-mast-selfhost:latest
ghcr.io/pixel-tailor-cn/pixel-telo-mast-selfhost:latest
```

二进制附件：

- `mast-selfhost-linux-amd64`
- `mast-selfhost-linux-arm64`
- `mast-selfhost-windows-amd64.exe`

Windows 安装包与家庭局域网最短路径见 README。`docker-compose.yml` 默认使用 `tls.mode=auto`；启动前设置 `MAST_PUBLIC_URL` 为手机能访问的 HTTPS 根 URL。前置反代应改用 `--tls-mode off`，不要把后端端口直接暴露到公网。

## 1. 部署前决策

部署前需要确定：

1. 对外地址使用域名还是公网 IP。
2. TLS 由 Self-host 直接提供，还是由前置入口终止。
3. 启用哪些网页来源（Provider），并确认其条款、访问政策和许可。
4. 数据目录、备份目录和运行账户。
5. 固定使用的镜像或二进制版本。

推荐优先级：

- 有域名和可信证书：使用受公共 CA 信任的域名证书。
- 只能使用公网 IP：申请 SAN 包含该 IP 的可信 IP 证书。
- 私有或测试环境：使用 Self-host 自动生成的自签名证书，并通过配对信息校验 SPKI Pin。

`init` 默认生成 `tls.mode=auto`。监听 `127.0.0.1` 时会自动填 `https://127.0.0.1:8443`。监听 `0.0.0.0` 且未传 `--public-url` 时：若在终端里运行，会列出本机网卡地址并可选查询公网 IP 供选择；非交互环境（Compose、CI）会打印检测到的地址并失败，需要显式传入 `--public-url`。容器内列出的地址通常不是手机能访问的宿主机 IP。前置反代使用 `--tls-mode off --allow-insecure-private-network`。`serve` 启动后会在控制台列出已配置 URL 和本机检测到的其它候选地址。

## 2. 网络模型

### 2.1 Self-host 直接提供 TLS

```text
客户端
  |
  | HTTPS
  v
Mast Self-host
```

适用于：

- 使用自动生成的自签名证书。
- 已有可直接挂载的域名或 IP 证书。
- 希望配对命令直接计算 Self-host 证书的 SPKI Pin。

### 2.2 前置入口终止 TLS

```text
客户端
  |
  | HTTPS
  v
前置 TLS 入口
  |
  | 受控私有网络中的 HTTP
  v
Mast Self-host
```

适用于已有统一证书签发、续期和公网入口的环境。此时 Self-host 使用 `tls.mode=off`，但只允许在受控私有网络中使用，后端端口不得直接暴露到公网。

前置入口必须：

- 保留 Authorization Header。
- 保留原始 Host。
- 向后端使用 HTTP。
- 将外部 HTTPS URL 写入 `tls.public_url`。
- 不记录完整请求体、Token 或号码。

此模式下 `pairing` 无法读取前置入口的证书，因此 `spki_pin` 为空。客户端必须依赖系统信任验证证书。

## 3. 数据目录

Self-host 是单进程、单持久化目录服务。数据目录保存：

- `config.yaml`
- Bearer Token
- `runtime.db` 及 WAL/SHM
- Instance ID
- 自动 TLS 证书和私钥
- 可选 baseline 数据
- `logs/mast.log`

建议使用专用目录：

```bash
sudo install -d -m 0700 -o 65532 -g 65532 /srv/mast-selfhost
```

容器镜像使用非 root UID/GID 65532。使用 bind mount 时必须保证该用户可以写入数据目录。

不要把数据目录放到不支持可靠文件锁和共享内存语义的网络文件系统。Runtime SQLite 强制使用 WAL。

## 4. 使用容器部署

```bash
export MAST_IMAGE=mystery0/pixel-telo-mast-selfhost:latest
export MAST_DATA=/srv/mast-selfhost
```

初始化配置：

```bash
docker run --rm \
  -v "${MAST_DATA}:/app/data" \
  "${MAST_IMAGE}" \
  init --dir /app/data \
  --if-missing \
  --listen 0.0.0.0:8443 \
  --public-url https://mast.example.com:8443 \
  --provider-id sogou \
  --provider-id 360 \
  --sync-on-start=false
```

`init` 只生成 `config.yaml`，不生成 Token、Runtime 数据库、证书或 Instance ID，也不会覆盖已有配置。`--public-url` 必须是没有 path、query 或 userinfo 的 HTTPS 根 URL，并与客户端实际访问地址一致。手机访问时不能填 `127.0.0.1`。已有证书时使用 `--tls-mode files`，再编辑 `cert_file` / `key_file`。

启动服务：

```bash
docker run -d \
  --name mast-selfhost \
  --restart unless-stopped \
  -p 8443:8443 \
  -v "${MAST_DATA}:/app/data" \
  "${MAST_IMAGE}" \
  serve --dir /app/data
```

查看日志：

```bash
docker logs --tail 100 mast-selfhost
```

停止服务：

```bash
docker stop mast-selfhost
```

镜像没有 Shell。配置编辑、备份和证书管理应在宿主机数据目录完成。

## 5. 使用二进制部署

为 Self-host 创建专用低权限账户，并准备数据目录：

```bash
sudo useradd --system --home /var/lib/mast-selfhost --shell /usr/sbin/nologin mast-selfhost
sudo install -d -m 0700 -o mast-selfhost -g mast-selfhost /var/lib/mast-selfhost
```

把 Release 中对应架构的二进制放到 PATH，初始化：

```bash
sudo -u mast-selfhost mast-selfhost init \
  --dir /var/lib/mast-selfhost \
  --listen 0.0.0.0:8443 \
  --public-url https://mast.example.com:8443 \
  --provider-id sogou \
  --provider-id 360 \
  --sync-on-start=false
```

检查配置后再启动：

```bash
sudo -u mast-selfhost mast-selfhost serve --dir /var/lib/mast-selfhost
```

生产环境应使用服务管理器负责自动启动、异常重启、SIGTERM 优雅关闭和日志收集。不要以 root 身份长期运行。仓库不提供 systemd 或 Windows Service 示例。

## 6. 完整配置示例

### 6.1 直接使用可信证书

```yaml
server:
  listen: "0.0.0.0:8443"
auth:
  token_file: "/app/data/token"
tls:
  mode: "files"
  public_url: "https://mast.example.com:8443"
  cert_file: "/app/data/tls/fullchain.pem"
  key_file: "/app/data/tls/private.key"
  allow_insecure_private_network: false
storage:
  runtime_path: "/app/data/runtime.db"
baseline:
  enabled: true
  sync_on_start: false
  check_interval: "24h"
query:
  timeout: "2s"
  max_concurrent: 8
rate_limit:
  requests_per_second: 2
  burst: 10
upstream:
  provider_ids:
    - "sogou"
    - "360"
providers:
  sogou:
    min_interval: "500ms"
    max_concurrent: 2
    breaker_timeout: "30s"
  "360":
    min_interval: "500ms"
    max_concurrent: 2
    breaker_timeout: "30s"
log:
  level: "info"
  format: "json"
  rotation:
    max_size_mb: 100
    daily: true
    local_time: true
    compress: true
  retention:
    max_age: "720h"
    max_backups: 30
    max_total_size_mb: 1024
```

YAML 使用严格解码，未知字段会导致启动失败。`query.max_concurrent` 和 `rate_limit.*` 会作用于实例查询限流；`providers.<id>` 只影响对应网页来源的间隔、并发和熔断时间。`query.max_concurrent` 不会改变单个 Provider 的 semaphore。

### 6.2 自动自签名证书

```yaml
server:
  listen: "0.0.0.0:8443"
tls:
  mode: "auto"
  public_url: "https://203.0.113.10:8443"
  cert_file: ""
  key_file: ""
  allow_insecure_private_network: false
```

首次成功启动时会生成证书和私钥。证书有效期为 365 天，SAN 来自 `public_url` 的域名或 IP。

自动模式不会自动续期。证书过期、`public_url` 改变或 SAN 不匹配时，服务会拒绝启动。更换地址前应先停止服务，备份并安全处理旧证书文件。

### 6.3 前置入口终止 TLS

```yaml
server:
  listen: "0.0.0.0:8080"
tls:
  mode: "off"
  public_url: "https://mast.example.com"
  cert_file: ""
  key_file: ""
  allow_insecure_private_network: true
```

`allow_insecure_private_network` 只是显式确认风险，不会自动验证网络是否私有。必须用防火墙和网络策略阻止公网直接访问后端端口。

## 7. 申请域名证书

### 7.1 准备 DNS

为服务创建 A 或 AAAA 记录：

```text
mast.example.com → 服务公网 IP
```

等待公共 DNS 生效后检查：

```bash
dig +short mast.example.com A
dig +short mast.example.com AAAA
```

### 7.2 选择验证方式

常见 ACME 验证方式：

- HTTP-01：签发时公网 80 端口必须能够响应挑战。
- DNS-01：通过 DNS TXT 记录验证，不要求服务开放 80，适合通配符证书。
- TLS-ALPN-01：签发期间需要控制公网 443。

应使用支持自动续期和部署钩子的 ACME 客户端。签发结果至少包括：

- 完整证书链 `fullchain.pem`
- 与证书匹配的私钥 `private.key`

配置 `tls.mode=files` 后，Self-host 会在启动时检查证书有效期，并在设置 `public_url` 时验证 SAN。

### 7.3 自动续期

证书续期后，运行中的进程不会自动重新加载证书。续期钩子必须：

1. 原子替换证书和私钥文件。
2. 保持运行账户可读、其他用户不可读。
3. 重启或滚动重启 Self-host。
4. 验证健康检查和证书有效期。

不要让续期脚本把私钥内容写入标准输出或日志。

## 8. 申请 IP 证书

IP 证书必须在 Subject Alternative Name 中包含精确公网 IP，仅设置 Common Name 不够。

申请前确认 CA 或 ACME 服务明确支持 IP Identifier。不同 CA 对 IP 地址所有权验证、证书有效期和自动续期有不同限制。不能用域名证书代替 IP SAN 证书。

签发后检查：

```bash
openssl x509 -in fullchain.pem -noout -dates -ext subjectAltName
```

预期 SAN 包含：

```text
IP Address:203.0.113.10
```

直接由 Self-host 提供 IP TLS 时：

```yaml
tls:
  mode: "files"
  public_url: "https://203.0.113.10:8443"
  cert_file: "/app/data/tls/fullchain.pem"
  key_file: "/app/data/tls/private.key"
```

若前置入口管理 IP 证书，Self-host 使用 `tls.mode=off`，并把 `public_url` 写成客户端实际访问的 HTTPS IP URL。

## 9. Provider 与容量

`upstream.provider_ids` 决定允许查询的来源及默认优先级。列表顺序就是优先级顺序。当前可注册 ID 是 `sogou` 和 `360`。未知 ID 会阻止启动。

`providers` 下的参数含义：

- `min_interval`：同一来源两次实际请求开始的最小间隔。
- `max_concurrent`：同一来源最大并发。
- `breaker_timeout`：连续失败达到阈值后的熔断时间。连续失败阈值是代码常量 3，不能通过 YAML 修改。

未配置时默认并发 1、最小间隔 0、熔断 30 秒。提高实例限流不代表上游允许同等请求速率。公开服务应从保守参数开始，根据 429、Retry-After、超时和上游政策逐步调整。

默认查询总超时为 2 秒，最大允许配置为 10 秒。不要为了掩盖上游不稳定而盲目提高超时。

## 10. 首次启动与配对

启动后检查公开首页和健康检查：

```bash
# 自签名证书加 -k；可信证书可去掉 -k
curl -k --fail --show-error https://mast.example.com:8443/
curl -k --fail --show-error https://mast.example.com:8443/api/health
```

首页是静态状态页，不展示 Token、Instance ID 或 Commit。健康检查返回 `{"status":"ok"}`。这两条路由不需要 Bearer Token。

首次成功启动后输出配对信息：

```bash
docker run --rm \
  -v "${MAST_DATA}:/app/data" \
  "${MAST_IMAGE}" \
  pairing --dir /app/data
```

二进制部署：

```bash
mast-selfhost pairing --dir /var/lib/mast-selfhost
```

配对输出格式：

```text
url=<public-url> token=<token> instance_id=<uuid> spki_pin=sha256/<base64>
```

该输出必须按密码处理，不得进入日志、工单或公开剪贴板历史。`tls.mode=off` 时 `spki_pin` 为空。

## 11. 接口验证

公开接口：

```bash
curl -k --fail --show-error https://mast.example.com:8443/api/health
```

鉴权接口：

```bash
read -rsp "Token: " MAST_TOKEN
echo

curl -k --fail --show-error \
  -H "Authorization: Bearer ${MAST_TOKEN}" \
  https://mast.example.com:8443/api/selfhost/v1/info

curl -k --fail --show-error \
  -H "Authorization: Bearer ${MAST_TOKEN}" \
  https://mast.example.com:8443/api/v2/sources
```

验证查询时使用合法测试号码，不要把完整号码或响应写入共享日志：

```bash
read -rp "Test phone number: " TEST_NUMBER

curl -k --fail --show-error \
  -H "Authorization: Bearer ${MAST_TOKEN}" \
  -H "Content-Type: application/json" \
  --data "{\"number\":\"${TEST_NUMBER}\",\"sources\":[\"360\",\"sogou\"]}" \
  https://mast.example.com:8443/api/v2/query
```

除公开首页 `GET /` 和 `GET /api/health` 外，所有接口都必须携带 Bearer Token。查询接口另外受实例限流。

## 12. 防火墙与安全加固

- 直接 TLS 模式只开放实际 HTTPS 监听端口。
- 前置 TLS 模式只允许可信入口访问后端 HTTP 端口。
- 限制数据目录权限，特别是 Token 和私钥。
- 内置访问日志只记录方法、路由模板、状态码、耗时和 Request ID，不记录完整 URL、Header 或请求体。
- 不把 Token、私钥、数据库或证书提交到 Git。
- 不提供反馈、管理、离线生成、清理或 `/metrics` 路由。
- 不配置到官方 Mast 实时查询接口的失败回退。
- 定期检查 Provider 条款、访问策略和实际限流响应。

## 13. 配置变更

配置使用严格 YAML 解码，未知字段会导致启动失败。修改后先停止服务并备份配置，再重启验证。

以下变更需要特别处理：

- `public_url` 改变：检查证书 SAN。
- 自动 TLS 地址改变：旧证书不会自动重签。
- Provider 列表改变：同步检查 `providers` 参数。
- 数据目录改变：必须同时迁移 Token、Runtime DB 和其他运行文件。
- 前置 TLS 与直接 TLS 切换：同步修改监听端口、防火墙和客户端 URL。

## 14. 升级

升级前：

1. 阅读目标版本发布说明。
2. 备份完整数据目录。
3. 固定目标版本 Tag 或校验过的摘要。
4. 保留旧镜像或二进制以便回滚。

容器升级示例：

```bash
docker pull mystery0/pixel-telo-mast-selfhost:<VERSION>
docker stop mast-selfhost
docker rm mast-selfhost

docker run -d \
  --name mast-selfhost \
  --restart unless-stopped \
  -p 8443:8443 \
  -v "${MAST_DATA}:/app/data" \
  mystery0/pixel-telo-mast-selfhost:<VERSION> \
  serve --dir /app/data
```

升级后验证：

- 健康检查。
- info 中的版本和 API Version。
- sources 列表。
- 一次受控查询。
- Instance ID 未意外变化。
- Token 仍然有效。

## 15. 备份与恢复

Runtime 使用 SQLite WAL。不要在持续写入时只复制 `runtime.db`。

一致备份流程：

1. 停止 Self-host。
2. 确认进程已经退出。
3. 备份整个数据目录。
4. 加密保存备份并限制访问。
5. 重新启动服务。
6. 验证健康、Instance ID 和鉴权接口。

```bash
docker stop mast-selfhost
tar -C /srv -czf mast-selfhost-backup.tar.gz mast-selfhost
docker start mast-selfhost
```

备份包含 Token 和可能存在的 TLS 私钥，应按高敏感秘密管理。

恢复时：

1. 停止服务。
2. 恢复完整数据目录和权限。
3. 确认配置中的绝对路径仍然有效。
4. 启动服务。
5. 验证 Instance ID、Token、证书和查询。

## 16. 常见故障

### 16.1 启动前退出

常见原因：

- `config.yaml` 不存在或包含未知字段。
- 未配置任何 Provider。
- 非 Loopback 明文监听未显式允许，且 `tls.mode=off`。
- Token 文件存在但内容无效。
- 证书过期、私钥不匹配或 SAN 与 `public_url` 不符。
- 数据目录不可写。
- `baseline.sync_on_start=true` 时官方同步失败。

### 16.2 健康正常但返回 401

- 使用了错误实例的 Token。
- Token 前后包含额外字符。
- 前置入口移除了 Authorization Header。

### 16.3 返回 429

可能来自实例限流或 Provider 限流。检查 Retry-After，并降低调用速率。不要用自动重试形成请求风暴。

### 16.4 返回 503 或 504

- 503：Provider 不可用、解析失败或没有可靠结果。
- 504：查询超过总超时。

失败时不得自动把号码发送到官方 Mast 实时查询接口。

### 16.5 pairing URL 不正确

显式设置 `tls.public_url`。前置 TLS 模式必须填写客户端实际访问的 HTTPS 根 URL，不能使用后端 HTTP 地址。家庭局域网必须填电脑的局域网 IP 或可解析域名，不能填 `127.0.0.1`。
