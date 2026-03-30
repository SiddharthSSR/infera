---
name: security_assessment_2026_03_30
description: Comprehensive security assessment of Infera project covering auth, secrets, Docker, CORS, TLS, rate limiting, and worker auth
type: project
---

Security assessment completed 2026-03-30. Key findings:

**Critical**: Worker token comparison uses == (timing side-channel), /metrics endpoint unauthenticated, provider API keys/secrets stored plaintext in SQLite, bootstrap admin key persisted to disk in plaintext, vLLM worker Dockerfile runs as root.

**High**: DefaultConfig allows CORS wildcard origin *, no Access-Control-Max-Age set, worker-to-gateway communication uses HTTP (not mTLS), no request body size limit on JSON decoders (only on HTTP layer).

**Positive**: API keys hashed with SHA-256 before storage, session cookies use HttpOnly+Secure+SameSite=Strict, RBAC with permission checks, parameterized SQL queries (no injection), multi-stage Docker builds with non-root users (gateway, sglang, tensorrt), rate limiting with token bucket, circuit breaker on workers, Caddy provides HSTS+security headers.

**Why:** Pre-production security baseline needed before public deployment.
**How to apply:** Prioritize critical items before any internet-facing deployment.
