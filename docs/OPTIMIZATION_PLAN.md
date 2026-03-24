# Infera Optimization Plan

> Codebase audit conducted on the `frontend-improvs` branch (2026-03-18).
> Covers the full stack: Python worker, Go gateway, Go router, Go providers, and React frontend.

---

## Table of Contents

1. [Architecture Overview](#architecture-overview)
2. [Tier 1 — Highest Impact (tokens/sec & throughput)](#tier-1--highest-impact-tokenssec--throughput)
3. [Tier 2 — Production Resilience](#tier-2--production-resilience)
4. [Tier 3 — Observability & Debugging](#tier-3--observability--debugging)
5. [Tier 4 — Frontend Performance](#tier-4--frontend-performance)
6. [Quick Reference: Bottleneck → Fix Mapping](#quick-reference-bottleneck--fix-mapping)
7. [Missing Production-Readiness Features](#missing-production-readiness-features)
8. [Key File Index](#key-file-index)

---

## Architecture Overview

### Python Worker (`python/src/infera_worker/`)

| Component | Notes |
|---|---|
| Entry point | `worker.py` (Worker class) + `http_server.py` (aiohttp REST) |
| Concurrency | `asyncio` + semaphore (max_concurrent_requests) |
| State machine | INITIALIZING → READY → BUSY → DRAINING → SHUTTING_DOWN |
| Engine | Pluggable interface; vLLM production + MockEngine for tests |
| Heartbeat | Every 5s to gateway; 3 consecutive auth failures → self-terminate |
| Drain timeout | **1 second** — kills in-flight streaming requests on shutdown |

### Go Gateway (`go/internal/gateway/`)

| Component | Notes |
|---|---|
| API | OpenAI-compatible `/v1/chat/completions` with streaming |
| Auth | JWT + workspace quota enforcement |
| Backpressure | Hard reject at 100 concurrent in-flight requests |
| Worker client | HTTP with circuit breaker (5-failure threshold, 10s reset) |
| Rate limiter | In-memory token bucket per API key (60 req/min, burst 10) |
| Metrics | Prometheus histograms: RPS, TTFT, TPOT, batch size/wait |

### Go Router (`go/internal/router/`)

| Component | Notes |
|---|---|
| Routing strategies | Least Loaded (default), Round Robin, Latency Based |
| Batching | Per-model queues; 50ms max wait OR 8 requests — no token counting |
| Registry | In-memory; degraded after 10s no heartbeat, removed after 30s |
| Retries | Max 3 retries per request |

### Go Providers (`go/internal/providers/`)

| Component | Notes |
|---|---|
| RunPod | GraphQL API; polls readiness every 5s for up to 10 minutes |
| Runtime presets | Hard-coded Go map: 3 models (Qwen2.5-7B, Qwen3-4B, Kimi-K2.5) |
| Spec decoding | Auto-enabled on GPUs with ≥40GB VRAM |

### Frontend (`frontend/src/`)

| Component | Notes |
|---|---|
| Stack | React 18 + React Router v7 + TanStack Query v5 + Vite |
| Stats polling | **2 seconds** — highest frequency query |
| Bundle | 825KB JS uncompressed; all auth pages eagerly loaded |
| Data fetching | Multiple parallel queries on every page load; no deduplication |

---

## Tier 1 — Highest Impact (tokens/sec & throughput)

### 1. vLLM: Enable Prefix Caching + Chunked Prefill

**File:** `python/src/infera_worker/engines/vllm_engine.py`
**Estimated impact:** +20–40% tokens/sec on repeated prompts; +10–20% throughput on long prompts

Neither `enable_prefix_caching` nor `enable_chunked_prefill` is currently passed to `AsyncEngineArgs`. Both are off by default in vLLM.

- **Prefix caching** reuses the KV cache for requests sharing the same system prompt or conversation prefix. Critical for chat workloads where system prompts are repeated across every request.
- **Chunked prefill** prevents a single long-prompt request from blocking the decode queue. Instead of processing the entire prefill in one step, vLLM breaks it into chunks interleaved with ongoing decode steps.

**Change in `vllm_engine.py`:**
```python
engine_kwargs["enable_prefix_caching"] = self.config.vllm_enable_prefix_caching
engine_kwargs["enable_chunked_prefill"] = self.config.vllm_enable_chunked_prefill
engine_kwargs["max_num_batched_tokens"] = self.config.vllm_max_num_batched_tokens
```

**Add to `config.py`:**
```python
vllm_enable_prefix_caching: bool = Field(default=True, description="Reuse KV cache for shared prompt prefixes")
vllm_enable_chunked_prefill: bool = Field(default=True, description="Interleave prefill chunks with decode steps")
vllm_max_num_batched_tokens: int = Field(default=8192, description="Max tokens per scheduler step; tune per GPU VRAM")
```

**Add to `runtime.go` model presets** — set `max_num_batched_tokens` per model where appropriate (e.g., Kimi-K2.5 on 8×H100 can push 65536+).

---

### 2. vLLM: Multi-Step Scheduling

**File:** `python/src/infera_worker/config.py` + `vllm_engine.py`
**Estimated impact:** +10–25% GPU utilization on A100/H100 with continuous batching

vLLM's V1 engine supports `num_scheduler_steps` — running multiple decode steps per Python-GPU round-trip. On high-VRAM GPUs running smaller models, the Python scheduling overhead becomes a bottleneck relative to actual compute. Setting this to 8–10 steps amortizes that overhead.

```python
# config.py
vllm_num_scheduler_steps: int = Field(default=1, description="Decode steps per scheduler iteration; increase on large GPUs")

# vllm_engine.py
if self.config.vllm_num_scheduler_steps > 1:
    engine_kwargs["num_scheduler_steps"] = self.config.vllm_num_scheduler_steps
```

Recommended values by GPU tier:
- RTX 4090 / L40S: 1 (default — GPU is the bottleneck)
- A100 40GB / 80GB: 4–8
- H100 (single): 8
- H100 (multi): 10+

Expose `INFERA_VLLM_NUM_SCHEDULER_STEPS` as an env var and add it to the runtime preset system so it is set automatically per GPU type.

---

### 3. Speculative Decoding: Expand Model Coverage

**File:** `go/internal/providers/runtime.go`
**Estimated impact:** +15–30% tokens/sec for eligible models on large GPUs

Currently only 3 models have spec decoding presets. The system is already built to handle it — the gap is coverage. Priorities:

| Model | Draft Model | Status |
|---|---|---|
| `Qwen/Qwen2.5-7B-Instruct` | `Qwen/Qwen2.5-0.5B-Instruct` | ✅ Done |
| `Qwen/Qwen3-4B-Thinking-2507` | `[ngram]` | ✅ Done |
| `moonshotai/Kimi-K2.5-Instruct` | `[ngram]` | ✅ Done |
| `Qwen/Qwen2.5-14B-Instruct` | `Qwen/Qwen2.5-0.5B-Instruct` | ❌ Missing |
| `Qwen/Qwen2.5-32B-Instruct` | `Qwen/Qwen2.5-1.5B-Instruct` | ❌ Missing |
| `meta-llama/Llama-3.1-8B-Instruct` | `[ngram]` | ❌ Missing |
| `mistralai/Mistral-7B-Instruct-v0.3` | `[ngram]` | ❌ Missing |

Also: raise `NumSpecTokens` from 5 → 7 for Qwen2.5-7B on H100 (draft verification is fast enough).

---

### 4. Worker Heartbeat: Reuse HTTP Connection

**File:** `python/src/infera_worker/http_server.py`
**Estimated impact:** -~50ms per heartbeat; eliminates TLS handshake overhead every 5s

Currently `httpx.AsyncClient()` is instantiated per heartbeat call. Every 5 seconds this creates a new TCP connection and completes a full TLS handshake to the gateway.

**Fix:** Create a single shared client at server startup:

```python
# In InferaWorkerServer.__init__
self._gateway_client = httpx.AsyncClient(
    base_url=self.config.router_address,
    timeout=httpx.Timeout(10.0, connect=5.0),
    http2=True,  # if gateway supports HTTP/2
    limits=httpx.Limits(max_keepalive_connections=2, keepalive_expiry=60),
)

# In shutdown:
await self._gateway_client.aclose()
```

Replace all inline `async with httpx.AsyncClient() as client:` calls in heartbeat/registration with `self._gateway_client`.

---

### 5. Gateway: Worker Connection Keep-Alive Pool

**File:** `go/internal/gateway/worker_client.go`
**Estimated impact:** -5–15ms per inference request

The Go HTTP client for worker communication should use persistent connections. Add explicit transport configuration:

```go
transport := &http.Transport{
    MaxIdleConnsPerHost:   8,
    MaxConnsPerHost:       16,
    IdleConnTimeout:       90 * time.Second,
    TLSHandshakeTimeout:   5 * time.Second,
    ExpectContinueTimeout: 1 * time.Second,
    DisableCompression:    false,
}
client := &http.Client{
    Transport: transport,
    Timeout:   120 * time.Second,
}
```

Keep one client per worker (keyed by worker address) rather than one global client, so connection pools don't interfere across workers.

---

### 6. Adaptive Batching: Token-Aware Flush

**File:** `go/internal/router/batcher/batcher.go`
**Estimated impact:** Better GPU utilization; reduces OOM risk on large-context models

Current flush triggers: 50ms elapsed OR 8 requests. A request with a 4K-token prompt counts the same as one with 10 tokens — this can cause batches that exceed GPU memory.

**Improvements:**
1. Track estimated token count per batch (sum of `max_tokens` + estimated prompt tokens)
2. Flush early if batch would exceed a token budget (e.g., 16K tokens for L40S, 32K for A100)
3. Make `maxWaitMs` adaptive: when queue depth > 2× `maxBatchSize`, reduce wait to 10ms

```go
type Batcher struct {
    maxBatchSize    int
    maxWaitMs       int
    maxBatchTokens  int  // new: token budget per batch
    // ...
}
```

---

## Tier 2 — Production Resilience

### 7. Worker Graceful Shutdown: Extend Drain Timeout

**File:** `python/src/infera_worker/worker.py` line 77
**Issue:** 1-second drain timeout kills in-flight streaming responses during deploys

A streaming response for 500 tokens at 40 tokens/sec takes ~12 seconds. Any deploy or rolling restart drops those streams.

**Fix:**
```python
# config.py
drain_timeout_seconds: int = Field(default=30, description="Seconds to wait for active requests to complete on SIGTERM")

# worker.py — SIGTERM handler
async def _handle_sigterm(self):
    self._state = WorkerState.DRAINING
    deadline = time.monotonic() + self.config.drain_timeout_seconds
    while self.active_requests and time.monotonic() < deadline:
        await asyncio.sleep(0.5)
    await self.shutdown()
```

Also add `SIGTERM` → `DRAINING` state transition so the worker stops accepting new requests immediately while completing existing ones.

---

### 8. Worker Auth Failure: Softer Retry Policy

**File:** `python/src/infera_worker/http_server.py` lines 415–423
**Issue:** 3 consecutive auth failures → worker self-terminates; gateway restart wipes all workers

If the gateway restarts for a rolling deploy, all workers see auth failures simultaneously and self-terminate. Re-provisioning 8 pods takes 10+ minutes.

**Fix:** Change to exponential backoff with a maximum retry window:
```python
# Instead of exiting after 3 failures:
_auth_failure_count = 0
_auth_backoff_seconds = [5, 10, 30, 60, 120, 300, 600]  # up to 10 min

async def _handle_auth_failure(self):
    self._auth_failure_count += 1
    backoff = self._auth_backoff_seconds[
        min(self._auth_failure_count - 1, len(self._auth_backoff_seconds) - 1)
    ]
    if self._auth_failure_count > len(self._auth_backoff_seconds):
        logger.error("Persistent auth failure — shutting down")
        await self.shutdown()
    else:
        logger.warning(f"Auth failure #{self._auth_failure_count}; retrying in {backoff}s")
        await asyncio.sleep(backoff)
```

---

### 9. Circuit Breaker: Jitter + Softer Thresholds

**File:** `go/internal/gateway/` (circuit_breaker.go)
**Issue:** Fixed 10s reset timeout → synchronized probe storms when multiple workers fail together

**Recommended changes:**
- Raise failure threshold: 5 → 10
- Add ±2s jitter to reset timeout
- Increase base reset timeout: 10s → 30s
- Implement exponential backoff on repeated openings (30s → 60s → 120s)

```go
resetTimeout: 30 * time.Second + time.Duration(rand.Intn(4000))*time.Millisecond,
```

---

### 10. Rate Limiter: Redis Backend for Multi-Gateway

**File:** `go/internal/gateway/rate_limiter.go`
**Issue:** In-memory state is not shared across multiple gateway instances

The current token bucket implementation is clean and interface-based — the backing store just needs to be swapped. Use Redis `INCR` + sliding window for distributed rate limiting:

```go
type RateLimiter interface {
    Allow(key string) (bool, error)
    Reset(key string) error
}

// MemoryRateLimiter — current, single-instance
// RedisRateLimiter  — add for multi-gateway
```

This unblocks horizontal gateway scaling without per-instance rate limit divergence.

---

### 11. Model Preset Catalog: Move to Config File

**File:** `go/internal/providers/runtime.go`
**Issue:** Every new model requires a Go code change + full gateway redeploy

Move `modelRuntimePresets` to a YAML/JSON file read at startup. The Go struct definitions stay; only the data moves.

**Proposed schema (`config/model_presets.yaml`):**
```yaml
models:
  - id: "Qwen/Qwen2.5-7B-Instruct"
    max_model_len: 32768
    gpu_memory_utilization: "0.94"
    spec_decoding:
      draft_model: "Qwen/Qwen2.5-0.5B-Instruct"
      num_spec_tokens: 5
      min_vram_gb: 40
  - id: "Qwen/Qwen3-4B-Thinking-2507"
    max_model_len: 65536
    gpu_memory_utilization: "0.94"
    spec_decoding:
      mode: ngram
      num_spec_tokens: 4
      ngram_lookup: 4
      min_vram_gb: 40
```

The gateway hot-reloads this file on `SIGHUP`, enabling model catalog updates without downtime.

---

## Tier 3 — Observability & Debugging

### 12. Distributed Tracing: W3C Trace Context

**Files:** `go/internal/gateway/gateway.go` + `python/src/infera_worker/http_server.py`

Currently only `X-Request-ID` is forwarded. This prevents correlating gateway logs with worker logs for the same request, and makes latency breakdown (queue time vs. TTFT vs. decode time) impossible.

**Add to gateway outbound requests:**
```go
// Propagate W3C traceparent header to worker
req.Header.Set("traceparent", fmt.Sprintf("00-%s-%s-01", traceID, spanID))
```

**Add to worker incoming requests:**
```python
traceparent = request.headers.get("traceparent", "")
# include in structured log output for all request events
```

This enables Jaeger/Tempo integration later with zero changes to the tracing logic.

---

### 13. TPOT Variance Metric per Model

**File:** `go/internal/gateway/gateway.go` (streaming path) + `go/internal/gateway/metrics.go`

The current TPOT histogram records per-token timing but doesn't expose **variance** — which is the key signal for speculative decoding health. High variance = many spec token rejections (all 5 speculated tokens rejected → one slow step). Add:

```go
// metrics.go
tpotVariance = prometheus.NewGaugeVec(prometheus.GaugeOpts{
    Name: "infera_inference_tpot_variance_seconds",
    Help: "Running variance of TPOT per model — high values indicate spec decoding misses",
}, []string{"model"})
```

Calculate using Welford's online algorithm during the streaming loop.

---

### 14. Worker Queue Depth Metric

**File:** `python/src/infera_worker/http_server.py`

The worker reports `active_requests` but not queue depth (requests waiting for semaphore). This makes it impossible to distinguish "GPU is saturated" from "worker is misconfigured":

```python
# Add to Prometheus metrics
infera_worker_queue_depth = Gauge(
    "infera_worker_queue_depth",
    "Requests waiting for a concurrency slot",
    ["worker_id"],
)
```

Expose this in the `/metrics` endpoint and in heartbeat payloads so the gateway router can avoid sending to workers with deep queues.

---

## Tier 4 — Frontend Performance

### 15. Reduce Stats Polling: 2s → 10s

**File:** `frontend/src/hooks/useApi.ts` line 34

`useStats` polls every 2 seconds. At 5 open browser tabs, this is 2.5 gateway requests/second per user — for data that changes at most once per second in practice.

```ts
// useStats
refetchInterval: 10_000,         // was 2_000
refetchIntervalInBackground: false,  // pause when tab is hidden
```

Apply `refetchIntervalInBackground: false` to all polling queries. A hidden tab has no user to show the data to.

---

### 16. Lazy-Load All Authenticated Pages

**File:** `frontend/src/App.tsx`

Only Login, Docs, and AcceptInvitation are currently lazy. Dashboard, Models, Instances, Logs, ApiKeys, and WorkspaceAdmin are all eagerly bundled — that's why the main JS chunk is 825KB.

```ts
const Dashboard     = lazyWithRetry(() => import('./pages/Dashboard'),      'dashboard');
const Models        = lazyWithRetry(() => import('./pages/Models'),         'models');
const Instances     = lazyWithRetry(() => import('./pages/Instances'),      'instances');
const Logs          = lazyWithRetry(() => import('./pages/Logs'),           'logs');
const ApiKeys       = lazyWithRetry(() => import('./pages/ApiKeys'),        'api-keys');
const WorkspaceAdmin = lazyWithRetry(() => import('./pages/WorkspaceAdmin'), 'workspace');
```

Wrap the authenticated routes section in the existing `<Suspense fallback={<RouteLoader />}>` — it's already there for the outer unauthenticated routes.

Estimated reduction: 825KB → ~200KB initial chunk (recharts lives in Dashboard/Models, only loaded on navigation).

---

### 17. React Query: Tune Global Defaults

**File:** `frontend/src/App.tsx` QueryClient config

```ts
const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: 1,
      staleTime: 30_000,              // was 2_000 — serve cached data freely
      gcTime: 5 * 60 * 1000,         // keep unused cache 5 min
      refetchOnWindowFocus: false,    // don't hammer on tab switch
      refetchOnReconnect: true,       // do refresh after offline recovery
      refetchIntervalInBackground: false,
    },
  },
});
```

Critical: `refetchOnWindowFocus: false` is important because the current setting causes every tab switch to fire parallel requests for workers + stats + instances + costs + models simultaneously.

---

## Quick Reference: Bottleneck → Fix Mapping

| Bottleneck | File | Fix | Estimated Impact |
|---|---|---|---|
| No prefix/KV cache reuse | `vllm_engine.py` | `enable_prefix_caching=True` | +20–40% tokens/sec (chat workloads) |
| No chunked prefill | `vllm_engine.py` | `enable_chunked_prefill=True` | +10–20% throughput on long prompts |
| Single-step scheduling | `vllm_engine.py` + `config.py` | `num_scheduler_steps` per GPU tier | +10–25% GPU utilization on A100/H100 |
| Heartbeat TCP overhead | `http_server.py` | Shared `httpx.AsyncClient` | -~50ms per heartbeat |
| Worker kills itself on gateway restart | `http_server.py` | Exponential backoff retry (10 min window) | Eliminates cold-start storms on deploy |
| Token-unaware batching | `batcher.go` | Token-weighted batch flush | Better GPU utilization; prevents OOM |
| New TCP per worker request | `worker_client.go` | Keep-alive connection pool per worker | -5–15ms per inference call |
| 1s drain timeout | `worker.py` | 30s configurable drain with SIGTERM | No mid-stream kills on rolling deploy |
| Circuit breaker probe storm | `circuit_breaker.go` | Jitter + exponential backoff on reset | Prevents synchronized probe thundering herd |
| In-memory rate limiter | `rate_limiter.go` | Redis sliding window backend | Enables horizontal gateway scaling |
| Hard-coded model presets | `runtime.go` | YAML catalog + hot-reload on SIGHUP | Zero-deploy model onboarding |
| Spec decoding gaps | `runtime.go` | Add presets for Qwen2.5-14B/32B, Llama-3.1 | +15–30% tokens/sec on eligible models |
| No request tracing | `gateway.go` + `http_server.py` | W3C `traceparent` propagation | Full end-to-end latency breakdown |
| 2s stats poll | `useApi.ts` | 10s + `refetchIntervalInBackground: false` | -80% gateway stats request load |
| 825KB initial JS bundle | `App.tsx` | Lazy-load all authenticated pages | ~200KB initial load |
| `refetchOnWindowFocus: true` | `App.tsx` QueryClient | Disable globally | Eliminates parallel burst on tab switch |

---

## Missing Production-Readiness Features

| Feature | Status | Blocking For |
|---|---|---|
| Connection pooling (worker→gateway heartbeat) | Missing | Latency at scale |
| Distributed rate limiting (Redis) | Missing | Multi-gateway horizontal scale |
| Adaptive batching (token-aware) | Missing | Stable throughput on large-context models |
| Request cancellation (abort mid-stream) | Stub only | Client UX; hung request cleanup |
| Graceful drain on SIGTERM | Partial (1s) | Zero-downtime deploys |
| W3C distributed tracing | Missing | Root-cause latency analysis |
| Dynamic model catalog | Missing | Zero-deploy model onboarding |
| Worker rebalancing / model migration | Missing | Load efficiency across workers |
| Multi-gateway state replication | Missing | High availability / active-active |
| Response caching (ETag / Cache-Control) | Missing | Bandwidth + latency for repeated queries |
| Adaptive polling (client-side) | Missing | Gateway load from browser clients |
| vLLM prefix caching | Missing | Tokens/sec on chat workloads |
| vLLM chunked prefill | Missing | Throughput under mixed load |
| Per-model spec decoding coverage | Partial (3 models) | Full tokens/sec gains across catalog |

---

## Key File Index

| File | Lines | Role |
|---|---|---|
| `python/src/infera_worker/http_server.py` | ~779 | Heartbeat, registration, HTTP endpoints |
| `python/src/infera_worker/worker.py` | ~368 | Concurrency model, state machine, stats |
| `python/src/infera_worker/engines/vllm_engine.py` | ~356 | vLLM AsyncEngineArgs, inference, streaming |
| `python/src/infera_worker/config.py` | ~125 | All worker env vars |
| `go/internal/gateway/gateway.go` | ~600 | Request routing, auth, backpressure |
| `go/internal/gateway/worker_client.go` | ~250 | Worker HTTP client, circuit breaker |
| `go/internal/gateway/rate_limiter.go` | ~120 | Per-key token bucket |
| `go/internal/gateway/metrics.go` | ~150 | Prometheus histogram definitions |
| `go/internal/router/router.go` | ~250 | Routing strategy dispatch, retries |
| `go/internal/router/batcher/batcher.go` | ~150 | Batch queue, flush timer |
| `go/internal/router/registry/registry.go` | ~200 | Worker registry, health thresholds |
| `go/internal/providers/runtime.go` | ~215 | Model runtime presets, spec decoding config |
| `go/internal/providers/runpod/runpod.go` | ~250 | RunPod GraphQL provisioning |
| `frontend/src/hooks/useApi.ts` | ~229 | All polling intervals, query keys |
| `frontend/src/App.tsx` | ~400 | QueryClient config, lazy loading, nav |
| `deploy/observability/prometheus/prometheus.yml` | — | Scrape config, SD intervals |
