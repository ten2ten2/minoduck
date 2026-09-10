# 架构与明确决策

## 域名及进程

`minoduck.ai` 只将原始路径与查询参数 308 到 `www.minoduck.ai`。官网位于 www，控制台位于 app，Go API 位于 api。控制台路由为 `/w/:workspaceSlug/...`，不增加 `/app` 别名。

两个 Nuxt 构建为 Cloudflare Workers；官网预渲染公开页面，控制台为 CSR。浏览器只访问 app 的 `/api/v1`；Nitro BFF 校验固定方法/路径白名单并添加服务身份，不代理任意 URL。Go 再验证服务身份、session、CSRF、Workspace 与角色。Stripe 直接访问 Go Webhook；此路径不进入 BFF 白名单。

Go 是模块化单体，API、Worker、scheduler 共用代码但独立运行。耗时同步/导出进入 River；每个任务只带资源 ID，不携带凭据或账单正文。PostgreSQL 是唯一任务与业务状态库，R2 保存私有来源快照和导出。

## 财务模型

- SQL `numeric(30,12)`；Go decimal；JSON 金额为字符串。前端只在图形坐标使用浮点，不计算财务结果。
- Actual、Billed、Calculated、Estimated 分口径查询。Usage 不再作为第二笔 Actual。
- 原币种分别汇总；无跨币种直接相加、虚构汇率或浮点 cents 转换。
- 来源自然键排除金额；账户锁 + 唯一 current 索引控制幂等。更正关闭旧 current 并新增版本；撤回后重新出现继续递增版本。
- 一页读取失败则整片失败，不能将部分结果替换为完整快照。已经提交的完整分片保留，checkpoint 只越过该片。重新执行可能重复拉取已完成分片，发布仍然幂等。
- L2：Actual + 单独声明的调整项 对比 Billed，原币种绝对容差 0.01。税费等调整只能计入一次。`match_status` 与人工 `handling_status` 分开。
- L1：已保存文本用量 × 历史有效价格；缺价格、计费维度、来源窗口或范围确认即降为待确认。结果存独立对账运行，不改写 Actual。证据保留使用的 metrics、价格版本和源批次 ID。
- 价格候选仅模拟同模型版本/币种/层级/地区的样本期间价差；要求核验质量、延迟、数据政策与附加费。Applied 不是已实现节省。

## 计费 ADR

新 Stripe 价格文档覆盖执行规格中的旧 Paddle 与旧价格。月付为默认，年付明确展示全年实际扣款。Billing Account 由用户拥有，其 Workspace 共用订阅与配额；连接/成员/预算等不会因创建第三个 Workspace 而乘三。Owner 才能管理其 Billing Account。

Checkout success 页面仅提示正在确认；Webhook 验签后锁定订阅行，再向 Stripe 读取当前订阅。事件 ID 去重，旧事件不能倒退新状态，未知价格不授予付费权限。支付失败七天宽限期从 Stripe 当前账单创建时刻计算；重复事件不延长。降级/年转月在期末处理；升级和月转年遵循服务端策略。超额支出不自动扣费，不切断数据读取。

## 安全与生命周期

财务明细、快照、对账、导出等启用 FORCE RLS；无 tenant context 时不可读取。角色/连接/规则等元数据仍必须由查询显式校验所属 Workspace。生产进程拒绝 superuser/BYPASSRLS 数据库角色；迁移使用独立账户。

Provider 凭据 AES-256-GCM 加密，AAD 绑定 Workspace 与连接，密钥版本独立记录；断开清除凭据并递增 generation。在途同步发布前重新检查 generation 与删除标记。当前实现使用单一有效主密钥；轮换时应让用户重新提交凭据，或先实施双密钥迁移，不能直接覆盖旧主密钥。

Workspace 删除先提交 tombstone，再撤销凭据、取消任务、清除 R2 文件和级联删除数据。tombstone 故意保留，恢复备份后必须先重放删除清单再开放访问。保留期由 maintenance 清理；降级提供 30 天删除缓冲，但查询权限立即按当前套餐计算。导出 24 小时有效，下载再次验证身份、成员资格与付费权益。

## 本版范围

完整提供通用 CSV 和手录账单参考，不宣称中文 Provider 专属模板已通过真实样本认证。价格由管理员输入带日期和证据的版本，不自动抓取全球价格。没有 Gateway、代理推理、Prompt/Response 采集、实时告警、自动切流、自动退款或质量等价承诺。

本版把正确性和可追溯闭环放在首位：账单 PDF/OCR、独立 invoice-line 文件解析器、更丰富的模型映射、恢复自动化、限流与容量测试作为上线验收/后续扩展事项列在 acceptance.md，不以页面存在代替验证。
