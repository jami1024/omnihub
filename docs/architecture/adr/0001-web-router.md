# ADR 0001 — HTTP Router and Middleware Stack

- **Status:** Accepted
- **Date:** 2026-05-17
- **Decision makers:** @jami1024

## Context

OmniHub is a Go-based AI gateway whose hot path is dominated by:

1. A long chain of middleware-like "Guards" (Auth, RateLimit, Quota, Session,
   Resolver, CircuitBreaker, Forwarder, Response).
2. Streaming response forwarding (SSE for OpenAI/Anthropic, AWS EventStream for
   Bedrock).
3. Heavy use of `httputil.ReverseProxy`-style transforms when forwarding to
   upstream LLM providers.
4. An admin API surface (account management, virtual keys, billing) that follows
   conventional CRUD patterns.

The project is also a longer-term effort that may attract third-party plugin
authors. The chosen framework therefore has implications for community on-ramp.

## Options considered

| Option | Pros | Cons |
| ------ | ---- | ---- |
| **Gin** | Largest ecosystem (~88k stars); `binding` + `c.JSON` sugar; familiar to most Go developers; sub2api uses it in production; abundant middleware and tutorial coverage. | `*gin.Context` leaks into business code; middleware signature is framework-specific; some streaming sugar (`c.Stream`) is easier to bypass than to use. |
| **chi** | 100% `net/http` compatible; Guard pipeline maps 1:1 to `func(http.Handler) http.Handler`; trivially testable with `httptest`; minimal core. | Smaller ecosystem (~18k stars); fewer ready-made middleware; CRUD endpoints need hand-written render/validate helpers. |
| **Echo** | Cleaner Context than Gin; `return err` middleware ergonomics. | Mid-size ecosystem; still introduces a custom `echo.Context`; no compelling advantage over either Gin or chi for this project. |
| **Fiber** | Highest raw throughput. | Built on `fasthttp`; not `net/http`-compatible; no HTTP/2; cannot use `httputil.ReverseProxy`. **Rejected outright.** |

## Decision

**Use `github.com/gin-gonic/gin` v1.10.x as the HTTP router and middleware stack.**

Concretely:

- Handlers are `gin.HandlerFunc` (signature `func(c *gin.Context)`).
- Guards are also `gin.HandlerFunc`, registered via `r.Use(...)` or `Group.Use(...)`.
- Streaming endpoints drop directly into `c.Writer` and assert `http.Flusher` rather
  than relying on `c.Stream` — Gin's sugar is helpful for CRUD, less so for SSE.
- Business code (`internal/service/`, `internal/repository/`) must not depend on
  `*gin.Context`. Pass `context.Context` and plain typed values across layer
  boundaries to keep the option of swapping the router later non-zero but bounded.

## Consequences

**Positive**

- Fastest path to a working MVP: validation, binding, recovery, logging all come
  out of the box.
- Direct reuse of patterns and idioms already proven in sub2api.
- Easier onboarding for collaborators who likely already know Gin.
- Larger pool of existing middleware (rate limit, JWT, CORS, gzip, etc.) ready
  to drop in.

**Negative**

- `*gin.Context` is framework-specific. Any handler / Guard depending on it is
  not portable to chi/Echo without rewriting. We mitigate this by restricting
  `*gin.Context` to the `internal/handler/` and `internal/service/guard/`
  packages — business code below those layers is stdlib-typed.
- The `c.Abort()` / `c.Next()` flow is easy to get wrong (forgetting `return`
  after `c.AbortWithStatusJSON` lets the chain keep executing). Convention to
  follow: every error branch ends with an explicit `return`, and Guards are
  reviewed for this on PR.

**Neutral**

- Performance: framework-level overhead is ≈10µs per request. LLM upstream
  response time is 500ms–30s. Choice of Gin / chi / Echo is performance-equivalent
  for this workload.

## Out of scope / revisit triggers

The choice should be re-examined if any of the following becomes true:

1. `*gin.Context` starts leaking below `internal/service/guard/` despite the
   convention above — that is a sign the boundary is not holding and the cost
   of switching grows.
2. A future requirement mandates HTTP/3 in a way that requires more than the
   standard `quic-go/http3` wrapper around our router.
3. Gin's maintenance cadence stalls or a breaking v2 is forced on the ecosystem.

## References

- [Gin web framework](https://github.com/gin-gonic/gin) (~88.5k stars, 2014–)
- [sub2api](https://github.com/jami1024/sub2api) — production reference, runs Gin.
- [smallnest/go-web-framework-benchmark](https://github.com/smallnest/go-web-framework-benchmark)
- Internal discussion on framework selection (May 2026): chi was the initial
  recommendation but was reconsidered in light of ecosystem size and team
  familiarity. The decision to adopt Gin was made after weighing those factors
  against chi's stdlib alignment.
