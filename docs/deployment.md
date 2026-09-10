# 部署与运行

本仓库不会自动部署或创建付费资源。Render Blueprint 和 Cloudflare workflow 是可 review 的生产配置入口。首次发布前完成 acceptance.md 中的外部验收。

## 1. PostgreSQL 和 Render

1. 导入 `render.yaml`，数据库/API/Worker/Cron 使用同一区域。配置为常驻付费实例；确认费用后再创建资源。
2. 迁移连接 `MIGRATION_DATABASE_URL` 使用建表账号。为运行进程另建 `minoduck_app`，属性 `NOSUPERUSER NOBYPASSRLS NOCREATEDB NOCREATEROLE`，授予 public schema 的 DML/sequence 权限及迁移账户的 default privileges。不要把 Blueprint 自动创建的管理员连接直接填到运行进程。
3. 环境组 `DATABASE_URL` 使用运行角色。`BFF_SERVICE_TOKEN` 为至少 32 字符随机值；`PROVIDER_ENCRYPTION_MASTER_KEY` 为 `openssl rand -base64 32` 的结果，存放密钥管理系统。
4. API pre-deploy 执行 `/app/migrate`，同时应用应用表和 River 迁移。迁移完成后启动 Worker 和每五分钟运行的 scheduler。只把 API 绑定 `api.minoduck.ai`。
5. `/healthz` 是进程状态；`/readyz` 包含数据库连通检查。生产启动会拒绝会绕过 RLS 的数据库角色。

新实例使用 PostgreSQL 18，Compose 与 CI 固定 18.6。Render 为 18.x 托管小版本更新。已有 Render 17 实例需先完成备份、测试升级，再按 [Render 官方流程](https://render.com/docs/postgresql-upgrading) 升级；仅修改 Blueprint 不会迁移现有数据。

River 从 0.23 升至 0.47 包含新的队列迁移（含 migration 7）；必须先停止旧 Worker，运行新版 `/app/migrate`，再启动新版 Worker。Worker 收到退出信号后最多等待 20 秒完成当前任务，随后取消任务上下文；应用等待退出最多 30 秒，Compose 留出 35 秒。被退出中断的任务保留重试状态，实际失败和超时仍遵循重试上限。

### 本地已有 PostgreSQL 17 数据

PostgreSQL 18 官方镜像将持久目录改为 `/var/lib/postgresql`。新版 Compose 使用独立的 `postgres18-data` 卷，保留原 `postgres-data` 卷；不会自动搬运数据。已有本地数据时按顺序操作：

```bash
# 更新代码前，停止写入并用旧版 PostgreSQL 17 导出。
docker compose stop api worker scheduler
docker compose exec -T postgres pg_dump -U minoduck_migrator -d minoduck -Fc > ../minoduck-pg17.dump
docker compose stop postgres
# 更新到新版代码后，只启动新的空 PostgreSQL 18 数据库。
docker compose up -d --wait postgres
docker compose exec -T postgres pg_restore --exit-on-error --no-owner --no-acl -U minoduck_migrator -d minoduck < ../minoduck-pg17.dump
docker compose up --build
```

恢复只对新的空库执行一次；保留备份与旧卷，核对数据和权限后再自行清理。不要把旧版数据目录直接挂到 18 镜像，也不要在迁移前执行 `down -v`。目录变化见 [PostgreSQL 官方镜像说明](https://hub.docker.com/_/postgres)。

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

| 环境变量                       | 单价（cents） | 周期  |
| ------------------------------ | ------------: | ----- |
| `STRIPE_PRICE_STARTER_MONTHLY` |          2900 | month |
| `STRIPE_PRICE_STARTER_YEARLY`  |         29000 | year  |
| `STRIPE_PRICE_TEAM_MONTHLY`    |          7900 | month |
| `STRIPE_PRICE_TEAM_YEARLY`     |         79000 | year  |

后端使用官方 `stripe-go/v86` 86.4.2，API 版本由 SDK 固定为 `2026-08-26.dahlia`。为新的 Webhook 端点选择同版本的 **snapshot events**；当前处理器使用事件内的对象 ID，然后重新读取 Stripe 订阅状态。端点 `https://api.minoduck.ai/api/v1/webhooks/stripe`，订阅事件：

- `checkout.session.completed`
- `customer.subscription.created`、`customer.subscription.updated`、`customer.subscription.deleted`
- `invoice.paid`、`invoice.payment_failed`

配置 `STRIPE_SECRET_KEY`、`STRIPE_WEBHOOK_SECRET`、四个 Price ID。创建独立 Customer Portal configuration，允许更新支付方式、账单记录，**禁用 Portal 订阅换档**；设置 `STRIPE_PORTAL_CONFIGURATION`。套餐变更走应用接口，以保持立即升级/期末降级策略一致。

使用 Stripe CLI 转发测试 Webhook 时，目标是 Go 的 `localhost:8080/api/v1/webhooks/stripe`，使用该 CLI 会话产生的 signing secret。成功返回页不代表订阅已生效，页面可刷新核实服务端状态。

如已有旧版本端点，先在 sandbox 验证新版 API 的 Checkout、升级、期末降级、取消、付款失败和事件回放，再切换端点版本。SDK 最多重试两次瞬时网络/API 故障，重用业务幂等键；每个请求含重试的总时限为 25 秒。验签保留正负五分钟窗口和密钥轮换支持。仓库测试使用合成 Stripe 响应，真实支付验收仍按 `acceptance.md` 执行。

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
