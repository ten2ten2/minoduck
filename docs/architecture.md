# 架构与业务规则

## 域名及进程

`minoduck.ai` 保留路径与查询参数 308 到 `www.minoduck.ai`。官网位于 www，控制台位于 app，Go API 位于 api；控制台路由为 `/w/:workspaceSlug/...`。

两个 Nuxt 构建为 Cloudflare Workers；官网预渲染公开页面，控制台为 CSR。浏览器只访问 app 的 `/api/v1`；Nitro BFF 校验固定方法/路径白名单并添加服务身份，不代理任意 URL。Go 再验证服务身份、session、CSRF、Workspace 与角色。Stripe 直接访问 Go Webhook；此路径不进入 BFF 白名单。

`packages/ui` 是两端共用的 Nuxt layer，集中维护组件、样式、语言配置与安全响应头。官网的内容页、预渲染与 sitemap 共用 `apps/site/shared/utils/site-routes.ts`；公开 SEO 与控制台私有缓存策略分别留在各应用。

Go 是模块化单体，API、Worker、scheduler 共用代码但独立运行。耗时同步/导出进入 River；每个任务只带资源 ID，不携带凭据或账单正文。PostgreSQL 是唯一任务与业务状态库，R2 保存私有来源快照和导出。

## 财务模型

- SQL `numeric(30,12)`；Go decimal；JSON 金额为字符串。前端只在图形坐标使用浮点，不计算财务结果。
- Actual、Billed、Calculated、Estimated 分口径查询。Usage 不再作为第二笔 Actual。
- 原币种分别汇总；无跨币种直接相加、虚构汇率或浮点 cents 转换。
- 来源自然键排除金额；账户锁 + 唯一 current 索引控制幂等。更正关闭旧 current 并新增版本；撤回后重新出现继续递增版本。
- 同步先拉取并暂存所有分片，再在一个租户事务中发布账本、用量、checkpoint 和成功状态；失败不发布部分结果。提交前重新校验凭据 generation、Provider 身份、套餐和删除标记。
- L2：Actual + 单独声明的调整项 对比 Billed，原币种绝对容差 0.01。税费等调整只能计入一次。`match_status` 与人工 `handling_status` 分开。
- L1：已保存文本用量 × 历史有效价格；缺价格、计费维度、来源窗口或范围确认即降为待确认。结果存独立对账运行，不改写 Actual。证据保留使用的 metrics、价格版本和源批次 ID。
- 价格候选仅模拟同模型版本/币种/层级/地区的样本期间价差；要求核验质量、延迟、数据政策与附加费。Applied 不是已实现节省。

## 计费

Stripe 月付为默认，年付明确展示全年实际扣款。Billing Account 由用户拥有，其 Workspace 共用订阅与配额；连接、成员、预算等额度不会随 Workspace 数量翻倍。Owner 才能管理其 Billing Account。

Checkout 返回只提示正在确认，权益以验签并读取 Stripe 当前状态后的 Webhook 处理结果为准。Billing Account 锁串行化计费与资源变更；支付命令、幂等键和事件处理结果持久化，重复和乱序事件不能重复扣款或倒退权益。需要 3DS 等操作时返回付款入口，不提前授予升级权益。

支付失败的七天宽限期从当前账单创建时刻计算，重复事件不延长。降级/年转月在期末生效；终止订阅收回付费权益。超出套餐的 Workspace、成员、连接和预算规则按确定顺序暂停，升级后恢复；支出软限额只提醒，不自动扣费。

## 安全与生命周期

财务明细、快照、对账、导出等启用 FORCE RLS；无 tenant context 时不可读取。角色/连接/规则等元数据仍必须由查询显式校验所属 Workspace。生产进程拒绝 superuser/BYPASSRLS 数据库角色；迁移使用独立账户。

Provider 凭据使用 AES-256-GCM，AAD 绑定 Workspace 与连接；断开清除凭据并递增 generation。主密钥轮换要求凭据重交或受控重新加密，不支持直接覆盖密钥。

对象上传前先提交清理意向；数据库提交结果不明确时，由 maintenance 判断对象是否仍被引用。保留期清理先认领数据库状态，再删除对象并完成记录清理。来源覆盖与保留期使用批次的 `period_start/period_end`，不从预览 JSON 推断。

Workspace 删除先提交 tombstone，再撤销凭据、取消任务、清除 R2 文件和级联删除数据；恢复备份后先重放删除清单再开放访问。降级提供 30 天删除缓冲，查询权限立即按当前套餐执行。导出 24 小时有效，下载重新验证成员资格与付费权益。

产品边界和外部验收见 [上线检查](acceptance.md)，部署及恢复操作见 [部署手册](deployment.md)。
