# 公告与套餐/按量计费设计

## 背景

OmniHub 目前已经具备供应商管理、用户门户、钱包余额、兑换码、用户价格倍率和逐请求计费记录。下一步需要补齐运营能力：

1. 管理员可以向门户用户发布公告。
2. 平台可以同时支持两种售卖方式：套餐和按量计费。
3. 套餐额度优先消耗，必要时可以按量兜底。

本设计聚焦第一期，不包含在线支付、自动续费、套餐升降级差价和周期自动续费。

## 目标

- 后台可以管理公告和套餐。
- 门户用户可以看到公告、当前套餐、钱包余额和可用套餐。
- 请求扣费时区分套餐额度和按量余额。
- 保留现有钱包/兑换码能力，避免破坏当前按量扣费逻辑。

## 非目标

- 不接入真实支付渠道。
- 不做自动续费。
- 不做套餐升级/降级差价计算。
- 不做复杂的企业合同账期。

## 计费模式

支持三种用户使用方式：

1. **纯套餐**：只允许使用套餐内额度，额度用尽或套餐过期后拒绝请求。
2. **纯按量**：没有套餐，直接从钱包余额按请求实际费用扣减。
3. **套餐 + 按量兜底**：优先扣套餐额度，套餐额度不足时再扣钱包余额。

默认推荐第三种，最适合运营：用户买套餐后可以继续超额使用，不会因为套餐额度刚好不足而中断。

## 扣费顺序

每次请求完成并计算出 `billed_usd` 后，按以下顺序处理：

1. 找到用户当前生效的套餐权益。
2. 如果有可用套餐额度，优先扣套餐剩余额度。
3. 如果套餐额度不足：
   - 套餐允许按量兜底，则剩余部分从钱包余额扣。
   - 套餐不允许兜底，则拒绝请求或在请求前拦截。
4. 如果没有套餐，则按纯按量模式从钱包余额扣。
5. 如果钱包余额不足，则拒绝请求。

为减少并发误差，请求前仍需要做余额/额度预检查，请求完成后按实际成本记录消费。

## 公告设计

### 公告字段

新增公告表，建议字段：

- `id`
- `title`
- `body`
- `kind`：`info`、`maintenance`、`pricing`、`model`
- `status`：`draft`、`published`、`archived`
- `placement`：`portal_home`、`login`、`banner`
- `priority`
- `starts_at`
- `ends_at`
- `dismissible`
- `created_at`
- `updated_at`

### 后台能力

- 公告列表
- 新建公告
- 编辑公告
- 发布/下线公告
- 删除草稿或归档公告

### 门户展示

- 门户首页展示当前生效公告。
- 顶部横幅只展示最高优先级的重要公告。
- 可关闭公告记录到本地或数据库，第一期可先本地关闭。

## 套餐设计

### 套餐模板

新增套餐模板表，建议字段：

- `id`
- `name`
- `description`
- `price_usd`
- `included_credit_usd`
- `valid_days`
- `rpm_limit`
- `daily_usd_limit`
- `allowed_models`
- `price_ratio`
- `allow_payg_overage`
- `enabled`
- `sort_order`
- `created_at`
- `updated_at`

说明：

- `included_credit_usd` 是套餐包含额度。
- `price_ratio` 是套餐内用户计费倍率。
- `allow_payg_overage` 控制套餐额度不足时是否允许继续按量扣钱包。

### 用户套餐权益

新增用户套餐权益表，建议字段：

- `id`
- `user_id`
- `plan_id`
- `plan_name_snapshot`
- `starts_at`
- `expires_at`
- `credit_granted_usd`
- `credit_remaining_usd`
- `price_ratio_snapshot`
- `allow_payg_overage_snapshot`
- `status`：`active`、`expired`、`depleted`、`revoked`
- `created_at`
- `updated_at`

保留快照字段，避免套餐模板后来被改名或调价后影响历史权益。

### 套餐消费流水

为清楚展示套餐额度如何被扣减，建议新增套餐消费流水：

- `id`
- `user_plan_grant_id`
- `message_request_id` 或请求关联信息
- `amount_usd`
- `created_at`

如果当前请求记录是异步批量写入，第一期也可以先不强绑定 `message_request_id`，只记录请求时间、用户和金额。

## 钱包按量计费

现有钱包继续作为按量余额：

- 充值、兑换码、管理员加款仍进入钱包。
- 没有套餐时，按 `billed_usd` 扣钱包。
- 套餐额度不足且允许兜底时，差额扣钱包。

门户展示时需要清楚区分：

- 套餐剩余额度
- 钱包余额
- 本次请求由套餐扣了多少、由钱包扣了多少

## 后台页面

### 公告管理

入口放在设置或运营分组下。

页面包含：

- 公告列表
- 状态筛选
- 新建/编辑表单
- 发布/下线操作

### 套餐管理

入口建议放在计费分组下。

页面包含：

- 套餐列表
- 新建/编辑套餐
- 启用/停用套餐
- 给用户手动分配套餐

### 用户详情

在用户管理中增加：

- 当前套餐
- 套餐剩余额度
- 套餐到期时间
- 手动分配/撤销套餐

## 门户页面

### 首页

展示：

- 当前公告
- 当前套餐摘要
- 钱包余额

### 套餐页

展示：

- 当前套餐
- 剩余额度
- 到期时间
- 是否允许按量兜底
- 可领取或可购买套餐列表

第一期没有在线支付时，按钮文案可为：

- 免费领取
- 联系管理员开通
- 使用兑换码开通

### 钱包页

保留现有钱包能力，同时增加说明：

- 钱包余额用于按量扣费或套餐超额兜底。

## API 设计

### 后台 API

- `GET /admin/api/announcements`
- `POST /admin/api/announcements`
- `PATCH /admin/api/announcements/:id`
- `DELETE /admin/api/announcements/:id`
- `GET /admin/api/plans`
- `POST /admin/api/plans`
- `PATCH /admin/api/plans/:id`
- `POST /admin/api/users/:id/plan-grants`
- `PATCH /admin/api/users/:id/plan-grants/:grant_id`

### 门户 API

- `GET /portal/api/announcements`
- `GET /portal/api/plans`
- `GET /portal/api/me/plan`
- `POST /portal/api/plans/:id/claim`，仅用于免费套餐或无需支付的套餐

## 验证标准

- 管理员可以发布公告，门户能看到。
- 管理员可以创建套餐，门户能看到启用套餐。
- 管理员可以给用户分配套餐。
- 用户请求优先扣套餐额度。
- 套餐额度不足时，如果允许按量兜底，会扣钱包余额。
- 套餐额度不足且不允许兜底时，请求被拒绝。
- 没有套餐的用户继续按钱包余额按量计费。
- 现有兑换码、钱包、请求记录不被破坏。
