# OmniHub

[English](README.md) · **简体中文**

> **统一 AI 网关 — 供应商可插拔、支付可插拔、可观测性内置，用 Go 实现。**

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Status: Early](https://img.shields.io/badge/status-early%20development-orange)]()
[![Go Version](https://img.shields.io/badge/go-1.24+-00ADD8?logo=go)]()

OmniHub 是一个开源 AI 网关，把 OpenAI、Anthropic、AWS Bedrock、Google Gemini、
Vertex AI、Azure OpenAI 等众多 LLM 服务统一在 OpenAI 兼容的 API 之下。

它从同一份代码里服务三种场景，仅通过配置切换：

- **团队** — 多账号统一调度、预算管控、审计日志。
- **企业** — SSO/RBAC、合规审计、多租户治理。
- **SaaS 运营商** — 套餐管理、支付插件、多级分销。

## ✨ 核心特性

- 🔌 **插件优先** — 支付、认证、通知、长尾 LLM 供应商均以独立 gRPC 子进程的形式
  存在。第三方可以用任意语言开发自己的插件。
- 🚪 **多协议入口** — 同一个网关同时接受 Anthropic Messages、OpenAI Chat Completions、
  Gemini、Bedrock 风格的请求。
- 🧠 **智能路由** — 会话粘性、熔断器、权重 / 优先级 / 标签路由。最大化 Prompt Cache
  命中率，最小化供应商绑定。
- 💰 **Token 级计费** — 虚拟 Key 支持单 Key 预算、团队 / 组织汇总、实时成本看板。
- 📊 **可观测性内置** — Prometheus 指标、OpenTelemetry 链路追踪、决策链审计日志。
- 🚀 **单二进制** — Go 后端 + 内嵌 React UI。Docker / Compose / K8s / systemd 全支持。

## 🚧 当前状态

OmniHub 处于**早期开发阶段**。架构与 SPI 在公开设计中——见
[`docs/architecture/`](docs/architecture/) 的设计文档与 ADR。

首个 MVP 目标：Anthropic + OpenAI 两家供应商 + 最小 Guard 管线。关注本仓库获取后续更新。

## 🚀 快速开始（开发环境）

需要 Go 1.24+。

```bash
git clone https://github.com/jami1024/omnihub.git
cd omnihub
make run
```

默认监听 `:8080`，验证启动成功：

```bash
curl http://localhost:8080/healthz
curl http://localhost:8080/version
```

如需修改监听地址：`OMNIHUB_LISTEN=:9000 make run`。

## 📐 架构预览

```
客户端 → 边缘层 (Auth / RateLimit) → 协议适配 → Guard 管线
       → Provider Driver (内置或插件) → 上游 LLM
       → 异步用量写入 + Prometheus / OTel
```

规划中的插件 SPI（gRPC 进程外）：

- `PaymentProvider` — Stripe、支付宝、微信支付、自定义
- `ProviderDriver` — 长尾 / 实验性 LLM 供应商
- `AuthProvider` — SSO、SAML、OAuth2、LDAP
- `NotificationSink` — Slack、钉钉、飞书、邮件
- `AuditSink` — SIEM 系统
- `BackgroundJob` — 自定义定时任务

完整设计详见 [`docs/architecture/overview.md`](docs/architecture/overview.md)。

## 🤝 参与贡献

项目当前处于设计期 — 欢迎通过 [Issues](https://github.com/jami1024/omnihub/issues)
和 [Discussions](https://github.com/jami1024/omnihub/discussions) 参与讨论。正式的
`CONTRIBUTING.md` 将在开放 PR 后落地。

## 📄 许可证

[MIT](LICENSE) © OmniHub 贡献者
