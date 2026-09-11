# 部署与运行

官网与控制台部署到 Cloudflare Workers，API、Worker 和 Cron 部署到 Render。Render Blueprint 使用常驻付费实例；部署前核对配置、域名和费用。

## PostgreSQL 与 Render

1. 导入 `render.yaml`，数据库、API、Worker、Cron 使用同一区域。数据库使用 PostgreSQL 18；Compose 与 CI 固定为 18.6。
2. `MIGRATION_DATABASE_URL` 使用迁移账户。另建 `minoduck_app`，属性为 `NOSUPERUSER NOBYPASSRLS NOCREATEDB NOCREATEROLE`，运行进程的 `DATABASE_URL` 使用此角色，并且不要授予 `public` schema 的 `CREATE` 权限。不要把数据库管理员或迁移账户连接用于 API/Worker。
3. 配置 `BFF_SERVICE_TOKEN` 为至少 32 字符的随机值；`PROVIDER_ENCRYPTION_MASTER_KEY` 使用 `openssl rand -base64 32` 生成，并保存到密钥管理系统。
4. API pre-deploy 执行 `/app/migrate`，应用业务表与 River 迁移。迁移完成后启动 API/Worker，Cron 每五分钟执行 scheduler。只把 API 绑定到 `api.minoduck.ai`。
5. `/healthz` 检查进程，`/readyz` 检查数据库连接；生产启动会拒绝 superuser、BYPASSRLS 以及拥有 `public` schema `CREATE` 权限的运行角色，避免误把 migrator 用作 runtime。

以下权限同时覆盖已有表与随后由迁移账户创建的表：

```sql
GRANT CONNECT ON DATABASE minoduck TO minoduck_app;
GRANT USAGE ON SCHEMA public TO minoduck_app;
GRANT SELECT,INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA public TO minoduck_app;
GRANT USAGE,SELECT ON ALL SEQUENCES IN SCHEMA public TO minoduck_app;
ALTER DEFAULT PRIVILEGES FOR ROLE minoduck_migrator IN SCHEMA public
 GRANT SELECT,INSERT,UPDATE,DELETE ON TABLES TO minoduck_app;
ALTER DEFAULT PRIVILEGES FOR ROLE minoduck_migrator IN SCHEMA public
 GRANT USAGE,SELECT ON SEQUENCES TO minoduck_app;
```

Compose 的数据库卷挂到 `/var/lib/postgresql`。Worker 收到退出信号后先等待任务完成，20 秒后取消任务上下文，最多等待 30 秒退出；Compose 留出 35 秒。中断的任务可以重试，实际失败仍受重试次数限制。

## 私有 R2、邮件与 Google

| 服务   | 配置                                                                                                                                                                 |
| ------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| R2     | 私有 bucket；`R2_ENDPOINT=https://<account-id>.r2.cloudflarestorage.com`、bucket 名称和仅限该 bucket 的 S3 凭据。API/Worker 共用配置，不启用公共 r2.dev 或媒体域名。 |
| Resend | 验证发送域，设置 `RESEND_API_KEY`、`MAIL_FROM`；配置 SPF/DKIM/DMARC，验证三语言邮件投递。                                                                            |
| Google | 设置 OAuth client ID/secret；回调为 `https://app.minoduck.ai/api/v1/auth/google/callback`。本地另注册 `http://localhost:3001/api/v1/auth/google/callback`。          |

相关变量按服务必须成套配置：R2 的 endpoint/bucket/access key/secret key、Resend 的 key/from、Google 的 client ID/secret 缺任一项都会在进程启动时直接报错；完全不配置时 development 才会使用本地对象存储和开发登录流程。`APP_ENV` 只接受 `development` 或 `production`，未知值不会退化成 development。生产 session 为 `__Host-md_session`，带 HttpOnly、Secure、SameSite=Lax；前端不接触服务端密钥。

## Stripe

先使用 sandbox 验证，test/live 分别管理 Price ID、API key 与 Webhook secret。创建两个 Product、四个 USD recurring Price：

| 环境变量                       | 单价（cents） | 周期  |
| ------------------------------ | ------------: | ----- |
| `STRIPE_PRICE_STARTER_MONTHLY` |          2900 | month |
| `STRIPE_PRICE_STARTER_YEARLY`  |         29000 | year  |
| `STRIPE_PRICE_TEAM_MONTHLY`    |          7900 | month |
| `STRIPE_PRICE_TEAM_YEARLY`     |         79000 | year  |

Stripe 配置采用 all-or-nothing：`STRIPE_SECRET_KEY`、`STRIPE_WEBHOOK_SECRET`、`STRIPE_PORTAL_CONFIGURATION` 与四个 Price ID 必须全部存在，否则后端拒绝启动，避免出现 Checkout 可用但 Webhook/Portal 不可用的半配置状态。Webhook 使用 SDK 对应的 API 版本 `2026-08-26.dahlia`，选择 **snapshot events**，端点为 `https://api.minoduck.ai/api/v1/webhooks/stripe`，订阅：

- `checkout.session.completed`
- `customer.subscription.created`、`customer.subscription.updated`、`customer.subscription.deleted`
- `invoice.paid`、`invoice.payment_failed`

创建独立 Customer Portal configuration，允许更新支付方式、查看账单，禁用 Portal 订阅换档。套餐变更由应用处理，确保立即升级、期末降级的规则一致。

Stripe CLI 本地转发目标为 `localhost:8080/api/v1/webhooks/stripe`，使用当前 CLI 会话的 signing secret。付款成功页只提示确认中；权益以 Webhook 验签后重新读取的 Stripe 状态为准。

## Cloudflare

1. 接入 `minoduck.ai` DNS，核对 www/app/api 归属和 TLS。根域 Worker 将路径与查询参数一并 308 到 www。
2. Console Worker 设置私密 `NUXT_BFF_SERVICE_TOKEN`，与 Render 的 `BFF_SERVICE_TOKEN` 相同；设置 `NUXT_API_ORIGIN=https://api.minoduck.ai`。密钥不使用 `NUXT_PUBLIC_` 前缀。
3. GitHub `production` environment 配置 `CLOUDFLARE_API_TOKEN`、`CLOUDFLARE_ACCOUNT_ID`，并将 `CONTACT_EMAIL` variable 设为真实支持邮箱。
4. 手动运行 `Deploy Cloudflare`。Workflow 检查语言键、重定向和类型，并由各自 Wrangler build hook 构建发布官网、控制台与根域 Worker。
5. 验证 session/CSRF、私有资源 no-store/noindex、HSTS、anti-frame、nosniff 与 Referrer-Policy；访问日志排除认证 query、cookie、邮件、凭据和账单正文，只记录脱敏的 request ID、route 与 status。

在 `apps/console` 运行 `wrangler secret put NUXT_BFF_SERVICE_TOKEN` 设置 secret。构建不需要密钥，Worker 在运行时读取配置。

## 运维与恢复

- 监控 readyz、队列延迟/失败、同步错误、支付回调、邮件失败和数据库/R2 容量。
- 启用 PostgreSQL 备份/PITR，按同一恢复窗口核对数据库与私有对象；独立备份凭据主密钥。
- 将 `deletion_tombstones` 复制到备份之外的审计介质。恢复后先合并最新删除清单、重跑删除任务，确认对象与财务行已删除，再开放访问。
- 数据库迁移只前向执行。应用回滚使用已验证的镜像；不可逆 schema 操作必须先备份。
- 保留期与删除规则见 [架构说明](architecture.md)，上线前执行 [验收清单](acceptance.md)。
