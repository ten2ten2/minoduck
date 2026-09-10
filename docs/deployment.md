# 部署与运行

本仓库不会自动部署或创建付费资源。Render Blueprint 和 Cloudflare workflow 是可 review 的生产配置入口。首次发布前完成 acceptance.md 中的外部验收。

## 1. PostgreSQL 和 Render

1. 导入 `render.yaml`，数据库/API/Worker/Cron 使用同一区域。配置为常驻付费实例；确认费用后再创建资源。
2. 迁移连接 `MIGRATION_DATABASE_URL` 使用建表账号。为运行进程另建 `minoduck_app`，属性 `NOSUPERUSER NOBYPASSRLS NOCREATEDB NOCREATEROLE`，授予 public schema 的 DML/sequence 权限及迁移账户的 default privileges。不要把 Blueprint 自动创建的管理员连接直接填到运行进程。
3. 环境组 `DATABASE_URL` 使用运行角色。`BFF_SERVICE_TOKEN` 为至少 32 字符随机值；`PROVIDER_ENCRYPTION_MASTER_KEY` 为 `openssl rand -base64 32` 的结果，存放密钥管理系统。
4. API pre-deploy 执行 `/app/migrate`，同时应用应用表和 River 迁移。迁移完成后启动 Worker 和每五分钟运行的 scheduler。只把 API 绑定 `api.minoduck.ai`。
5. `/healthz` 是进程状态；`/readyz` 包含数据库连通检查。生产启动会拒绝会绕过 RLS 的数据库角色。

已有表时授予运行角色权限；以下只展示授权，不包含生产密码：

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

## 2. 私有 R2、邮件和 Google

- R2 bucket 必须私有，不打开公共 r2.dev/custom domain。设置 `R2_ENDPOINT=https://<account-id>.r2.cloudflarestorage.com`、bucket 和仅该 bucket 的 S3 API 访问凭据。API/Worker 共用配置；所有文件通过鉴权 API 下载。
- Resend 验证发送域名，配置 `RESEND_API_KEY`、`MAIL_FROM`。为生产发送域配置 SPF/DKIM/DMARC，并验证三语言邮件可投递。
- Google OAuth 的授权回调精确注册为 `https://app.minoduck.ai/api/v1/auth/google/callback`，配置 client ID/secret；前端不接触 secret。本地另加 `http://localhost:3001/api/v1/auth/google/callback`。
- 邮件一次性链接与原浏览器 nonce 绑定，15 分钟有效；Google 使用 state、nonce、PKCE。生产 session 为 `__Host-md_session`，HttpOnly/Secure/SameSite=Lax，无跨子域 cookie。

## 3. Stripe

先在 sandbox 配置并验证，live 与 test 的四个 Price ID 和 Webhook Secret 必须分别管理。新建两个 Product、四个 USD recurring Price：

| 环境变量 | 单价（cents） | 周期 |
|---|---:|---|
| `STRIPE_PRICE_STARTER_MONTHLY` | 2900 | month |
| `STRIPE_PRICE_STARTER_YEARLY` | 29000 | year |
| `STRIPE_PRICE_TEAM_MONTHLY` | 7900 | month |
| `STRIPE_PRICE_TEAM_YEARLY` | 79000 | year |

后端固定 Stripe API `2025-06-30.basil`。Webhook 端点 `https://api.minoduck.ai/api/v1/webhooks/stripe`，订阅事件：

- `checkout.session.completed`
- `customer.subscription.created`、`customer.subscription.updated`、`customer.subscription.deleted`
- `invoice.paid`、`invoice.payment_failed`

配置 `STRIPE_SECRET_KEY`、`STRIPE_WEBHOOK_SECRET`、四个 Price ID。创建独立 Customer Portal configuration，允许更新支付方式、账单记录，**禁用 Portal 订阅换档**；设置 `STRIPE_PORTAL_CONFIGURATION`。套餐变更走应用接口，以保持立即升级/期末降级策略一致。

使用 Stripe CLI 转发测试 Webhook 时，目标是 Go 的 `localhost:8080/api/v1/webhooks/stripe`，使用该 CLI 会话产生的 signing secret。成功返回页不代表订阅已生效，页面可刷新核实服务端状态。

上线前按真实商户资格确定主体、税务、发票、退款规则和联系渠道；本仓库的 Terms/Privacy 是明确标注的发布前草案。退款通过经授权的 Stripe 后台流程进行，本版不提供自动退款 API。

## 4. Cloudflare

1. 核验并接入 `minoduck.ai` DNS 区域，确认 www/app/api 实际归属和 TLS。
2. 在 Console Worker 配置私密 `NUXT_BFF_SERVICE_TOKEN`，值与 Render 的 `BFF_SERVICE_TOKEN` 一致。`NUXT_API_ORIGIN=https://api.minoduck.ai`。不要使用 `NUXT_PUBLIC_` 暴露密钥。
3. GitHub production environment 配置 `CLOUDFLARE_API_TOKEN`、`CLOUDFLARE_ACCOUNT_ID`，设置 `CONTACT_EMAIL` variable 为真实支持邮箱。
4. 手动运行 `Deploy Cloudflare`。Workflow 会检查语言键、重定向和类型，并分别构建发布 www、app、根域重定向 Worker。已声明 custom domains，首次发布前核对域名绑定。
5. 核实 HTTPS 下登录、三语言路由、root 308 保留路径/查询、BFF session/CSRF、私有资源 no-store/noindex。Cloudflare 访问日志应排除认证 query、cookie、邮件和账单正文；关闭可能记录敏感请求 URL 的默认日志收集，再按 request ID/route/status 建立脱敏监控。

`wrangler secret put NUXT_BFF_SERVICE_TOKEN` 需在 `apps/console` 下运行。发布前 build 无需密钥；Worker 运行时从 Cloudflare secret/env 读取，不能把密钥打入客户端。

## 5. 运维和回滚

- 监控 readyz、River 队列延迟/失败、同步错误、Stripe 重试、邮件失败及数据库/R2容量。单条同步最长 12 分钟、分片 7 日，超大任务应缩短范围重试。
- 数据库启用备份/PITR，私有对象与数据库按同一恢复窗口核验；凭据主密钥必须独立安全备份。
- 删除清单 `deletion_tombstones` 应复制到备份之外的持久审计介质。恢复后先合并最新删除清单，重跑删除任务，核对对象和财务行均消失，再恢复流量。这是上线演练步骤，目前未宣称真实恢复已验证。
- 同步断开后保留历史证据，Workspace 删除才删除所属数据。导出24小时；未提交上传7天；财务数据按套餐保留，降级30天清理缓冲。
- 回滚应用使用上一个已验证 commit/image。迁移仅前向；对不可逆 schema 变更先备份并进行兼容发布，不自动 down migration。
