# 上游供应商 OAuth 插件方案

## 背景

OmniHub 当前的上游账号模型已经围绕 `accounts` 表、`provider.Driver`、`provider.Registry`、Resolver 和 Forwarder 构建。一个上游账号代表一组供应商凭证，Resolver 负责选账号，Driver 负责把内部统一请求转换成供应商请求，Forwarder 负责发送请求、流式转发、错误处理和重试。

后续要支持的“OAuth”不是终端用户登录 OmniHub，而是**上游供应商账号的认证方式**。例如管理员添加一个 GPT Pro / Codex、Claude Code、Gemini CLI 订阅账号，OmniHub 用该账号的 OAuth 登录态调用对应上游能力。

根据开源项目调研，常见做法是：

- Codex / ChatGPT 订阅：读取或生成 Codex OAuth 登录态，将 `/v1/responses`、`/v1/chat/completions` 转发到 Codex 后端。
- Claude 订阅：通过 Claude Code OAuth、Claude Code SDK、Claude CLI 或 OAuth 代理封装成 Anthropic-compatible / OpenAI-compatible API。
- Gemini CLI：通过 Google OAuth 登录态访问 Gemini / Code Assist 能力。
- 多账号项目通常会做账号池、轮询、sticky、token 自动刷新、quota 统计和 401/429 处理。

本方案把这类能力设计为插件化的**上游账号认证能力**，不混入用户登录 `AuthProvider`，也不混入支付订阅 `PaymentProvider`。

## 目标

1. 管理员添加上游账号时，可以选择 `API Key`、`OAuth 登录`、`导入 auth.json`、`Service Account` 等认证方式。
2. OAuth 登录、回调交换、token 刷新、凭证校验由插件负责。
3. 模型请求参数、协议转换和响应解析仍由 `ProviderDriver` 负责。
4. OmniHub 核心继续负责账号池、路由、限流、计费、健康检查和日志。
5. OAuth 插件不进入每次流式请求热路径，避免影响吞吐和稳定性。
6. Codex / Claude 订阅账号作为实验型上游账号接入，稳定生产场景继续优先推荐官方 API Key、Bedrock、Vertex、Teams / Enterprise OAuth 等方式。

## 非目标

- 不把 GPT Pro / Claude Pro / Claude Max 订阅伪装成正式 OpenAI Platform / Anthropic Console API。
- 不把 OmniHub 用户登录改成 OAuth。
- 不在 OAuth 插件里处理 prompt、messages、tools、images 等模型请求内容。
- 不默认支持多用户高并发共享一个个人订阅账号。
- 不要求第一阶段实现完整浏览器 OAuth；可以先支持导入现有 CLI 凭证。

## 核心概念

### Provider

`provider` 仍表示上游模型供应商或协议驱动，例如：

- `openai`
- `anthropic`
- `claude-platform`
- `openai-codex`
- `claude-code`
- `gemini-cli`

### Auth Type

`auth_type` 表示这个上游账号如何获得访问权限：

- `api_key`
- `oauth`
- `imported_oauth`
- `service_account`
- `adc`
- `worker`

### Auth Plugin

`auth_plugin` 表示由哪个插件管理认证生命周期：

- `codex-oauth`
- `claude-oauth`
- `claude-code-worker`
- `gemini-oauth`

### Provider Driver

`ProviderDriver` 负责：

- 构造上游 URL。
- 构造上游请求 body。
- 注入认证 header。
- 将 OpenAI Chat、Responses、Anthropic Messages 等协议做转换。
- 解析非流式和流式响应。

OAuth 插件不做这些事。

## 推荐架构

```text
Admin UI
  ↓
Admin API
  ↓
UpstreamAuthPlugin  ← 登录、导入、刷新、校验
  ↓
accounts.credentials

Client Request
  ↓
Guard / Billing / Limits
  ↓
Resolver
  ↓
TokenManager
  ↓
ProviderDriver
  ↓
Forwarder
  ↓
Upstream Provider
```

热路径是：

```text
Resolver → TokenManager → ProviderDriver → Forwarder
```

其中 `TokenManager` 只有在 token 快过期、401 重试或手动刷新时才调用 OAuth 插件。

## 数据模型

当前 `accounts` 表已有：

```sql
provider
credentials JSONB
base_url
enabled
weight
priority
```

建议新增字段：

```sql
ALTER TABLE accounts
  ADD COLUMN auth_type TEXT NOT NULL DEFAULT 'api_key',
  ADD COLUMN auth_plugin TEXT,
  ADD COLUMN auth_status TEXT NOT NULL DEFAULT 'ok',
  ADD COLUMN auth_subject TEXT,
  ADD COLUMN auth_email TEXT,
  ADD COLUMN auth_plan TEXT,
  ADD COLUMN auth_expires_at TIMESTAMPTZ,
  ADD COLUMN last_refresh_at TIMESTAMPTZ,
  ADD COLUMN refresh_error TEXT;
```

推荐 `auth_status` 取值：

- `ok`
- `expiring`
- `refreshing`
- `refresh_failed`
- `login_required`
- `revoked`
- `quota_exceeded`
- `unsupported_region`
- `disabled`

### OAuth Session 表

浏览器 OAuth 登录需要临时状态表：

```sql
CREATE TABLE upstream_oauth_sessions (
    id              BIGSERIAL PRIMARY KEY,
    state           TEXT NOT NULL UNIQUE,
    provider        TEXT NOT NULL,
    auth_plugin     TEXT NOT NULL,
    account_id      BIGINT,
    account_name    TEXT,
    code_verifier   TEXT,
    redirect_after  TEXT,
    metadata        JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at      TIMESTAMPTZ NOT NULL
);
```

说明：

- `account_id` 为空表示新建账号。
- `account_id` 不为空表示重新登录已有账号。
- `code_verifier` 属于敏感字段，建议复用现有 secret 加密能力。

## Credentials 结构

### API Key

```json
{
  "api_key": "sk-..."
}
```

### Codex OAuth

```json
{
  "access_token": "...",
  "refresh_token": "...",
  "expires_at": "1780000000",
  "account_id": "...",
  "email": "user@example.com",
  "plan": "pro",
  "scope": "...",
  "token_type": "Bearer"
}
```

### Claude OAuth

```json
{
  "access_token": "...",
  "refresh_token": "...",
  "expires_at": "1780000000",
  "account_id": "...",
  "email": "user@example.com",
  "plan": "max",
  "organization_id": "...",
  "token_type": "Bearer"
}
```

### 导入 CLI 凭证

导入 `~/.codex/auth.json` 或 Claude Code 凭证时，不建议把原始文件全文作为长期主结构。推荐解析成标准字段，同时保留必要的源信息：

```json
{
  "access_token": "...",
  "refresh_token": "...",
  "expires_at": "1780000000",
  "account_id": "...",
  "email": "user@example.com",
  "source": "codex_auth_json",
  "source_schema_version": "1"
}
```

## 插件 SPI

建议新增冷路径 SPI：`UpstreamAuthProvider`。

```go
type UpstreamAuthProvider interface {
    Metadata(ctx context.Context) (*AuthProviderMetadata, error)
    BeginAuth(ctx context.Context, req *BeginAuthRequest) (*BeginAuthResponse, error)
    ExchangeCallback(ctx context.Context, req *CallbackRequest) (*TokenBundle, error)
    ImportCredentials(ctx context.Context, req *ImportCredentialsRequest) (*TokenBundle, error)
    Refresh(ctx context.Context, req *RefreshRequest) (*TokenBundle, error)
    Validate(ctx context.Context, req *ValidateRequest) (*AccountProfile, error)
    Revoke(ctx context.Context, req *RevokeRequest) error
}
```

### Metadata

```json
{
  "name": "codex-oauth",
  "display_name": "OpenAI Codex OAuth",
  "supported_providers": ["openai-codex"],
  "auth_methods": ["browser_oauth", "device_code", "import_auth_json"],
  "experimental": true
}
```

### BeginAuth

请求：

```json
{
  "provider": "openai-codex",
  "account_id": 123,
  "account_name": "gpt-pro-main",
  "callback_url": "https://omnihub.example.com/admin/api/upstream-auth/callback"
}
```

响应：

```json
{
  "authorization_url": "https://...",
  "state": "...",
  "expires_at": "2026-06-10T12:00:00Z"
}
```

### ExchangeCallback

请求：

```json
{
  "state": "...",
  "code": "...",
  "redirect_uri": "https://omnihub.example.com/admin/api/upstream-auth/callback"
}
```

响应：

```json
{
  "access_token": "...",
  "refresh_token": "...",
  "expires_at": "2026-06-10T13:00:00Z",
  "subject": "...",
  "email": "user@example.com",
  "plan": "pro",
  "extra": {
    "account_id": "..."
  }
}
```

### ImportCredentials

用于导入 `auth.json`、Claude Code credentials 等：

```json
{
  "provider": "openai-codex",
  "source_type": "codex_auth_json",
  "payload": "{...}"
}
```

插件应解析、校验、规范化后返回 `TokenBundle`。

### Refresh

请求：

```json
{
  "provider": "openai-codex",
  "account_id": 123,
  "refresh_token": "...",
  "access_token": "...",
  "expires_at": "2026-06-10T13:00:00Z"
}
```

响应：

```json
{
  "access_token": "...",
  "refresh_token": "...",
  "expires_at": "2026-06-10T14:00:00Z",
  "subject": "...",
  "email": "user@example.com",
  "plan": "pro"
}
```

刷新成功后，OmniHub 核心负责写回 `accounts.credentials`、`auth_expires_at`、`last_refresh_at`。

## 管理端流程

### 添加账号

```text
添加上游账号
  ↓
选择供应商
  ↓
选择认证方式
  ↓
填写账号基础信息
  ↓
认证方式分支
    API Key：填写 key
    OAuth：跳转授权
    导入：粘贴 auth.json / credentials
    Service Account：填写或上传服务账号配置
  ↓
测试连接
  ↓
保存账号
```

### 重新登录

OAuth 账号失效时，不新建账号，而是更新原账号凭证：

```text
账号状态 login_required
  ↓
管理员点击“重新登录”
  ↓
BeginAuth(account_id)
  ↓
ExchangeCallback
  ↓
更新原 accounts 行
```

这样历史用量、账号 ID、限额、健康状态和分组不会断裂。

### 后台展示字段

账号列表建议展示：

- 供应商。
- 认证方式。
- OAuth 状态。
- 登录邮箱或 subject。
- 订阅计划。
- token 过期时间。
- 最近刷新时间。
- 最近刷新错误。
- 是否实验功能。

## 运行时请求参数设计

请求参数分三层：

```text
1. 下游用户请求参数
2. OmniHub 内部统一请求参数
3. 上游供应商请求参数
```

OAuth 插件只处理第 3 层中的认证凭证，不接收完整模型请求内容。

### 下游请求

用户可以继续请求 OmniHub 已支持的入口：

- Anthropic Messages。
- OpenAI Chat Completions。
- OpenAI Responses。

示例：

```json
{
  "model": "gpt-5-codex",
  "messages": [
    {
      "role": "user",
      "content": "解释这个函数"
    }
  ],
  "stream": true
}
```

### 内部统一请求

请求先解析为 `ir.UnifiedRequest`，继续走现有 Guard / Resolver / Forwarder。

核心只向 Driver 传：

```text
UnifiedRequest
Account
AuthMaterial
```

### AuthMaterial

建议在核心中引入一个运行时认证材料结构：

```go
type AuthMaterial struct {
    Type        string
    AccessToken string
    APIKey      string
    Headers     map[string]string
    ExpiresAt   time.Time
}
```

`TokenManager` 根据账号生成它：

- `api_key`：从 `credentials.api_key` 读取。
- `oauth`：确保 token 新鲜后读取 `credentials.access_token`。
- `service_account`：可生成短期 access token。

Driver 根据 `AuthMaterial` 注入 header。

## Header 规则

### OpenAI API Key

```http
Authorization: Bearer <api_key>
```

### Codex OAuth

```http
Authorization: Bearer <access_token>
Content-Type: application/json
Accept-Encoding: identity
```

### Anthropic API Key

```http
x-api-key: <api_key>
anthropic-version: 2023-06-01
Content-Type: application/json
```

### Claude OAuth

```http
Authorization: Bearer <access_token>
Content-Type: application/json
Accept-Encoding: identity
```

具体供应商是否需要额外 header，由对应 Driver 管理。

## 不透传的内容

以下内容只留在 OmniHub 本地，不能传给上游：

- 本地用户 ID。
- 本地 virtual key。
- 本地 tenant ID。
- 本地钱包、套餐、余额信息。
- OmniHub 管理员账号。
- 下游原始 `Authorization`。
- `X-OmniHub-*` 内部 header。

除非账号显式开启 `ForwardClientIP`，否则也不透传真实客户端 IP。当前 Forwarder 已默认清理 `X-Forwarded-*` 等 header，应继续保持。

## TokenManager

新增 `TokenManager`，位于 Resolver 和 Driver 之间。

职责：

- 判断账号是否需要刷新。
- 对同一个 `account_id` 加刷新锁。
- 调用 `UpstreamAuthProvider.Refresh`。
- 写回新凭证。
- 更新 `auth_status` 和 `refresh_error`。
- 在 401 时支持一次强制刷新重试。

伪流程：

```text
EnsureFresh(account):
  if account.auth_type != oauth:
      return account

  if expires_at - now > refresh_window:
      return account

  lock(account.id)
  defer unlock

  reload account from DB
  if expires_at - now > refresh_window:
      return reloaded account

  token = plugin.Refresh(reloaded.credentials)
  update account credentials
  return updated account
```

推荐刷新窗口：

```text
默认提前 5 分钟刷新
可通过环境变量配置
```

刷新失败：

```text
refresh_failed
  ↓
记录 refresh_error
  ↓
Resolver 暂时跳过该账号
  ↓
后台提示重新登录
```

## Resolver 和健康状态

Resolver 应跳过以下 OAuth 状态：

- `login_required`
- `refresh_failed`
- `revoked`
- `disabled`
- `quota_exceeded`

可继续参与路由：

- `ok`
- `expiring`

如果请求上游返回：

- `401`：触发一次强制刷新并重试。
- `403`：标记 `login_required` 或 `unsupported_region`，视错误体判断。
- `429`：进入 account cooldown，不一定刷新 token。
- quota 相关错误：标记 `quota_exceeded`，等待后台或定时任务恢复。

## Codex 订阅账号设计

### Provider

```text
provider = openai-codex
auth_type = oauth / imported_oauth
auth_plugin = codex-oauth
```

### Driver 职责

`openai-codex` Driver 负责：

- `/v1/responses` 透传或轻量转换到 Codex backend。
- `/v1/chat/completions` 转换成 Responses 风格。
- 处理 SSE 流。
- 处理 tools / tool_calls 映射。
- 处理 model 与 reasoning effort 映射。
- 查询 `/backend-api/codex/models` 或兼容模型列表。

### 第一阶段请求形态

优先支持 Responses：

```http
POST /v1/responses
```

上游：

```http
POST https://chatgpt.com/backend-api/codex/responses
Authorization: Bearer <access_token>
```

下游 body：

```json
{
  "model": "gpt-5-codex",
  "input": "帮我分析这个项目",
  "stream": true,
  "reasoning": {
    "effort": "high"
  }
}
```

上游 body 由 Driver 构造。第一版可以尽量保持 Responses 结构，减少 Chat Completions 转换复杂度。

### 第二阶段支持 Chat Completions

下游：

```json
{
  "model": "gpt-5-codex",
  "messages": [
    {
      "role": "system",
      "content": "You are a coding assistant."
    },
    {
      "role": "user",
      "content": "解释这个函数"
    }
  ],
  "stream": true
}
```

Driver 转换：

```json
{
  "model": "gpt-5-codex",
  "input": [
    {
      "role": "system",
      "content": [
        {
          "type": "input_text",
          "text": "You are a coding assistant."
        }
      ]
    },
    {
      "role": "user",
      "content": [
        {
          "type": "input_text",
          "text": "解释这个函数"
        }
      ]
    }
  ],
  "stream": true
}
```

## Claude 订阅账号设计

Claude 建议分三类支持：

```text
anthropic-api-key         稳定
claude-code-worker        较稳，调用官方 SDK / CLI
claude-subscription-oauth 实验
```

### Claude OAuth Provider

```text
provider = claude-subscription
auth_type = oauth / imported_oauth
auth_plugin = claude-oauth
```

### Driver 职责

`claude-subscription` Driver 负责：

- Anthropic Messages 原生请求。
- OpenAI Chat Completions 到 Anthropic Messages 的转换。
- Claude 流式事件到 OmniHub IR 的转换。
- 工具调用映射。

### Anthropic Messages 请求

下游：

```json
{
  "model": "claude-sonnet-4",
  "max_tokens": 4096,
  "messages": [
    {
      "role": "user",
      "content": "帮我重构这段代码"
    }
  ],
  "stream": true
}
```

上游 header：

```http
Authorization: Bearer <access_token>
Content-Type: application/json
Accept-Encoding: identity
```

上游 body 由 Driver 构造，不由 OAuth 插件构造。

## Worker 型认证

对于 Claude Code SDK / CLI、Codex CLI 这类官方客户端，另一个可选模式是：

```text
provider = claude-code-worker / codex-worker
auth_type = worker
```

请求不直接由 Forwarder 发到供应商 HTTP API，而是进入本地 worker 队列：

```text
OmniHub
  ↓
Worker Queue
  ↓
Claude Code SDK / Codex CLI
  ↓
官方客户端自己处理认证和调用
```

这种方式更适合代码任务、低并发、本地或小团队场景，不适合高并发普通聊天 API。

## 插件与 Driver 的边界

正确边界：

```text
OAuth 插件：
  登录
  回调
  导入凭证
  刷新 token
  校验账号 profile
  撤销登录

Provider Driver：
  请求 URL
  请求 body
  认证 header 注入
  协议转换
  响应解析
  流式事件解析

OmniHub Core：
  账号池
  路由
  限流
  计费
  健康检查
  日志
  熔断
```

错误边界：

```text
OAuth 插件接收 messages / prompt
OAuth 插件直接发模型请求
OAuth 插件决定账号池路由
Driver 自己刷新 token
Forwarder 透传下游 Authorization
```

## 安全要求

1. `credentials`、`code_verifier`、refresh token 必须加密存储。
2. 后台 API 返回账号时继续脱敏凭证。
3. OAuth callback 必须校验 `state`。
4. OAuth session 必须有过期时间。
5. Refresh token 轮换时必须原子写回。
6. 同一个账号刷新必须加锁，避免并发刷新导致 refresh token 失效。
7. 导入 `auth.json` 后只保存必要字段，不在日志中输出原文。
8. 默认不暴露 OAuth 代理到公网；如果开放下游 API，必须要求 OmniHub 自己的 virtual key。
9. 下游用户身份不透传给上游供应商。

## 分阶段落地

### 阶段 1：核心数据结构

- `accounts` 增加 `auth_type`、`auth_plugin`、`auth_status`、`auth_expires_at` 等字段。
- `provider.Account` 增加对应字段。
- Repository 读写这些字段。
- 后台账号列表展示认证方式和状态。

### 阶段 2：导入式 OAuth

- 新增 `UpstreamAuthProvider` SPI 的本地接口。
- 新增 `TokenManager`。
- 支持导入 Codex `auth.json`。
- 支持 token 过期前刷新。
- 支持 401 强制刷新重试。

### 阶段 3：Codex 实验 Driver

- 新增 `openai-codex` Driver。
- 支持 `/v1/responses`。
- 支持基础模型列表。
- 支持流式透传。
- 后台可以测试 Codex OAuth 账号。

### 阶段 4：浏览器 OAuth

- 新增 `upstream_oauth_sessions` 表。
- 新增：
  - `POST /admin/api/upstream-auth/:plugin/start`
  - `GET /admin/api/upstream-auth/:plugin/callback`
  - `POST /admin/api/accounts/:id/relogin`
- 支持 Codex OAuth PKCE 登录。

### 阶段 5：Claude 订阅实验支持

- 支持 Claude credentials 导入或 Claude OAuth 插件。
- 新增 `claude-subscription` Driver 或 `claude-code-worker`。
- 第一版优先支持 Anthropic Messages。
- Chat Completions 转换后置。

### 阶段 6：账号池增强

- OAuth 账号 sticky 路由。
- 429 cooldown。
- quota 查询和展示。
- per-account 并发限制。
- 多账号 round-robin / weighted 选择。

## 推荐默认策略

1. 稳定生产环境：
   - OpenAI API Key。
   - Anthropic API Key。
   - Bedrock。
   - Vertex。
   - Teams / Enterprise OAuth。

2. 实验功能：
   - Codex / GPT Pro OAuth。
   - Claude Pro / Max OAuth。
   - Gemini CLI OAuth。

3. UI 标签：
   - 实验。
   - 适合本地/小团队。
   - 不建议作为正式商业供应商。

## 参考项目

- Codex Pro API：<https://github.com/314051672/codexProapi>
- Codex OpenAI Proxy：<https://github.com/0oAstro/codex-openai-proxy>
- Codex Gateway：<https://github.com/LanternCX/codex-gateway>
- CLIProxyAPI：<https://github.com/crazyrabbit0/CLIProxyAPI>
- ProxyPool Hub：<https://github.com/yiyao-ai/proxypool-hub>
- auth2api：<https://github.com/AmazingAng/auth2api>
- Meridian：<https://github.com/rynfar/meridian>
- LiteLLM Claude Code Max Subscription：<https://docs.litellm.ai/docs/tutorials/claude_code_max_subscription>

## 最终结论

OmniHub 应把供应商 OAuth 设计成独立的上游账号认证插件体系：

```text
accounts.provider      = 调哪个供应商 / Driver
accounts.auth_type     = 怎么认证
accounts.auth_plugin   = 谁负责认证生命周期
accounts.credentials   = 加密凭证
```

请求热路径保持：

```text
Resolver → TokenManager → ProviderDriver → Forwarder
```

这样既能承接 Codex、Claude、Gemini 等订阅账号接入，也不会破坏现有 API Key、账号池、计费和流式转发架构。
