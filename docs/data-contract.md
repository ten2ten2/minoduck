# 数据来源与契约

## 原生连接器

| 来源 | 凭据和端点 | Actual | 边界 |
|---|---|---|---|
| OpenAI | 组织 Admin Key；`/v1/organization/costs` 与 `/v1/organization/usage/completions` | Costs 返回的原币种金额 | 费用行不虚构模型归属；usage 独立保存；文本 L1 不代表全部费用 |
| Anthropic | Admin API Key；`/v1/organizations/cost_report`、`usage_report/messages` | cents 精确除以 100 | Console 范围；不代表 Bedrock/Vertex；priority tier 已知排除 |
| OpenRouter | Management Key；`/api/v1/activity?date=YYYY-MM-DD` | activity usage | 最近 30 个已完成 UTC 日；BYOK inference 镜像金额不再次加到 Actual |
| CSV | 用户上传去敏报表 | 用户选择口径 | 通用模板，尚无供应商专属认证模板 |

初次同步默认最近 7 个完整日；后续扫描本月和上月已完成日，OpenRouter 截为 30 天。Free 每日，付费两小时一次（OpenRouter 每日）。401/403 终止当前同步并提示更换凭据；429 按 Retry-After 退避并限制次数，5xx 由 River 重试。所有快照是仅含账单字段的规范化证据，不保存推理正文。

账户标识由用户声明，不能当作 Provider 已验证的组织身份。相同 Billing Account 内重复 provider/account_ref 被阻止；不同声明无法自动证明是不同真实账户。

## CSV

UTF-8，可带 BOM；20 MiB / 100,000 行上限。禁止未知字段，避免误收 prompt、response、API key 等内容。必填列 `period_start,period_end,amount,currency,charge_category,coverage`；可选 `model,model_vendor,project,source_event_id`。

上传同时指定 `account_id`、`source_scope`、`timezone`（IANA）、`cost_kind`、`granularity`。时间窗口为开始包含、结束不包含；只有日期时按声明时区解释；显式偏移保留真实时刻。`aggregate` 对同一完整维度只接受一行；`event` 必须带稳定 `source_event_id`。事件修订以同一 `source_event_id` 识别原记录，即使金额、日期、模型、类别或其他可修订字段发生变化也不会被当成第二笔费用。金额最多 18 位整数和 12 位小数，不接受科学计数法、千位逗号、NaN。

充值不是消费，未知费用类别会隔离报错，未知模型名保留。预览返回最多 20 行、逐行错误、每币种汇总。存在错误不能提交。提交前再次校验存档 hash 并解析；更改当前记录必须显式确认。不同 source_scope 的同账户同口径时间重叠不能静默相加。CSV 证据引用保存对应记录序号，原生连接器证据引用保存规范化 JSON entry 指针。

`coverage=complete` 是用户对每个来源窗口的声明；`coverage=partial` 明确表示不完整。还须所有窗口覆盖对账期间。勾选“范围已核实”不补足缺少的日期。

## API 约定

API 前缀 `/api/v1`，控制台经同源 BFF 使用。除认证入口、providers、探针及 Stripe Webhook 外，必须有服务身份和 session；写操作另需 `Origin` 与 `X-CSRF-Token`。Workspace 路径使用 UUID，UI 使用 slug。

错误为 `{ "error": { "code": "...", "message_key": "errors....", "retryable": false, "request_id": "..." } }`。金额字符串；未知值用 null，与 `"0"` 区分。402 为套餐门槛；403 为权限/CSRF；404 隐藏非成员资源；409 为版本/状态冲突；503 为暂不可用。

Costs 查询接受 `start,end,cost_kind,currency,provider,model,page,page_size`，结束日期不包含；分页默认 50、上限 100。Exports 使用同样筛选，UTF-8 BOM 和用户语言列名，金额保留精度，文本以安全前缀防止电子表格公式执行。

[OpenAPI](openapi.json) 固定主要请求模型、路由和安全语义；动态报表 evidence 以 schema object 返回，具体键见后端响应构建处。本版不提供第三方公共 API key。
