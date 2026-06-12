# 反向代理 Header 配置（Nginx / Caddy）

OmniHub 通常部署在 Nginx / Caddy / 云 LB 之后。要让粘性路由（sticky
session）、真实客户端 IP 和 SSE 流式都正常工作，代理层需要满足以下要求。

## 必须透传给 OmniHub 的 header

| Header | 用途 |
| --- | --- |
| `X-OmniHub-Session-ID` | 客户端会话标识；SessionHash 的最高优先级来源（设计文档 §11.2），决定粘性路由。 |
| `X-OmniHub-Request-ID` / `X-OmniHub-*` | 客户端协议 header（设计文档 §10），日志关联 / 重试去重。 |
| `Authorization` | 虚拟 key 认证。 |
| `anthropic-beta`、`x-stainless-*`、`x-app`、`x-claude-code-session-id`、`x-client-request-id` | SDK 指纹 allow-list；改善上游 prompt cache 命中率。 |
| `X-Forwarded-For` | 真实客户端 IP。须与 `OMNIHUB_TRUSTED_PROXIES` 配合（见下）。 |

Nginx 默认会透传未知 header（下划线 header 除外），通常无需逐个配置；
**注意不要在代理层 `proxy_set_header X-OmniHub-Session-ID ""` 之类地清空它们**。
如果客户端使用了带下划线的自定义 header，需要 `underscores_in_headers on;`。

## 真实客户端 IP

OmniHub 只在对端 IP 命中 `OMNIHUB_TRUSTED_PROXIES`（逗号分隔的 CIDR）时才信任
`X-Forwarded-For`，否则取直连对端 IP。代理层配置：

```nginx
location / {
    proxy_pass http://omnihub:8080;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header Host $host;
}
```

OmniHub 环境变量：

```bash
OMNIHUB_TRUSTED_PROXIES=10.0.0.0/8,172.16.0.0/12   # 按实际代理网段
```

未设置时 OmniHub 不信任任何代理（`c.ClientIP()` 返回 LB 自身地址），按 IP 的
限流 / 封禁会失效。

## SSE 流式

`/v1/messages`、`/v1/chat/completions`、`/v1/responses` 都是 SSE 流式端点，
代理层必须关闭缓冲并放宽超时：

```nginx
location ~ ^/v1/(messages|chat/completions|responses)$ {
    proxy_pass http://omnihub:8080;
    proxy_buffering off;            # 等价于 OmniHub 回包里的 X-Accel-Buffering: no
    proxy_cache off;
    proxy_read_timeout 600s;        # 长推理 / thinking 请求
    proxy_http_version 1.1;
    proxy_set_header Connection "";
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header Host $host;
}
```

OmniHub 已经在 SSE 响应上发送 `X-Accel-Buffering: no`，支持该 header 的
Nginx 会自动按请求关缓冲；显式 `proxy_buffering off` 是双保险。

## 不透传给上游的 header

`X-OmniHub-*` 只在「客户端 → OmniHub」之间有意义（设计文档 §16）。OmniHub 的
driver 从零构建上游请求、只复制 SDK 指纹 allow-list，因此这些 header 天然不会
泄漏给上游供应商，代理层无需额外处理。
