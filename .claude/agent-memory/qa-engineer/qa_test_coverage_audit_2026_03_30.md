---
name: Test Coverage Audit - 2026-03-30
description: Comprehensive test coverage audit of Infera project — Python worker + Go gateway, ranked gaps by risk
type: project
---

## Summary

**Python tests**: 19 test files covering worker, engines (mock/vllm/sglang/tensorrt), types, config, HTTP server, benchmarking.
**Go tests**: 32 test files covering gateway, auth, providers, router, vault, audit, deployments, benchmarks.

## Critical Gaps

1. **VLLMEngine.infer() and infer_stream() have ZERO test coverage** — only config wiring and load_model tested.
2. **Gateway handleChatCompletions inference hot path** — tested via contract tests with fake transport, but no test for backpressure (maxInFlight), inference timeout, or retry/fallback on worker failure.
3. **WorkerClient.InferStream goroutine error paths** — circuit breaker integration tested in isolation but not the streaming goroutine's error/EOF/context-cancel handling.
4. **HTTP server handle_infer (non-streaming)** — no test exercises the happy path through handle_infer with a real mock worker.
5. **Worker._run_gpu_preflight** — only tests the failure stub; the actual subprocess logic (parsing stdout, stderr, timeout kill) is untested.
6. **No integration tests** exist anywhere — all tests use mocks/fakes.

## Test Quality Observations
- Python engine tests heavily rely on monkeypatching, which means they validate wiring but NOT actual engine behavior.
- Go gateway contract tests are well-structured with proper SSE validation.
- Auth middleware has good coverage of Bearer, X-API-Key, session cookie, and role-based access.
- Router tests cover batching, affinity, and fallback strategies well.

## How to apply
Use this as the baseline for prioritizing test gap closure. VLLMEngine inference paths and gateway backpressure are the highest-risk items.
