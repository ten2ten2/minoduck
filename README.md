# MinoDuck

AI 账单成本管理 MVP：把来源费用、用量和账单参考放在一起，保留证据与版本，解释差额，再评估有假设约束的价格候选。

本版依据 `MinoDuck_MVP_Execution_Spec_v1.0_zh-Hans.md` 与 `minoduck-mvp-pricing-stripe.md` 实现；价格及支付冲突以 Stripe 文档为准。**可本地运行，生产服务尚未部署；真实 Provider、支付和中文专属账单模板验收状态见 [验收记录](docs/acceptance.md)。**

## 已实现

- Nuxt 4 官网与控制台：英文、简体、繁体；浅色、深色、跟随系统；官网语言路由、SEO、根域 308。
- 邮件一次性登录、Google OIDC/PKCE、服务端 session/CSRF、Workspace 和 Owner/Admin/Viewer。
- OpenAI、Anthropic、OpenRouter 连接器；加密凭据；River 异步同步、重试、分片 checkpoint、断开防回写。
- CSV 校验、预览、确认更正、幂等导入；十进制金额、四种成本口径、原币种、来源版本和历史。
- 成本总览、筛选、分页、来源详情、账单参考登记、L2 对账；历史价格与文本用量 L1 对账。
- 预算、突增、同步失败提醒、三语言邮件；价格候选与估算假设；异步 CSV 导出。
- Stripe Checkout、Portal、Webhook 验签/去重/状态核对、按比例升级、期末降级、取消与失败宽限期。
- PostgreSQL RLS、迁移、sqlc 核心身份查询、私有 R2、删除任务和保留期清理；Docker、Render 和 Cloudflare 配置。

## 本地启动

需要 Node.js 26.8.2、pnpm 12.3.4、Docker Compose。直接运行 Go 时使用 Go 1.27.1。Node 版本由 `.node-version` 固定，CI 与本地一致。

```bash
npm install --global pnpm@12.3.4
pnpm install --frozen-lockfile
node scripts/setup-local.mjs
# 启动 PostgreSQL、迁移、API、Worker
# 首次构建会下载 Go 依赖。
docker compose up --build
```

另开终端：

```bash
pnpm dev
```

官网 `http://localhost:3000`，控制台 `http://localhost:3001`，API 探针 `http://localhost:8080/readyz`。开发模式未配置 Resend 时，登录页面会显示一次性验证入口。生产模式禁用此入口，必须配置邮件服务。Google 登录与 Stripe 按需配置；未配置时相关请求返回明确错误，不授予付费权限。

不运行 Docker 中的 API/Worker 时，可只启动数据库与迁移，然后在 `services/backend` 使用 `set -a; source .env; set +a` 加载开发配置，分别运行 `go run ./cmd/api`、`go run ./cmd/worker`。手动同步通过控制台触发；定时扫描执行 `docker compose --profile jobs run --rm scheduler`，生产 Cron 每五分钟执行一次。

## 第一次使用

1. 邮件登录并建立 Workspace。
2. 在 Connections 建立 CSV 来源；从页面下载空模板，填入自己的去敏账单费用，上传后检查预览再提交。原生 API 来源需要对应组织管理凭据。
3. 在 Costs 查看原币种与来源版本；在 Invoices 登记账单总额、费用范围和有证据的调整项。
4. 运行 L2 对账。缺失来源或范围不完整会保持待确认；Free 可查看摘要，付费计划解锁详情、处理和导出。
5. L1/价格候选需要真实有效日期、完整计费维度和价格证据；系统不预置未经验证的“实时价格”。

## 套餐

| 计划     |     月付 | 年付总额 |     连接 | 成员 / Workspace |     历史 | 月管理支出 |
| -------- | -------: | -------: | -------: | ---------------: | -------: | ---------: |
| Free     |       $0 |        — |        2 |            1 / 1 |    30 天 |       $500 |
| Starter  |      $29 |     $290 |        5 |            3 / 1 |   180 天 |     $5,000 |
| Team     |      $79 |     $790 |       15 |           10 / 3 |   730 天 |    $25,000 |
| Business | 人工联系 | 人工联系 | 人工约定 |         人工约定 | 人工约定 |   人工约定 |

额度属于用户拥有的 Billing Account，覆盖其多个 Workspace。支出上限为软限制，无自动超额扣费；非 USD 不自动折算。Business 无自助购买入口。

## 工程结构

| 路径                              | 用途                                         |
| --------------------------------- | -------------------------------------------- |
| `apps/site`                       | 官网 SSR/预渲染、三语言、SEO                 |
| `apps/console`                    | 控制台 CSR、Nitro 同源 BFF                   |
| `packages/ui`, `packages/locales` | 共享组件、样式、语言文案                     |
| `services/backend`                | Gin API、River Worker、Cron、迁移、测试      |
| `services/backend/queries`        | sqlc 身份与租户查询；其余报表查询由 pgx 执行 |
| `infra`                           | PostgreSQL 角色、Cloudflare 根域重定向       |
| `docs/openapi.json`               | HTTP 路由、鉴权和核心请求契约                |

## 验证与部署

```bash
pnpm verify
cd services/backend
go mod verify
go vet ./...
go test -race ./...
# 集成测试需要可建表、建测试角色的独立测试数据库；不会使用生产库。
TEST_DATABASE_URL='postgres://postgres:postgres@localhost:5432/minoduck_test?sslmode=disable' go test -race -count=1 ./...
go build ./cmd/...
# 修改 queries 或 schema 后（sqlc v1.31.1）：
sqlc generate
```

GitHub CI 运行 PostgreSQL 18.6 集成测试及两个 Nuxt 的类型检查/构建。没有 `TEST_DATABASE_URL` 时数据库测试明确跳过。部署入口为 [部署手册](docs/deployment.md)，依赖版本、TypeScript 7 兼容限制和 API 调整见 [依赖升级记录](docs/dependency-upgrade.md)，设计取舍为 [架构决策](docs/architecture.md)，Provider 范围和 CSV 规范为 [数据契约](docs/data-contract.md)。
