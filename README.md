# MinoDuck

跨 Provider 的 AI 成本管理应用：汇总费用与用量、保留来源证据、核对账单差额，并比较有明确假设的价格方案。

## 本地开发

需要 Node.js 26.8.2、pnpm 12.3.4 和 Docker Compose；直接运行后端时使用 Go 1.27.1。版本以 `.node-version`、`package.json` 和 `services/backend/go.mod` 为准。Vue 类型检查使用 TypeScript 6.0.3 的 JavaScript 编译器接口。

```bash
npm install --global pnpm@12.3.4
pnpm install --frozen-lockfile
node scripts/setup-local.mjs
docker compose up --build
```

另开终端运行 `pnpm dev`：

| 服务         | 地址                         |
| ------------ | ---------------------------- |
| 官网         | http://localhost:3000        |
| 控制台       | http://localhost:3001        |
| API 就绪探针 | http://localhost:8080/readyz |

开发环境未配置 Resend 时，登录页提供一次性验证入口；生产环境必须配置邮件服务。Google 登录和 Stripe 按需配置，未配置 Stripe 不会授予付费权益。

手动同步在控制台触发；定时扫描运行 `docker compose --profile jobs run --rm scheduler`。生产 Cron 每五分钟执行一次。若在本机运行后端，先启动数据库并执行迁移，再在 `services/backend` 加载本地环境文件，分别运行 `go run ./cmd/api` 与 `go run ./cmd/worker`。

## 功能

- OpenAI、Anthropic、OpenRouter 和通用 CSV；十进制金额、费用更正、来源版本与证据。
- 成本筛选、账单参考、费用对账（L2）、文本用量与历史价格对账（L1）、预算提醒和 CSV 导出。
- Free、Starter、Team 与人工开通的 Business；Stripe 月付/年付、套餐变更、取消和失败宽限期。
- 英文、简体和繁体；浅色、深色和跟随系统；Workspace 角色、会话验证和 PostgreSQL RLS。

价格方案只提供基于样本和假设的比较，不自动切换推理流量。Provider 费用与 MinoDuck 订阅分别计费。

## 工程结构

| 路径                              | 用途                                         |
| --------------------------------- | -------------------------------------------- |
| `apps/site`                       | Nuxt 官网、语言路由与 SEO                    |
| `apps/console`                    | Nuxt 控制台与同源 BFF                        |
| `packages/ui`、`packages/locales` | 共享组件、样式与语言文案                     |
| `services/backend`                | Go API、River Worker、Cron、数据库迁移与测试 |
| `infra`                           | PostgreSQL 角色初始化、Cloudflare 根域重定向 |

## 验证

```bash
pnpm verify
cd services/backend
go mod verify
go vet ./...
# 使用独立测试数据库，不要连接生产库。
TEST_DATABASE_URL='postgres://postgres:postgres@localhost:5432/minoduck_test?sslmode=disable' go test -race -count=1 ./...
go build ./cmd/...
# 修改 schema 或 queries 后，使用 sqlc 1.31.1：
sqlc generate
```

未设置 `TEST_DATABASE_URL` 时数据库集成测试会跳过。GitHub CI 使用 PostgreSQL 18.6，运行完整后端测试与两个前端的类型检查、生产构建。

## 文档

- [架构与业务规则](docs/architecture.md)：信任边界、金额口径、计费和数据生命周期。
- [数据契约](docs/data-contract.md)：Provider 范围、CSV 和 HTTP 约定；接口定义见 [OpenAPI](docs/openapi.json)。
- [部署手册](docs/deployment.md)：Cloudflare、Render、R2、邮件、Google 和 Stripe 配置。
- [上线检查](docs/acceptance.md)：自动测试覆盖、外部验收和产品边界。
