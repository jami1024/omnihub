# 上游订阅账号 OAuth 插件与账号分发方案

## 1. 结论先行

OmniHub 要支持的 OAuth 不是“用户用 OAuth 登录 OmniHub”，而是“上游供应商账号使用 OAuth / CLI 登录态接入 OmniHub”。典型场景包括：

- GPT Pro / Codex 订阅账号。
- Claude Pro / Max / Claude Code 账号。
- Gemini CLI / Google OAuth 账号。
- Kiro、Antigravity、Qwen Code、iFlow 等订阅型或 CLI 型账号。

推荐架构是：

```text
上游认证插件 UpstreamAuthPlugin
  负责登录、导入、刷新、校验、撤销

ProviderDriver
  负责请求参数、协议转换、header 注入、响应解析

Resolver / Account Pool
  负责账号组、sticky、quota、cooldown、failover、计费归因

OmniHub CLI / SDK Client Profile
  负责 OmniHub 自己的客户端指纹、会话、能力声明和排障标识
```

核心原则：

```text
认证生命周期插件化
请求协议 Driver 化
账号分发 Pool 化
客户端指纹 OmniHub 自有化
订阅账号能力 experimental 化
```

这意味着 OmniHub 不应该把 OAuth token refresh 写进 Driver，也不应该让 OAuth 插件处理 prompt、messages、tools、images 等模型内容。OAuth 插件只产生可用认证材料；Driver 才决定上游请求怎么发。

---

## 2. 当前项目基础

OmniHub 当前已有比较适合扩展的基础：

- `accounts` 表：上游账号池，已有 `provider`、`credentials`、`enabled`、`weight`、`priority`、`base_url` 等字段。
- `provider.Driver`：负责把 `ir.UnifiedRequest` 转成上游 HTTP 请求。
- `provider.Registry`：注册内置 provider driver。
- Resolver：选择具体上游账号。
- Forwarder：发送请求、处理流式响应、endpoint failover、custom headers、安全 header 清理。
- Provider Group：已有账号分组概念，可扩展成订阅账号池。
- Billing / Wallet / Plan：已有计费基础，可做用户、Key、账号、组维度成本归因。

现有 Driver 参数行为：

### Anthropic Driver

`internal/service/provider/drivers/anthropic/request.go` 当前发送：

```http
Content-Type: application/json
Accept: application/json
x-api-key: <api_key>
anthropic-version: <version>
anthropic-beta: <comma-separated beta list>
```

body 是 Anthropic Messages 结构：

```json
{
  "model": "...",
  "messages": [],
  "system": [],
  "tools": [],
  "tool_choice": {},
  "stream": true,
  "max_tokens": 4096,
  "temperature": 0.7,
  "top_p": 1,
  "top_k": 0,
  "stop_sequences": [],
  "thinking": {},
  "metadata": {}
}
```

### OpenAI Driver

`internal/service/provider/drivers/openai/request.go` 当前发送：

```http
Content-Type: application/json
Accept: application/json
Authorization: Bearer <api_key>
OpenAI-Organization: <organization>
OpenAI-Project: <project>
```

body 由 `internal/protocol/openai` 转成 OpenAI Chat Completions 格式。

### Claude Platform Driver

`internal/service/provider/drivers/claudeplatform/request.go` 当前发送：

```http
Content-Type: application/json
Accept: application/json
x-api-key: <api_key>
anthropic-version: <version>
anthropic-workspace-id: <workspace_id>
anthropic-beta: <comma-separated beta list>
```

### ClientMetadata 透传（SDK 指纹 allow-list）

三个 Driver 在构造请求时，除上述固定 header 外，还会把入口处采集的 `req.ClientMetadata` 逐项透传给上游（见 `anthropic/request.go`、`openai/request.go`、`claudeplatform/request.go` 末尾的 `for k, v := range req.ClientMetadata`）。

采集发生在网关入口 `collectClientMetadata`（`internal/handler/gateway/anthropic.go`，anthropic / openai 两个入口均调用），**不是全量透传，而是只读一份固定 allow-list**（`clientMetadataHeaders`）：

```text
x-stainless-lang
x-stainless-package-version
x-stainless-os
x-stainless-arch
x-stainless-runtime
x-stainless-runtime-version
x-stainless-retry-count
x-stainless-timeout
x-stainless-helper-method
x-app
x-claude-code-session-id
x-client-request-id
```

规则：

- 逐个读 allow-list header，**空值跳过**，一个都未命中则不附加任何 header。
- 用途是改善上游 **prompt cache 分区**与 analytics（提高 cache 命中率）。
- 明确**不透传** IP、`User-Agent`、`Authorization` 等含 PII 或身份的 header。
- 透传的是**下游客户端自带**的 SDK 指纹（如真实 Claude Code 的 `x-stainless-*` / `x-app`）；OmniHub 不在此处伪造指纹，指纹真实性取决于下游实际发送的内容。

这条与第 16 节 “`X-OmniHub-*` 默认不透传给上游” 不冲突：`X-OmniHub-*` 是 OmniHub 自有 header（不透传），`x-stainless-*` / `x-app` 是上游 SDK header（按 allow-list 透传），两套 header 各司其职。

### Forwarder 现有统一约束

Forwarder 当前会：

- 应用 `account.CustomHeaders`。
- 删除 `X-Forwarded-Host`、`X-Forwarded-Proto`、`X-Real-IP`、`Forwarded` 等转发链路 header。
- 默认删除 `X-Forwarded-For`，除非账号显式开启 `ForwardClientIP`。
- 强制 `Accept-Encoding: identity`，避免流式 SSE 被压缩缓冲破坏。

这说明现在已经有两个参数入口：

```text
协议字段：ir.UnifiedRequest → ProviderDriver → upstream body
额外 header：account.CustomHeaders / ClientMetadata → Driver / Forwarder
```

本方案要做的是把它们升级成更清晰的“认证插件 + Driver 参数 + Client Profile + 账号分发”体系。

---

## 3. 开源项目调研结论

### 3.1 Codex / Claude / Gemini 代理类项目

调研对象：

- `codexProapi`
- `codex-openai-proxy`
- `codex-gateway`
- `CLIProxyAPI`
- `proxypool-hub`
- `auth2api`
- `claude-oauth-proxy`
- `openai-codex-lb`
- `codex-pooler`
- `oauth-mux`

共同做法：

```text
1. 管理多个 OAuth / CLI 登录态 / API Key 账号
2. 对下游暴露 OpenAI-compatible / Anthropic-compatible API
3. 请求进来后选择一个上游账号
4. 检查 token 是否过期，必要时 refresh
5. 根据协议转换请求 body
6. 将响应转回下游协议
7. 记录 usage、状态、quota、日志
```

常见账号分发策略：

- `round_robin`
- `sticky`
- `random`
- `usage_balanced`
- `quota_aware`
- `least_loaded`
- `pinned account`
- `fallback chain`
- `cooldown after 429/5xx`
- `disable after 401/403`

### 3.2 sub2api 调研

项目：<https://github.com/Wei-Shaw/sub2api>

`sub2api` 的定位更接近 OmniHub：

```text
AI API Gateway Platform for Subscription Quota Distribution
```

它不是简单 OAuth proxy，而是完整平台：

```text
上游订阅账号 / API Key
  ↓
账号组 / 调度器
  ↓
平台 API Key
  ↓
用户请求
  ↓
鉴权、计费、限流、sticky、转发
```

值得借鉴的关键点：

#### 平台 API Key 与上游账号解耦

用户拿到的是平台生成的 API Key，而不是上游账号凭证：

```text
用户 API Key
  ↓
OmniHub 鉴权 / 计费 / 限流
  ↓
选择上游 OAuth 或 API Key 账号
```

这与 OmniHub 的 virtual key 模型一致。

#### sticky session 是账号分发核心

sub2api 文档特别提醒 Nginx 要开启：

```nginx
underscores_in_headers on;
```

原因是 Codex CLI 等客户端可能使用 `session_id` 这类带下划线的 header。Nginx 默认会丢弃带下划线的 header，导致多账号环境下 sticky session 失效。

OmniHub 应在部署文档里明确提醒：

```text
如果使用 Nginx / API Gateway / CDN，必须保留 session_id、conversation_id、prompt_cache_key 等会话信号。
```

#### session hash 必须在账号选择前生成

sub2api 的 issue 中暴露过一个问题：如果没有显式 `session_id`、`conversation_id`、`prompt_cache_key`，请求会随机分配到不同账号，影响 prompt cache 和上下文连续性。

OmniHub 应在 Resolver 前生成 `RouteContext.SessionHash`：

```text
1. X-OmniHub-Session-ID
2. session_id
3. conversation_id
4. prompt_cache_key
5. metadata.user_id
6. cache_control ephemeral marker
7. system + tools + 前 N 条 messages 的内容摘要
```

#### sticky 命中的账号必须重新校验

sticky 不是简单：

```text
session_hash -> account_id
```

而是：

```text
virtual_key_id + provider_group_id + model_family + session_hash
  ↓
account_id
```

命中后仍必须检查：

- account 是否 enabled。
- account 是否仍属于当前 group。
- auth_status 是否可用。
- 是否 cooldown。
- quota 是否足够。
- concurrency 是否可用。
- 是否支持当前模型。

### 3.3 sub2api fork 优化趋势

调研了 `sub2api` 的主要 fork：

| Fork | 优化方向 |
| --- | --- |
| `nianzs/sub2api` | 长期维护 Kiro；OAuth / AWS Builder ID / token 导入；Kiro 缓存模拟；按分组控制缓存模拟比例 |
| `xiangking/sub2api-kiro` | 专注 Kiro；授权、refresh、转发、模型映射、quota 展示；Claude Code 风格兼容 |
| `TokenFlux/TokenRouter` | 支付增强、安全限流、Sora 媒体签名 URL、h2c、外部系统 iframe、Antigravity 混合调度 |
| `Blue-Seventeen/sub2api` | Claude Code / Codex / Cherry Studio 深链路兼容；Kimi / GLM 修复；Responses 链路增强；图片返回归一；成本统计优化；账号自动运维和代理池 |
| `AFreeCoder/apipool` | 安装向导、Redis、优雅退出、Nginx header 注意事项、批量账号操作约束 |
| `Ming-321/sub2api-openai_pro` | 针对 OpenAI 订阅优化，公开 README 差异较少 |

对 OmniHub 的启发：

```text
1. 实验订阅渠道适合插件化独立维护
2. sticky session 和 prompt cache 是分发核心
3. 真实客户端兼容比协议兼容更细
4. 账号健康要包含代理、区域、成功率、冷却、自动下线
5. usage / cost / billing 需要按 user / key / group / account 归因
6. 管理后台、部署文档、安全限流要同步完善
```

---

## 4. 总体架构

```text
Admin UI
  ↓
Admin API
  ↓
UpstreamAuthPlugin
  - BeginAuth
  - ExchangeCallback
  - ImportCredentials
  - Refresh
  - Validate
  - Revoke
  ↓
accounts.credentials / auth_status

Client / CLI / SDK
  ↓
OmniHub API
  ↓
Guard / Virtual Key / Billing / Limits
  ↓
RouteContext Builder
  - client profile
  - session hash
  - request id
  - model family
  ↓
Resolver
  - account group
  - sticky binding
  - quota / health / cooldown
  ↓
TokenManager
  - ensure fresh token
  - refresh lock
  - 401 retry refresh
  ↓
ProviderDriver
  - protocol transform
  - header injection
  - stream parser
  ↓
Forwarder
  - endpoint failover
  - proxy
  - custom headers
  - security cleanup
  ↓
Upstream Provider
```

热路径：

```text
RouteContext → Resolver → TokenManager → ProviderDriver → Forwarder
```

冷路径：

```text
Admin OAuth login / import / refresh / validate / revoke
```

OAuth 插件只在冷路径和 token refresh 时参与，不进入每个流式 chunk。

---

## 5. 数据模型

### 5.1 accounts 扩展

建议新增：

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
  ADD COLUMN refresh_error TEXT,
  ADD COLUMN client_profile TEXT,
  ADD COLUMN client_profile_config JSONB NOT NULL DEFAULT '{}'::jsonb;
```

`auth_type`：

```text
api_key
oauth
imported_oauth
service_account
adc
worker
```

`auth_status`：

```text
ok
expiring
refreshing
refresh_failed
login_required
revoked
quota_exhausted
rate_limited
tier_insufficient
unsupported_region
disabled
```

### 5.2 OAuth session 表

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

- `account_id` 为空：创建新账号。
- `account_id` 不为空：重新登录已有账号。
- `code_verifier` 属于敏感字段，应加密或短期保存。

### 5.3 sticky binding 表

```sql
CREATE TABLE account_sticky_bindings (
    id                 BIGSERIAL PRIMARY KEY,
    virtual_key_id      BIGINT,
    user_id             BIGINT,
    provider_group_id   BIGINT NOT NULL,
    model_family        TEXT NOT NULL,
    session_hash        TEXT NOT NULL,
    account_id          BIGINT NOT NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at          TIMESTAMPTZ NOT NULL,
    UNIQUE (virtual_key_id, provider_group_id, model_family, session_hash)
);
```

### 5.4 account runtime state

可先复用现有 health tracker，后续再落表或 Redis：

```text
account_id
status
cooldown_until
rate_limit_reset_at
quota_reset_at
last_error_type
last_error_message
inflight_count
success_count
failure_count
latency_ewma_ms
```

### 5.5 routing bindings

用于手动绑定用户、Key、客户端或模型到账号组：

```sql
CREATE TABLE routing_bindings (
    id              BIGSERIAL PRIMARY KEY,
    scope_type      TEXT NOT NULL, -- virtual_key / user / client / model
    scope_id        TEXT NOT NULL,
    provider        TEXT NOT NULL,
    model_pattern   TEXT,
    group_id        BIGINT NOT NULL,
    sticky_mode     TEXT NOT NULL DEFAULT 'session',
    priority        INTEGER NOT NULL DEFAULT 0,
    enabled         BOOLEAN NOT NULL DEFAULT TRUE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

---

## 6. Credentials 规范

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

导入 `~/.codex/auth.json`、Claude Code credentials、Gemini CLI credentials 时，不建议原样长期保存完整文件。推荐解析成标准字段：

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

---

## 7. UpstreamAuthPlugin SPI

建议新增冷路径插件接口：

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

插件职责：

```text
登录
回调
导入凭证
刷新 token
校验账号 profile
撤销登录
```

插件不负责：

```text
模型请求 body
prompt / messages / tools / images
账号池调度
计费
流式响应解析
```

### Metadata 示例

```json
{
  "name": "codex-oauth",
  "display_name": "OpenAI Codex OAuth",
  "supported_providers": ["openai-codex"],
  "auth_methods": ["browser_oauth", "device_code", "import_auth_json"],
  "experimental": true
}
```

---

## 8. 管理端流程

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
- routing group。
- cooldown / quota 状态。
- 是否实验功能。

---

## 9. 运行时请求参数设计

请求参数分三层：

```text
1. 下游用户请求参数
2. OmniHub 内部统一请求参数
3. 上游供应商请求参数
```

OAuth 插件只处理认证材料，不接收完整模型请求内容。

### AuthMaterial

建议在运行时引入：

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

### Header 规则

OpenAI API Key：

```http
Authorization: Bearer <api_key>
```

Codex OAuth：

```http
Authorization: Bearer <access_token>
Content-Type: application/json
Accept-Encoding: identity
```

Anthropic API Key：

```http
x-api-key: <api_key>
anthropic-version: 2023-06-01
Content-Type: application/json
```

Claude OAuth：

```http
Authorization: Bearer <access_token>
Content-Type: application/json
Accept-Encoding: identity
```

### 参数合并顺序

Driver 构造请求时按以下顺序合并：

```text
1. Driver 默认协议参数
2. 请求入口解析出的标准参数
3. 账号 ParamOverrides
4. Client Profile 默认参数
5. 账号 CustomHeaders
6. Forwarder 安全约束
```

第 6 步永远最后执行，防止自定义参数破坏安全边界。

---

## 10. OmniHub 自有 CLI 指纹规范

这里的目标不是复制 Claude Code / Codex CLI 的私有字段，而是学习成熟 CLI 的参数分层方式，设计 OmniHub 自己的 CLI 协议。

### 推荐 Header

OmniHub CLI 请求 OmniHub 时，可以发送：

```http
User-Agent: OmniHubCLI/0.1.0 (darwin; arm64)
X-OmniHub-Client: cli
X-OmniHub-Client-Version: 0.1.0
X-OmniHub-Client-Platform: darwin/arm64
X-OmniHub-Client-Mode: interactive
X-OmniHub-Session-ID: sess_01J...
X-OmniHub-Request-ID: req_01J...
X-OmniHub-Install-ID: inst_01J...
X-OmniHub-Capabilities: streaming,tools,vision,thinking
X-OmniHub-Protocol: openai-chat
```

这些字段默认只在“客户端 → OmniHub”之间使用，不直接透传给上游。

### Session ID

`X-OmniHub-Session-ID` 表示“本地工作会话”，不表示用户身份。

推荐生成：

```text
sess_<ulid>
```

用途：

- Resolver sticky 路由。
- 上下文隔离。
- 日志关联。
- 本地重试去重。
- 后续 session cache。

不应包含：

- 用户邮箱。
- 机器名。
- 绝对路径。
- Git remote URL。
- 项目名原文。

### Install ID

```text
inst_<random-ulid>
```

要求：

- 首次运行随机生成。
- 保存在本地配置。
- 用户可删除或重置。
- 不从硬件序列号、MAC 地址、用户名派生。
- 不透传给上游供应商。

### Capabilities

```http
X-OmniHub-Capabilities: streaming,tools,vision,thinking,json-mode
```

也可以做能力协商：

```http
GET /v1/omnihub/capabilities
Authorization: Bearer <virtual-key>
```

响应：

```json
{
  "server_version": "0.1.0",
  "protocols": ["openai-chat", "openai-responses", "anthropic-messages"],
  "features": ["streaming", "tools", "vision", "thinking"],
  "client_headers": [
    "X-OmniHub-Session-ID",
    "X-OmniHub-Request-ID",
    "X-OmniHub-Capabilities"
  ]
}
```

### CLI 请求示例：Codex 风格

```http
POST /v1/responses
Authorization: Bearer ohk_...
Content-Type: application/json
User-Agent: OmniHubCLI/0.1.0 (darwin; arm64)
X-OmniHub-Client: cli
X-OmniHub-Client-Version: 0.1.0
X-OmniHub-Client-Mode: interactive
X-OmniHub-Session-ID: sess_01J...
X-OmniHub-Request-ID: req_01J...
X-OmniHub-Capabilities: streaming,tools,thinking
```

body：

```json
{
  "model": "gpt-5-codex",
  "input": [
    {
      "role": "user",
      "content": [
        {
          "type": "input_text",
          "text": "帮我分析这个仓库的认证流程"
        }
      ]
    }
  ],
  "reasoning": {
    "effort": "high"
  },
  "tools": [],
  "stream": true,
  "metadata": {
    "omnihub_client": "cli",
    "omnihub_session_id": "sess_01J...",
    "omnihub_request_id": "req_01J..."
  }
}
```

Driver 映射：

```text
下游 ohk_* virtual key：只用于 OmniHub 鉴权
上游 Authorization：由 TokenManager 注入 OAuth access_token
X-OmniHub-Session-ID：用于 sticky 和日志，不直接变成上游 session id
reasoning.effort：可映射到上游稳定参数
tools / tool_choice：由 Driver 转换和过滤
metadata.omnihub_*：默认不透传上游
```

### CLI 请求示例：Claude 风格

```http
POST /v1/messages
Authorization: Bearer ohk_...
Content-Type: application/json
User-Agent: OmniHubCLI/0.1.0 (darwin; arm64)
X-OmniHub-Client: cli
X-OmniHub-Client-Version: 0.1.0
X-OmniHub-Client-Mode: interactive
X-OmniHub-Session-ID: sess_01J...
X-OmniHub-Request-ID: req_01J...
X-OmniHub-Capabilities: streaming,tools,thinking
```

body：

```json
{
  "model": "claude-sonnet-4",
  "max_tokens": 4096,
  "system": [
    {
      "type": "text",
      "text": "You are OmniHub CLI, a coding assistant running inside the user's project."
    }
  ],
  "messages": [
    {
      "role": "user",
      "content": [
        {
          "type": "text",
          "text": "帮我检查这个 PR 的风险点"
        }
      ]
    }
  ],
  "tools": [],
  "thinking": {
    "type": "enabled",
    "budget_tokens": 2048
  },
  "stream": true,
  "metadata": {
    "omnihub_client": "cli",
    "omnihub_session_id": "sess_01J...",
    "omnihub_request_id": "req_01J..."
  }
}
```

Driver 映射：

```text
下游 ohk_* virtual key：只用于 OmniHub
上游 Anthropic API Key：x-api-key
上游 Claude OAuth：Authorization: Bearer <access_token>
anthropic-version：由 Driver 默认值或下游显式字段控制
anthropic-beta：只允许公开、可配置、可审计的 beta capability
system：OmniHub CLI 可以定义自己的 system 提示
X-OmniHub-Session-ID：用于 sticky 路由和日志，不直接透传
```

---

## 11. 账号分发设计

### 11.1 RouteContext

请求进入 Resolver 前先构造：

```go
type RouteContext struct {
    VirtualKeyID   int64
    UserID         *int64
    Provider       string
    Model          string
    ModelFamily    string
    ClientType     string
    ClientMode     string
    RequestID      string
    SessionHash    string
    PromptCacheKey string
    Capabilities   []string
}
```

### 11.2 SessionHash 生成顺序

```text
1. X-OmniHub-Session-ID
2. session_id
3. conversation_id
4. prompt_cache_key
5. metadata.user_id
6. cache_control ephemeral marker
7. system + tools + 前 N 条 messages 的内容摘要
```

### 11.3 Routing Policy

账号组支持：

```text
round_robin
weighted_random
sticky_by_session
sticky_by_virtual_key
usage_balanced
quota_aware
latency_aware
pinned_account
```

第一版建议先实现：

```text
round_robin
weighted_random
sticky_by_session
sticky_by_virtual_key
```

### 11.4 Sticky 校验

sticky 命中后必须重新校验：

```text
account enabled?
account in current group?
auth_status ok?
not cooldown?
quota available?
concurrency available?
model supported?
active window matched?
```

任何一个不满足，就重新调度。

### 11.5 Fallback Policy

```text
subscription_first
api_key_first
cheapest_first
stable_first
same_provider_only
no_fallback
```

示例：

```text
Codex OAuth 账号 429
  ↓
切到另一个 Codex OAuth 账号
  ↓
仍失败
  ↓
fallback 到 OpenAI API Key
```

### 11.6 失败状态机

```text
2xx:
  记录成功，保持 available

401 / 403:
  标记 auth_failed 或 login_required
  跳过账号
  可触发一次 refresh retry

429:
  标记 rate_limited
  设置 cooldown_until
  换下一个账号

quota exceeded:
  标记 quota_exhausted
  等待 quota window 恢复

5xx / transport error:
  短 cooldown
  换下一个账号

tier insufficient:
  标记 tier_insufficient
  不再为该模型选择此账号
```

### 11.7 状态标签

参考 `oauth-mux`，推荐稳定状态词：

```text
available
rate_limited
quota_exhausted
tier_insufficient
auth_failed
login_required
credential_unavailable
revalidation_needed
cooldown
disabled
```

---

## 12. TokenManager

`TokenManager` 位于 Resolver 和 Driver 之间。

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

---

## 13. Codex 订阅账号设计

```text
provider = openai-codex
auth_type = oauth / imported_oauth
auth_plugin = codex-oauth
client_profile = codex-compatible
```

Driver 职责：

- `/v1/responses` 透传或轻量转换到 Codex-compatible backend。
- `/v1/chat/completions` 转换成 Responses 风格。
- 处理 SSE 流。
- 处理 tools / tool_calls 映射。
- 处理 model 与 reasoning effort 映射。
- 支持模型列表和 quota 查询。

第一阶段优先：

```text
导入 auth.json
/v1/responses
streaming
refresh token
401 refresh retry
```

第二阶段：

```text
OAuth PKCE
device code
/v1/chat/completions 转换
quota-aware routing
websocket / previous_response_id support
```

---

## 14. Claude 订阅账号设计

建议分三类：

```text
anthropic-api-key         稳定
claude-code-worker        较稳，调用官方 SDK / CLI
claude-subscription-oauth 实验
```

OAuth 形态：

```text
provider = claude-subscription
auth_type = oauth / imported_oauth
auth_plugin = claude-oauth
client_profile = claude-compatible
```

Driver 职责：

- Anthropic Messages 原生请求。
- OpenAI Chat Completions 到 Anthropic Messages 的转换。
- Claude 流式事件到 OmniHub IR 的转换。
- 工具调用映射。
- thinking / beta capability 映射。

第一阶段优先：

```text
/v1/messages
streaming
thinking 参数
anthropic-beta allowlist
OAuth refresh
```

---

## 15. Worker 型账号

对于 Claude Code SDK / CLI、Codex CLI 这类官方客户端，可以使用 worker 模式：

```text
provider = claude-code-worker / codex-worker
auth_type = worker
```

请求路径：

```text
OmniHub
  ↓
Worker Queue
  ↓
Claude Code SDK / Codex CLI
  ↓
官方客户端自己处理认证和调用
```

适合：

- 代码任务。
- 低并发。
- 本地或小团队。
- 强交互 agent。

不适合：

- 高并发通用聊天 API。
- 大规模商业分发。
- 强 SLA 的统一模型网关。

---

## 16. 安全与隐私

必须满足：

1. `credentials`、`code_verifier`、refresh token 加密存储。
2. 后台 API 返回账号时脱敏凭证。
3. OAuth callback 校验 `state`。
4. OAuth session 有过期时间。
5. Refresh token 轮换时原子写回。
6. 同一个账号刷新加锁。
7. 导入 `auth.json` 后只保存必要字段，不在日志输出原文。
8. 默认不把 OAuth 代理暴露到公网。
9. 下游用户身份不透传给上游。
10. `X-OmniHub-*` 默认不透传给上游。
11. session id、install id 不包含用户隐私、机器名、路径、项目名。
12. 真实 prompt / files / images 默认不进入长期日志。

---

## 17. 分阶段落地

### 阶段 1：账号认证模型

- `accounts` 增加 `auth_type`、`auth_plugin`、`auth_status`、`auth_expires_at` 等字段。
- `provider.Account` 增加对应字段。
- Repository 读写这些字段。
- 后台账号列表展示认证方式和状态。

### 阶段 2：TokenManager + 导入式 OAuth

- 新增 `UpstreamAuthProvider` 本地接口。
- 新增 `TokenManager`。
- 支持导入 Codex `auth.json`。
- 支持 token 过期前刷新。
- 支持 401 强制刷新重试。

### 阶段 3：RouteContext + sticky

- 构造 `RouteContext`。
- 生成 `SessionHash`。
- 支持 sticky binding。
- 支持 group 校验。
- 增加 Nginx / proxy header 文档。

### 阶段 4：Codex experimental provider

- 新增 `openai-codex` Driver。
- 支持 `/v1/responses`。
- 支持基础模型列表。
- 支持流式响应。
- 后台可以测试 Codex OAuth 账号。

### 阶段 5：Claude experimental provider

- 支持 Claude credentials 导入或 Claude OAuth 插件。
- 新增 `claude-subscription` Driver 或 `claude-code-worker`。
- 第一版优先支持 Anthropic Messages。

### 阶段 6：账号池增强

- `round_robin`、`weighted_random`、`sticky_by_session`。
- 429 cooldown。
- quota 查询和展示。
- per-account 并发限制。
- usage-balanced / quota-aware。
- subscription-first / api-key-first fallback。

### 阶段 7：OmniHub CLI

- 定义 OmniHub CLI headers。
- 生成 session id、request id、install id。
- 支持 capabilities 协商。
- 支持本地配置与诊断命令。

---

## 18. 推荐默认策略

生产稳定路径：

```text
OpenAI API Key
Anthropic API Key
Bedrock
Vertex
Teams / Enterprise OAuth
```

实验路径：

```text
Codex / GPT Pro OAuth
Claude Pro / Max OAuth
Gemini CLI OAuth
Kiro / Antigravity OAuth
```

默认 UI 标签：

```text
实验
适合本地/小团队
不建议作为正式商业供应商
```

默认分发策略：

```text
stable provider group: weighted_random + health check
subscription provider group: sticky_by_session + cooldown + quota aware
enterprise group: dedicated binding + strict concurrency
```

---

## 19. 参考项目

- sub2api：<https://github.com/Wei-Shaw/sub2api>
- nianzs/sub2api：<https://github.com/nianzs/sub2api>
- xiangking/sub2api-kiro：<https://github.com/xiangking/sub2api-kiro>
- TokenFlux/TokenRouter：<https://github.com/TokenFlux/TokenRouter>
- Blue-Seventeen/sub2api：<https://github.com/Blue-Seventeen/sub2api>
- codexProapi：<https://github.com/314051672/codexProapi>
- codex-openai-proxy：<https://github.com/0oAstro/codex-openai-proxy>
- codex-gateway：<https://github.com/LanternCX/codex-gateway>
- CLIProxyAPI：<https://github.com/crazyrabbit0/CLIProxyAPI>
- proxypool-hub：<https://github.com/yiyao-ai/proxypool-hub>
- codex-pooler：<https://github.com/icoretech/codex-pooler>
- openai-codex-lb：<https://github.com/gngeorgiev/openai-codex-lb>
- oauth-mux：<https://github.com/Jesssullivan/oauth-mux>
- auth2api：<https://github.com/AmazingAng/auth2api>
- Meridian：<https://github.com/rynfar/meridian>
- LiteLLM Claude Code Max Subscription：<https://docs.litellm.ai/docs/tutorials/claude_code_max_subscription>

---

## 20. 最终建议

OmniHub 的上游 OAuth 和订阅账号能力应按以下方式落地：

```text
accounts.provider      = 调哪个供应商 / Driver
accounts.auth_type     = 怎么认证
accounts.auth_plugin   = 谁负责认证生命周期
accounts.credentials   = 加密凭证
client_profile         = OmniHub 维护的兼容请求形态
RouteContext           = 请求路由上下文
SessionHash            = sticky 和 cache locality 的核心键
ProviderGroup          = 账号池和策略边界
```

最终请求链路保持：

```text
Client → Guard → RouteContext → Resolver → TokenManager → ProviderDriver → Forwarder → Upstream
```

从 sub2api 和相关 fork 的经验看，订阅账号分发不能停留在“多个账号轮询”。真正可用的系统需要：

```text
平台 Key
账号组
session hash
sticky binding
账号健康状态机
quota-aware 调度
fallback chain
usage / cost 归因
管理后台和部署约束
```

OmniHub 已经有账号池、Provider Driver、Forwarder、计费和管理后台基础，最合适的路线是先做 `auth_type + TokenManager + sticky_by_session`，再逐步扩展到 Codex / Claude experimental provider 和 quota-aware 分发。
