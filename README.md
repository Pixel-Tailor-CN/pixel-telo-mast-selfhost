# Pixel Telo Mast Self-host

Pixel Telo 的自建实时号码查询服务。项目正在按
`pixel-telo-mast` 已验证的 source 优先级语义拆分公共 Query Core，Self-host 只查询本地显式启用的
Provider，并可选择同步官方离线库作为本地只读 baseline 缓存。

## 安全边界

- 实时查询不会代理到 Pixel Telo 官方 Mast。
- Self-host 不提供查询反馈、管理接口或 `/metrics`。
- 公共发行版不默认启用网页 Provider，部署者必须确认上游条款并显式配置。

## `phone.dat` 数据来源

号码归属地数据直接来源于 [pangongzi/phone](https://github.com/pangongzi/phone)，固定 Commit 为
`a0076a7cdfb5b44c53e70fac0bc46ef3ebb8bd80`；该项目注明其上游来源为
[ls0f/phone](https://github.com/ls0f/phone)。本项目使用的文件未修改，SHA-256 为
`5858836c6a472a706a690a55419d589ccbcd9e72721852642280d9e10e379bc2`。

`phone.dat` 作为 MIT 第三方数据分发，不适用本项目根目录的 Apache-2.0 许可证。完整归属和许可文本见
[`THIRD_PARTY_NOTICES.md`](THIRD_PARTY_NOTICES.md)。号码段数据可能过时、不完整或存在误差，不应作为
紧急服务、身份验证、计费、合规或人身安全决策的唯一依据。

## 运行方式

```bash
mast-selfhost init --dir /data
mast-selfhost serve --config /data/config.yaml
mast-selfhost pairing --config /data/config.yaml
```

服务端只使用配置中显式列出的 `upstream.provider_ids`，实时查询不会请求官方 Mast 查询地址。
`baseline.enabled` 可选用固定官方 HTTPS 地址同步离线库；启用 `sync_on_start` 时同步失败会阻止 HTTP
监听，周期同步失败会继续使用上一个成功版本。Runtime 数据保存在 SQLite 且不设置 TTL。

除 `GET /api/health` 外，Self-host API 均要求 Bearer Token：

- `GET /api/selfhost/v1/info`
- `GET /api/v1/query?number=...`
- `GET /api/v2/sources`
- `POST /api/v2/query`

Self-host 不提供反馈、管理、离线生成、清理或 `/metrics` 路由，也不会返回 `feedback_token`。
