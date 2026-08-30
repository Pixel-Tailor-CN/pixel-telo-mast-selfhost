# Pixel Telo Mast Self-host

自己在家里或小服务器上跑的**来电标记查询服务**，给 [Pixel Telo](https://github.com/Pixel-Tailor-CN/PixelTelo) 用。

手机来电时，App 可以问这台服务：这个号码有没有被网页来源标记成骚扰。查询只发给你自己启用的来源（目前是搜狗、360），**不会**把号码转到 Pixel Telo 官方实时查询。

## 先看哪份说明

| 你想做什么 | 看哪里 |
| --- | --- |
| 不想懂原理，先在家里跑起来 | 下面的「快速开始」 |
| Fork 后部署到 Vercel（推荐长期使用） | 下面的「部署到 Vercel」 |
| 域名证书、反代、限流、备份升级、Vercel 限制 | [`DEPLOY.md`](DEPLOY.md) |

快速开始使用镜像标签 `latest`。需要钉死版本时，请到 [Releases](https://github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/releases) 选完整版本号。

## 你需要准备什么

- 一台几乎一直开着的电脑：家里的 Linux / Windows，或已安装 Docker 的 NAS。
- 手机和这台电脑在**同一局域网**（先别急着弄公网域名）。
- 电脑的局域网 IP，例如 `192.168.1.8`。Windows 可在终端运行 `ipconfig`，macOS / Linux 可运行 `ipconfig getifaddr en0` 或 `hostname -I`。
- 启用搜狗或 360 前，请自己确认其网页服务条款；公共发行版不会替你默认打开来源。

快速开始使用自动生成的自签名证书。手机第一次连接时可能会提示证书不受系统信任，需要按 App 的配对方式核对证书指纹。这是预期行为，不是装错了。

## 快速开始：Docker Compose（推荐）

适合已安装 Docker 的 Linux、macOS、Windows（Docker Desktop）或 NAS。把 `192.168.1.8` 换成**这台电脑的局域网 IP**。

Linux / macOS：

```bash
git clone https://github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost.git
cd pixel-telo-mast-selfhost
export MAST_PUBLIC_URL=https://192.168.1.8:8443
docker compose up -d
```

Windows PowerShell：

```powershell
git clone https://github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost.git
cd pixel-telo-mast-selfhost
$env:MAST_PUBLIC_URL="https://192.168.1.8:8443"
docker compose up -d
```

镜像使用 `latest`。`init` 默认就是自动 HTTPS，只要带上手机能访问的 `MAST_PUBLIC_URL`，不用再改配置文件。

```bash
docker compose logs --tail 50
```

日志里会有 5 分钟有效的配对页面地址，用浏览器打开即可复制或扫码。

## 快速开始：Docker 命令

没有 compose 时也可以直接跑。容器里看到的网卡地址通常不是手机能用的，所以这里仍要填**宿主机局域网 IP**。

```bash
export MAST_IMAGE=mystery0/pixel-telo-mast-selfhost:latest
# 若 Docker Hub 拉不到，可改用：
# export MAST_IMAGE=ghcr.io/pixel-tailor-cn/pixel-telo-mast-selfhost:latest
export MAST_DATA="$HOME/mast-selfhost"
mkdir -p "$MAST_DATA"

docker run --rm \
  -v "${MAST_DATA}:/app/data" \
  "${MAST_IMAGE}" \
  init --dir /app/data \
  --listen 0.0.0.0:8443 \
  --public-url https://192.168.1.8:8443 \
  --provider-id sogou \
  --sync-on-start=false

docker run -d \
  --name mast-selfhost \
  --restart unless-stopped \
  -p 8443:8443 \
  -v "${MAST_DATA}:/app/data" \
  "${MAST_IMAGE}" \
  serve --dir /app/data
```

看是否起来：

```bash
docker logs --tail 50 mast-selfhost
```

日志里应出现 `self-host server started`。浏览器打开 `https://192.168.1.8:8443/` ，可能会提示证书不受信任，继续访问后应看到「运行正常」。

健康检查（自签名证书需要 `-k`）：

```bash
curl -k --fail --show-error https://192.168.1.8:8443/api/health
```

应返回 `{"status":"ok"}`。

服务启动成功后，终端会打印一个**限时 5 分钟**的配对页面地址（用配置里的 `public_url`）。用电脑或手机浏览器打开，页面上有复制按钮和二维码，把内容填进 Pixel Telo。不要把页面或密钥发到群里。

超过 5 分钟后页面失效，可再执行 `pairing` 查看文字（不会刷新网页链接，需在有效期内打开启动时打印的地址，或重启 `serve`）：

```bash
docker run --rm \
  -v "${MAST_DATA}:/app/data" \
  "${MAST_IMAGE}" \
  pairing --dir /app/data
```

Windows Docker Desktop 把上面的 `"$HOME/mast-selfhost"` 换成例如 `D:\mast-selfhost` 即可。

## 快速开始：Windows 安装包

1. 打开 [Releases](https://github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/releases/latest)，下载 `mast-selfhost-windows-amd64.exe`。
2. 新建文件夹，例如 `D:\mast-selfhost`，把 exe 放进去。
3. 在该文件夹打开终端：

```bat
mast-selfhost-windows-amd64.exe init --dir data --listen 0.0.0.0:8443 --provider-id sogou --sync-on-start=false
```

4. 没加 `--public-url` 时，命令会列出网卡地址让你选。选和手机同一 Wi-Fi 的那条，不要选 `127.0.0.1`。
5. 启动：

```bat
mast-selfhost-windows-amd64.exe serve --dir data
```

6. 保持这个窗口开着。启动成功后会打印配对页面地址，用浏览器打开即可复制或扫码，不必再开窗口。
7. 若 5 分钟内没打开，可另开终端执行 `pairing` 查看文字，或重启 `serve` 拿到新的页面链接：

```bat
mast-selfhost-windows-amd64.exe pairing --dir data
```

Windows 防火墙如果询问，允许专用网络访问。手机必须能访问配对页上的地址，不要用 `127.0.0.1`。

## 部署到 Vercel

适合不想在家里长期开电脑的人。推荐先 Fork 官方仓库，再把自己的 Fork 导入 Vercel。这样 GitHub 会保留 Fork 关系，以后可以直接同步上游并触发自动升级。这不是官方查询代理，失败时也**不会**把号码转到 Pixel Telo 官方实时查询。

部署步骤：

1. 打开 [Fork 页面](https://github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/fork)，把仓库 Fork 到自己的 GitHub 账号。
2. 在 Vercel 选择 **Add New → Project**，导入刚创建的 Fork；只需填写 `MAST_TOKEN` 和 `MAST_PROVIDER_IDS`，然后部署。Token 可用 `openssl rand -hex 32` 生成；Provider 可填写 `sogou`、`360` 或 `sogou,360`，启用前请自行确认网页服务条款。
3. 第一次部署后服务还不能使用：Vercel 可能显示 deployment 已完成，但应用缺少数据库。打开 [Neon Marketplace](https://vercel.com/marketplace/neon)，点击 **Install**，连接刚创建的项目并选择 **Free** 套餐。
4. 返回 Vercel 的 **Deployments** 页面，对刚才的 deployment 点击 **Redeploy**。Neon 会自动提供 `DATABASE_URL`；Go Framework、数据库 migration、版本和 Commit 也会自动处理。部署成功后，把 HTTPS 根地址和同一个 `MAST_TOKEN` 手工填入 Pixel Telo。

以后升级时，打开自己 Fork 的 GitHub 页面，点击 **Sync fork → Update branch**。Vercel 会自动部署新版本，并继续使用原来的环境变量和 Neon 数据库。

Vercel 模式和家里的 Docker / 二进制**不是同一套能力**：

- 没有配对页，也没有 SPKI Pin。要在 Pixel Telo 里手工填写 HTTPS 地址和 Token。
- 没有官方 baseline，也没有本地 SQLite。
- HTTPS 由 Vercel 提供，应用不管理证书。
- 限流、Provider 并发和熔断只对**单个 Go 进程**有效，多实例之间不共享。

三个环境变量都必须由部署者填写，**没有默认 Provider**：

| 变量 | 要求 |
| --- | --- |
| `DATABASE_URL` | PostgreSQL 连接串。不要把完整值写进文档、截图或日志。 |
| `MAST_TOKEN` | Bearer Token，去掉首尾空白后至少 32 字节。服务端**不会**自动生成。 |
| `MAST_PROVIDER_IDS` | 逗号分隔的来源 ID，例如 `sogou` 或 `sogou,360`。不能留空。启用前请自己确认网页服务条款。 |

可用 `openssl rand -hex 32` 生成 Token。当前可填写的来源仍是 `sogou` 和 `360`。

仓库根目录的 [`vercel.json`](vercel.json) 明确选择 Go Framework Preset，且不包含路径 rewrite。构建脚本会把当前 Git commit 和最近可追溯的 Release tag 注入 `cmd/api`：正式 Tag 显示如 `0.2.0`，Tag 后的提交显示如 `0.2.0-dev+e9fcfc8`。更完整的 Fork 导入、手工部署、免费层检查、变量说明和排错见 [`DEPLOY.md`](DEPLOY.md)。

## 走不通时先看这里

| 现象 | 常见原因 |
| --- | --- |
| `init` 提示 public URL is required | 监听写成了 `0.0.0.0`，但没加 `--public-url`（或 compose 没设 `MAST_PUBLIC_URL`） |
| 配对 URL 是 `127.0.0.1` | compose 第一次启动时没设 `MAST_PUBLIC_URL`。`--if-missing` 不会改已有配置，需要改 `config.yaml` 或清空数据后重来 |
| 启动失败，说至少要一个 provider | `init` 时没加 `--provider-id`，或后来把列表清空了 |
| 电脑浏览器能开，手机不行 | `public_url` 写成了 `127.0.0.1`，或手机不在同一局域网，或防火墙挡住 8443 |
| 配对命令报 Token 不存在 | 还没成功启动过一次 `serve`。Token 是第一次成功启动时才生成的 |
| 查询很慢或 429 | 网页来源限流了。先等一等，不要狂点刷新 |

更多排错、域名证书、反代和备份见 [`DEPLOY.md`](DEPLOY.md)。

## 这台服务不会做什么

- 不会把号码转发到官方实时查询。
- 没有网页管理后台，没有账号系统。家里人或手机共用同一个 Token。
- 没有查询反馈、没有 Prometheus、没有 `/metrics`。
- 公网不要用裸 HTTP。快速开始用的是自签名 HTTPS，只适合家庭局域网。要挂到公网，请按 [`DEPLOY.md`](DEPLOY.md) 换可信证书、前置反代，或使用 Vercel 托管 HTTPS。

浏览器打开首页 `https://你的地址:8443/` 和健康检查 `/api/health` 不需要 Token。其余接口都要 Token。

## 号码归属地数据

内嵌 `phone.dat` 来自 [pangongzi/phone](https://github.com/pangongzi/phone)，固定 Commit `a0076a7cdfb5b44c53e70fac0bc46ef3ebb8bd80`，上游为 [ls0f/phone](https://github.com/ls0f/phone)。文件未修改，SHA-256 为 `5858836c6a472a706a690a55419d589ccbcd9e72721852642280d9e10e379bc2`。

该数据按 MIT 分发，不适用本仓库根目录的 Apache-2.0。完整文本见 [`THIRD_PARTY_NOTICES.md`](THIRD_PARTY_NOTICES.md)。号码段可能过时或有误差，不能作为紧急服务、身份验证、计费或人身安全的唯一依据。
