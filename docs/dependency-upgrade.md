# 依赖与 API 升级记录

核对日期：2026-09-10。以 npm registry、Go module proxy 和项目官方 release 为版本依据；直接依赖固定版本，提交 pnpm 与 Go 锁定文件。配置更新不执行生产部署或迁移现有数据库。

## 版本

| 依赖 / 工具                  | 升级前                      | 本次版本                       |
| ---------------------------- | --------------------------- | ------------------------------ |
| Node.js                      | 24                          | 26.8.2（Current 稳定版）       |
| pnpm                         | 11.19.0                     | 12.3.4                         |
| TypeScript                   | ^5.9.3                      | 6.0.3，兼容例外见下文          |
| Vue                          | ^3.5.30                     | 3.5.42                         |
| Vue Router                   | ^5.0.3                      | 5.3.1                          |
| 图标                         | lucide-vue-next ^0.577.0    | @lucide/vue 1.44.0             |
| Wrangler                     | ^4.70.0                     | 4.131.0                        |
| Prettier                     | ^3.6.2                      | 3.9.6                          |
| @types/node                  | ^24.0.0                     | 26.5.1，与 Node 26 匹配        |
| Nuxt / i18n                  | 4.5.2 / 10.6.0              | 已为最新稳定版，保持           |
| Tailwind / Vite 插件         | 4.3.3                       | 已为最新稳定版，保持           |
| vue-tsc                      | 3.3.11                      | 已为最新稳定版，保持           |
| Go                           | go.mod 1.24.0 / 镜像 1.26.1 | 1.27.1                         |
| Gin                          | 1.10.1                      | 1.12.0                         |
| pgx                          | 5.7.5                       | 5.11.0                         |
| River 及 pgx driver          | 0.23.1                      | 0.47.0                         |
| go-oidc                      | 3.14.1                      | 3.21.0                         |
| oauth2                       | 0.30.0                      | 0.37.0                         |
| Stripe Go SDK                | 自行封装 HTTP / HMAC        | 86.4.2                         |
| Stripe API                   | 2025-06-30.basil            | 2026-08-26.dahlia              |
| sqlc                         | 1.30.0                      | 1.31.1，重新生成查询代码       |
| PostgreSQL                   | 17                          | Compose / CI 18.6；Render 18.x |
| 容器基础系统                 | Debian 12                   | Debian 13 / distroless nonroot |
| GitHub checkout              | v4                          | v7.0.1                         |
| GitHub setup-node / setup-go | v4 / v5                     | v7.0.0 / v7.0.0                |
| pnpm/action-setup            | v4                          | v6.1.0                         |

`google/uuid` 1.6.0 和 `shopspring/decimal` 1.4.0 已为各自最新稳定版。Go 间接依赖随 `go get -u -t ./...`、`go mod tidy` 更新。Nuxt 管理的嵌套依赖按其声明范围解析，避免强行覆盖框架内部的大版本约束。Wrangler 的官方依赖包含 Miniflare alpha；应用直接依赖均选择稳定发布。

### TypeScript 7 的兼容限制

已实际安装并验证 TypeScript 7.0.2。它不再提供 `typescript/lib/tsc`，而最新版 `vue-tsc` 3.3.11 的 Vue 模板检查仍需这个 JavaScript 编译器接口，导致 `nuxt typecheck` 报 `ERR_PACKAGE_PATH_NOT_EXPORTED`。同时框架内的 typescript-eslint 要求 `<6.1.0`。因此两个 Nuxt 应用使用最新兼容的 6.0.3，保留完整模板类型检查。

Vue 官方已为 TypeScript 6 兼容包增加适配，其发布包仍依赖 6 的编程接口；等 Vue/Nuxt 工具链支持原生 7 的接口后再升级。参见 [Vue 官方迁移说明](https://github.com/vuejs/language-tools/pull/6123)、[vue-tsc 3.3.11 实现](https://github.com/vuejs/language-tools/blob/v3.3.11/packages/tsc/index.ts) 和 [TypeScript 7 公告](https://devblogs.microsoft.com/typescript/announcing-typescript-7-0/)。

## 代码与 API 调整

- **Nuxt 数据请求**：所有 Workspace 数据使用响应式 `useAsyncData` 键，详情键包含 Workspace 和资源 ID；请求接收并传递 `AbortSignal`，支持取消过时请求。退出登录清理 Nuxt 请求缓存。连接和发票页面的独立读取改为并发。依据 [Nuxt useAsyncData 文档](https://nuxt.com/docs/4.x/api/composables/use-async-data)。
- **Lucide**：迁移到 `@lucide/vue`，更新组件导入。旧包已弃用，新包保留所用图标的组件 API。依据 [官方迁移指南](https://lucide.dev/guide/vue/migration)。
- **Stripe**：用实例化 SDK 替代自建认证、版本头和签名算法；订阅读取与计划阶段更新使用类型化 API。SDK 处理可重试的网络错误和 `lock_timeout`，最多两次，保留业务幂等键和 25 秒总时限；普通参数错误不重试。错误内容脱敏，响应读取上限 2 MiB，保留禁止跳转与正负五分钟验签窗口。展开对象或 ID 形式的 Customer、Schedule、Invoice 均可读取。依据 [SDK 发布记录](https://github.com/stripe/stripe-go/releases/tag/v86.4.2)。
- **Stripe 破坏性变更**：移除已失效的 `phases[].iterations`，使用目标计划的 `duration.interval` 和 `interval_count=1`；保留当前周期截止时间、期末换档、不补差价与 release 策略。Checkout 显式使用当前 `flexible` 计费模式，已有订阅保持原模式。依据 [iterations 迁移](https://docs.stripe.com/changelog/clover/2025-09-30/remove-iterations) 与 [billing mode 变更](https://docs.stripe.com/changelog/clover/2025-09-30/billing-mode-default-flexible)。
- **River**：采用 `SoftStopTimeout` 和 `Stopped()`，允许任务先完成，再取消上下文。退出中断不把最后一次尝试的导出误标为失败；同步中断退还应用侧尝试计数。保留真实失败的重试上限。依据 [River 变更记录](https://github.com/riverqueue/river/blob/v0.47.0/CHANGELOG.md)。
- **PostgreSQL / CI**：使用 18.6 服务验证 RLS、迁移和真实 HTTP 集成；Compose 改用 18 的新目录与独立卷。Node/Go 的 CI 版本直接读取仓库文件，升级所有 Actions。已有数据库与 River 队列的迁移顺序见 [部署手册](deployment.md)。

## 验证

```bash
pnpm install --frozen-lockfile
pnpm peers check
pnpm verify
cd services/backend
go mod verify
go vet ./...
TEST_DATABASE_URL='postgres://postgres:postgres@localhost:5432/minoduck_test?sslmode=disable' go test -race -count=1 ./...
go build ./cmd/...
```

新增回归覆盖 SDK 重试幂等键与响应关闭、参数错误脱敏、请求取消、展开字段解析、过期/未来时间戳和空签名密钥、月付/年付计划阶段时长，以及最后一次导出尝试被退出中断后的恢复。原有 CSV 金额版本、租户隔离、角色限制、订阅去重/乱序/宽限期测试一起重跑。

本地使用 PGlite 的 PostgreSQL 协议环境，CI 使用 PostgreSQL 18.6。真实 Stripe sandbox 支付、浏览器交互和已有 PostgreSQL 17 数据的备份恢复演练仍属于 [外部验收](acceptance.md)。
