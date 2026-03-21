# Performance Optimization Plan

> Tracking file for performance work across the Infera stack.
> Branch: `backend/affinity-routing-and-prefix-cache` | Last updated: 2026-03-21

---

## Current Validated State

- [x] vLLM prefix caching, chunked prefill, `max_model_len`, `max_num_batched_tokens`, speculative decoding, and scheduler-steps plumbing are wired in the worker.
- [x] RunPod provisioning already mounts `/workspace` as a persistent volume and points Hugging Face / Torch caches there.
- [x] Matching stopped instances are already reused via provider `start` instead of always reprovisioning from scratch.
- [x] Gateway worker HTTP clients already use keep-alive pooling.
- [x] Router batching exists, but gateway-to-worker dispatch is still one request per worker call; current batching mainly adds queue/wait behavior before worker selection.
- [x] Worker startup still blocks on model preload before the HTTP server is started, so readiness is coupled to full model load.

## Planning Corrections

- [x] Use benchmark-first execution. Do not change runtime defaults before capturing concurrent warm and cold baselines.
- [x] Prefer per-model / per-GPU runtime presets over global worker defaults for vLLM tuning.
- [x] Treat warm reuse (`stop/start`) as the current cold-start optimization path before adding a true warm-pool feature.
- [x] Keep non-performance product work out of this file. BYOC frontend and workspace billing rollups belong in product roadmap docs, not the performance execution queue.

---

## 1. Inference Throughput

- [x] **T1-01: Add missing vLLM tuning knobs to worker config and provider runtime env**
  - **What**: Expose `max_num_seqs`, `swap_space`, `enforce_eager`, and runtime-controlled tensor parallelism in the worker and provider preset layer.
  - **Why**: High impact. The current code only exposes part of the vLLM tuning surface, which blocks per-GPU throughput tuning and makes A40/A100/L40S comparisons noisy.
  - **How**: Add new fields to `python/src/infera_worker/config.py`; pass them through `python/src/infera_worker/engines/vllm_engine.py`; add matching env options and preset plumbing in `go/internal/providers/runtime.go`; ensure RunPod and Vast.ai propagate the new env vars.
  - **Measure**: Verify env-to-engine wiring in unit tests, then benchmark `decode tok/s`, TTFT, and GPU memory usage on L40S and A100 before/after.
  - **Status**: `[x]` done

- [x] **T1-02: Move scheduler-step and batching-token tuning into per-model / per-GPU runtime presets**
  - **What**: Tune `num_scheduler_steps`, `max_num_batched_tokens`, and `gpu_memory_utilization` through `runtime.go` presets instead of changing the global worker defaults.
  - **Why**: High impact. Global defaults are too blunt; some GPUs benefit from more aggressive decode amortization while others pay TTFT regression for little gain.
  - **How**: Keep `python/src/infera_worker/config.py` defaults conservative; extend `go/internal/providers/runtime.go` presets for hot models and GPU tiers; document before/after values in this file as changes land.
  - **Measure**: For each preset, record TTFT p50/p95, decode tok/s p50/p95, and peak memory on warm runs.
  - **Status**: `[x]` done

- [x] **T1-03: Add `max_num_seqs` and KV-cache tuning by GPU tier**
  - **What**: Tune sequence concurrency and KV-cache pressure for RunPod GPU SKUs we actually use.
  - **Why**: High impact on throughput stability. vLLM defaults can overcommit KV cache on single-GPU pods and cause throughput collapse or memory pressure.
  - **How**: Add `vllm_max_num_seqs`; define starting values by model family and GPU tier in `go/internal/providers/runtime.go`; document A40/L40S/A100 recommended ranges and when to raise or lower them.
  - **Measure**: Track tokens/sec, OOM rate, memory used/total, and latency tail under 2x, 4x, and 8x concurrency.
  - **Status**: `[x]` done

### Current Runtime Preset Snapshot

| Model / GPU tier | `max_num_batched_tokens` before | `max_num_batched_tokens` after | `num_scheduler_steps` before | `num_scheduler_steps` after | `max_num_seqs` before | `max_num_seqs` after |
|---|---:|---:|---:|---:|---:|---:|
| Qwen2.5-7B on L40S | 2048 | 2048 | unset | unset | unset | 16 |
| Qwen2.5-7B on A100 40GB | 2048 | 4096 | unset | 4 | unset | 32 |
| Qwen2.5-7B on A100 80GB | 2048 | 8192 | unset | 6 | unset | 48 |
| Qwen2.5-7B on H100 | 2048 | 8192 | unset | 8 | unset | 64 |
| Qwen3-4B on L40S | 2048 | 2048 | unset | unset | unset | 16 |
| Qwen3-4B on A100 40GB | 2048 | 4096 | unset | 4 | unset | 32 |
| Qwen3-4B on A100 80GB | 2048 | 8192 | unset | 6 | unset | 48 |
| Qwen3-4B on H100 | 2048 | 8192 | unset | 8 | unset | 64 |
| Kimi-K2.5 on 8xH100 | 4096 | 4096 | 8 | 8 | unset | 16 |

Range notes:

- Unknown OSS models now inherit the GPU-tier fallback preset instead of receiving no runtime tuning at all.
- Multi-GPU `A100` and `H100` requests now default `tensor_parallel_size` to the requested GPU count; single-GPU and smaller SKUs remain unchanged.
- Treat `A40` like the `L40S` tier for initial `max_num_seqs` experiments until it is added as a first-class GPU type.
- Raise `max_num_seqs` only when warm-run memory headroom is stable and TTFT does not regress materially at 4x concurrency.
- Lower `max_num_seqs` first if OOMs, swap thrash, or P95/P99 latency spikes appear before decode throughput improves.

- [ ] **T1-04: Benchmark quantized variants for hot models**
  - **What**: Create a benchmark matrix for FP16/BF16 versus AWQ/GPTQ/INT4/INT8 variants of the top deployed models.
  - **Why**: Medium to high impact on cost and throughput. Quantization may unlock smaller GPUs or higher concurrency, but only if TTFT and output quality remain acceptable.
  - **How**: Add candidate quantized model IDs to docs and runtime presets where appropriate; benchmark via `scripts/benchmark-chat.py`; only promote variants that pass latency and quality smoke checks.
  - **Measure**: Compare TTFT, decode tok/s, memory footprint, and cost/query for each quantized candidate against the base model.
  - **Status**: `[ ]` not started

- [x] **T1-05: Enable tensor-parallel presets for multi-GPU pods**
  - **What**: Use `tensor_parallel_size > 1` when provisioning multi-GPU workers where the model and GPU count justify it.
  - **Why**: Medium impact. Multi-GPU pods are currently under-optimized because tensor parallelism is configurable in the worker but not preset-driven in provisioning.
  - **How**: Add provider preset support for tensor parallel size, starting with multi-GPU A100/H100 requests in `go/internal/providers/runtime.go`; keep single-GPU presets unchanged.
  - **Measure**: Validate successful model load, compare warm TTFT and decode tok/s between TP=1 and TP=N on the same pod size.
  - **Status**: `[x]` done

- [ ] **T1-06: Expand speculative decoding coverage with tested model pairs**
  - **What**: Add tested draft-model and ngram-mode recommendations for the models we actively serve.
  - **Why**: Medium impact. The config path already exists, but coverage and guidance are too narrow to use confidently.
  - **How**: Extend `go/internal/providers/runtime.go` presets for safe large-GPU cases; add a short table of validated pairings and `num_speculative_tokens` guidance in docs.
  - **Measure**: Compare TTFT, decode tok/s, and acceptance/rejection behavior across warm benchmark runs.
  - **Status**: `[ ]` not started

---

## 2. Cold Start Reduction

- [ ] **C2-01: Measure and optimize the existing stop/start reuse path before building warm pools**
  - **What**: Treat matching stopped-instance reuse as the primary warm-path optimization and add benchmark coverage for it.
  - **Why**: High impact. The capability already exists, so improving reuse hit rate and restart latency is cheaper than building a new warm-pool control plane first.
  - **How**: Benchmark `provision`, `stop/start`, and `reused stopped instance` paths; add visibility around reuse in provider-manager logs and docs; verify matching logic in `go/internal/providers/manager.go`.
  - **Measure**: Record cold start to ready, restart to ready, and cache-hit behavior for reused instances.
  - **Status**: `[ ]` not started

- [ ] **C2-02: Split liveness and readiness from full model preload**
  - **What**: Start the worker HTTP surface earlier and distinguish process-up from model-ready.
  - **Why**: High impact on perceived cold-start time and operability. Today the server only starts after preload completes, so the platform cannot observe intermediate startup stages.
  - **How**: Change startup order in `python/src/infera_worker/cli.py` and `python/src/infera_worker/worker.py` so the server can report state during model load; add readiness-specific fields to `/health` and heartbeat payloads in `python/src/infera_worker/http_server.py`; keep inference endpoints rejecting requests until ready.
  - **Measure**: Compare provision-to-first-health, provision-to-ready, and gateway registration timing before/after.
  - **Status**: `[ ]` not started

- [ ] **C2-03: Add persistent-cache strategy for Vast.ai**
  - **What**: Avoid full model redownloads on Vast.ai restart paths.
  - **Why**: High impact where Vast.ai is used. RunPod already has persistent volume wiring; Vast.ai still needs an equivalent cache story.
  - **How**: Investigate Vast.ai disk/workspace options in `go/internal/providers/vastai/vastai.go`; if persistence is unavailable, evaluate a pre-baked image path for the top one or two models instead of generic weight baking.
  - **Measure**: Compare fresh provision-to-ready versus restart-to-ready on Vast.ai with the chosen cache strategy.
  - **Status**: `[ ]` not started

- [ ] **C2-04: Evaluate lazy-load versus eager-preload policy by workload type**
  - **What**: Decide when workers should preload models versus start fast and load on first request.
  - **Why**: Medium impact. Interactive single-model deployments and long-lived hot models have different optimal startup behavior.
  - **How**: Add a documented policy and, if needed, a config switch controlling eager preload; keep default behavior conservative until baseline data is captured.
  - **Measure**: Compare cold-start time, first-request latency, and operational simplicity for both modes.
  - **Status**: `[ ]` not started

- [ ] **C2-05: Warm pool for top models after reuse-path tuning is validated**
  - **What**: Keep a small number of idle ready workers alive only for the hottest models.
  - **Why**: High impact but high complexity. This should be a second-stage optimization after stop/start reuse and readiness split are working well.
  - **How**: Add deployment-level warm capacity fields in `go/internal/deployments/store.go` and manager logic in `go/internal/providers/manager.go`; define a strict policy for eligible models and idle timeout.
  - **Measure**: Compare cold-start rate, median interactive TTFT, and idle GPU cost before/after.
  - **Status**: `[ ]` not started

---

## 3. Gateway & Routing Efficiency

- [ ] **G3-01: Add a fast path that avoids batch wait for lone low-contention requests**
  - **What**: Skip the full `MaxBatchWaitMS` penalty when a request arrives to an empty per-model queue and a healthy worker is available.
  - **Why**: High impact on P50 latency. The current batch manager adds wait time even when there is nothing useful to batch.
  - **How**: Update `go/internal/router/batcher/batcher.go` and `go/internal/router/router.go` so single-request, non-streaming traffic can route immediately under low contention; keep batching active once queue depth grows.
  - **Measure**: Compare single-user P50/P95 latency and batch-size distribution before/after.
  - **Status**: `[ ]` not started

- [ ] **G3-02: Make gateway audit persistence asynchronous**
  - **What**: Remove synchronous SQLite audit writes from the inference hot path.
  - **Why**: High impact, low effort. `AppendInference` is currently called synchronously in the request defer path.
  - **How**: Introduce a buffered audit channel and background flush worker in `go/internal/gateway/gateway.go`; ensure graceful drain on shutdown; keep store semantics unchanged in `go/internal/audit/store.go`.
  - **Measure**: Compare gateway-added latency and request throughput before/after under concurrent non-streaming load.
  - **Status**: `[ ]` not started

- [ ] **G3-03: Reuse a shared httpx client for registration, heartbeats, and deregistration**
  - **What**: Stop creating a new `httpx.AsyncClient()` for each worker-to-gateway call.
  - **Why**: Medium impact. This reduces connection churn and removes avoidable overhead from the worker control path.
  - **How**: Create one long-lived client in `python/src/infera_worker/http_server.py`, reuse it for register/heartbeat/deregister, and close it in `stop()`.
  - **Measure**: Confirm reduced connection churn in logs/metrics and no regression in registration or heartbeat behavior.
  - **Status**: `[ ]` not started

- [ ] **G3-04: Benchmark routing strategy choices under realistic multi-turn load**
  - **What**: Compare least-loaded, latency-based, and affinity-heavy routing using the same concurrent conversation workload.
  - **Why**: Medium impact. Strategy decisions should be benchmarked against prefix-cache reuse and queue contention, not assumed.
  - **How**: Extend the benchmark harness to emit traffic that exercises follow-up turns; compare router behavior using current affinity metadata paths in gateway/router code.
  - **Measure**: Record TTFT p95/p99, affinity hit rate, queue depth, and decode tok/s by routing strategy.
  - **Status**: `[ ]` not started

- [ ] **G3-05: Add stronger queue and backpressure visibility**
  - **What**: Surface queue time, queue depth, and rejection reasons clearly enough to tune backpressure instead of guessing.
  - **Why**: Medium impact. Tail latency spikes are hard to interpret without seeing where requests are waiting.
  - **How**: Add metrics and logs around router wait time, rejected requests, and worker overload paths in `go/internal/gateway/metrics.go`, `go/internal/router/router.go`, and registry/strategy components.
  - **Measure**: Confirm dashboards can separate queueing delay from worker inference time under load.
  - **Status**: `[ ]` not started

---

## 4. Infrastructure & Reliability

- [ ] **I4-01: Add worker instability counters and alertable health transitions**
  - **What**: Emit Prometheus counters for unhealthy and removed workers.
  - **Why**: Medium impact. Worker churn directly affects latency and capacity but is currently hard to alert on.
  - **How**: Add counters in `go/internal/gateway/metrics.go` and increment them from `go/internal/router/registry/registry.go`.
  - **Measure**: Verify metrics increment during registry health-transition tests and appear in Prometheus scrape output.
  - **Status**: `[ ]` not started

- [ ] **I4-02: Separate liveness, readiness, and draining semantics across gateway and worker**
  - **What**: Make health endpoints and shutdown behavior clearly represent whether a process is alive, ready, or draining.
  - **Why**: Medium impact. This reduces bad routing decisions during rollout and makes readiness-based automation possible.
  - **How**: Extend `python/src/infera_worker/http_server.py` health output, keep worker state transitions explicit, and ensure gateway respects draining/unhealthy workers consistently.
  - **Measure**: Verify requests are not routed to draining workers and shutdown completes without dropping in-flight requests unnecessarily.
  - **Status**: `[ ]` not started

- [ ] **I4-03: Add persistent worker-registry backing only if in-memory registry becomes an operational limit**
  - **What**: Evaluate SQLite or Redis for worker registry persistence.
  - **Why**: Medium impact but not urgent. It is reliability work, not a first-order throughput optimization.
  - **How**: First document failure cases caused by the in-memory registry; only then design persistence in `go/internal/router/registry` and gateway bootstrap paths.
  - **Measure**: Use restart/failure drills to show whether registry persistence removes a real operational gap.
  - **Status**: `[ ]` not started

- [ ] **I4-04: Define TLS policy for gateway and worker transport**
  - **What**: Ensure all external and worker-facing traffic has an explicit transport-security story.
  - **Why**: Medium impact on production safety. This is reliability/security work that should be tracked, but it is not a top performance lever.
  - **How**: Document current HTTPS/TLS behavior, decide which channels require TLS termination versus direct HTTPS, and update provider/runtime docs accordingly.
  - **Measure**: Confirm production traffic paths are encrypted and worker registration still succeeds through provider-specific addressing.
  - **Status**: `[ ]` not started

- [ ] **I4-05: Improve structured request tracing**
  - **What**: Make request, worker, model, and routing identifiers consistently visible across logs and metrics.
  - **Why**: Medium impact. Accurate tracing is required to explain latency regressions after tuning.
  - **How**: Extend structured logging in gateway and worker paths, building on existing `traceparent` propagation and request/audit logging.
  - **Measure**: Confirm a single request can be followed end-to-end across gateway, router decision, worker execution, and audit output.
  - **Status**: `[ ]` not started

---

## 5. Benchmarking Harness

- [x] **B5-01: Add concurrent load mode to the benchmark script**
  - **What**: Support multi-client concurrent load instead of only sequential runs.
  - **Why**: High impact. Sequential runs hide the queueing and prefill/decode contention that matter in production.
  - **How**: Extend `scripts/benchmark-chat.py` with `--concurrency N` and asynchronous request execution for streaming and non-streaming runs.
  - **Measure**: Report TTFT p50/p95/p99, aggregate throughput, and per-request latency under 2x, 4x, and 8x concurrency.
  - **Status**: `[x]` done

- [x] **B5-02: Add warmup and cache-reuse modes**
  - **What**: Support warmup runs and repeated-prefix scenarios in the benchmark harness.
  - **Why**: High impact on measurement quality. Prefix caching and warm KV/cache behavior cannot be reasoned about from one cold request at a time.
  - **How**: Add `--warmup N` and a cache-reuse mode to `scripts/benchmark-chat.py`; update `docs/BENCHMARK_BASELINE_TEMPLATE.md` to capture warm versus cold conditions explicitly.
  - **Measure**: Record TTFT deltas between first-run and warmed or repeated-prefix runs.
  - **Status**: `[x]` done

- [x] **B5-03: Add cold-start benchmark workflow**
  - **What**: Standardize how cold-start time is measured for fresh provision, stop/start reuse, and reused stopped-instance flows.
  - **Why**: High impact. Cold-start improvement work is not actionable without a repeatable measurement path.
  - **How**: Expand `docs/BENCHMARK_BASELINE_TEMPLATE.md` and, if helpful, add a helper script that timestamps provision request, first health, registration, and first successful inference.
  - **Measure**: Record provision-to-health, provision-to-ready, and provision-to-first-successful-completion for each provider path.
  - **Status**: `[x]` done

- [ ] **B5-04: Establish a committed benchmark baseline before any runtime-default changes**
  - **What**: Capture a baseline for the current branch using the current production-like image, model, GPU, and routing strategy.
  - **Why**: High impact. Runtime tuning without a committed baseline makes regressions impossible to prove.
  - **How**: Run the benchmark harness against one standard model/GPU matrix and store the summarized results in this file or an adjacent benchmark doc such as `docs/BENCHMARK_BASELINE_CURRENT.md`.
  - **Measure**: Baseline must include TTFT p50/p95/p99, decode tok/s p50/p95, cold-start timing, and cost/query.
  - **Status**: `[ ]` not started

- [ ] **B5-05: Add CI or scheduled regression checks only after the benchmark harness is stable**
  - **What**: Automate regression detection for key latency and throughput metrics.
  - **Why**: Medium impact. CI gating is valuable, but only after the measurement method is trustworthy.
  - **How**: Add a workflow to compare benchmark JSON against a stored baseline once the benchmark environment is deterministic enough.
  - **Measure**: Fail the check on agreed regression thresholds for TTFT and decode tok/s.
  - **Status**: `[ ]` not started

---

## Execution Order

| Priority | Item | Why it goes first |
|----------|------|-------------------|
| 1 | **B5-01** | Need concurrent measurement before tuning anything else |
| 2 | **B5-02** | Warmup/cache-reuse data is required to interpret prefix-caching and affinity wins |
| 3 | **B5-03** | Cold-start work needs a repeatable benchmark path |
| 4 | **B5-04** | Commit a real baseline before changing runtime defaults |
| 5 | **G3-01** | Removes avoidable P50 latency from the current request path |
| 6 | **G3-02** | Cheap hot-path latency win in gateway |
| 7 | **G3-03** | Cheap control-plane cleanup in worker |
| 8 | **C2-01** | Existing stop/start reuse should be optimized before new warm-pool machinery |
| 9 | **C2-02** | Startup-path split is the real prerequisite for better cold-start visibility |
| 10 | **T1-01** | Unlocks the missing runtime-tuning knobs |
| 11 | **T1-02** | Apply scheduler/batching tuning through presets, not global defaults |
| 12 | **T1-03** | Tune KV-cache / sequence concurrency by GPU tier |
| 13 | **G3-04** | Choose routing strategy based on data, not intuition |
| 14 | **T1-05** | Multi-GPU throughput optimization after measurement and wiring exist |
| 15 | **T1-04** | Quantization comparisons after baseline harness is stable |
| 16 | **T1-06** | Expand speculative decoding coverage with measured pairings |
| 17 | **C2-03** | Provider-specific cache work after the common path is improved |
| 18 | **I4-01** | Improves alerting once behavior starts changing |
| 19 | **I4-02** | Tightens rollout safety and drain semantics |
| 20 | **C2-04** | Lazy versus eager policy after readiness split and benchmarks exist |
| 21 | **I4-05** | Tracing polish to aid later tuning/debugging |
| 22 | **B5-05** | Automate only after benchmarks are trustworthy |
| 23 | **I4-03** | Registry persistence only if proven necessary |
| 24 | **I4-04** | Important production hardening, but not a first-order perf lever |
| 25 | **C2-05** | Warm-pool feature comes last because it is expensive and operationally complex |

---

## Explicitly Deferred or Moved Out

- [x] BYOC frontend for workspace provider credentials moved out of this plan.
- [x] Workspace billing rollups moved out of this plan.
- [x] Request coalescing for identical prompts deferred; correctness risk is high and current value is lower than the items above.
- [x] Global worker-default change for `vllm_num_scheduler_steps` replaced by per-model / per-GPU preset tuning.

## Notes for Each Change Set

- Record before/after values for every vLLM config change in the commit or adjacent benchmark note.
- Prefer logical commits by optimization slice: benchmarking, gateway hot-path, cold-start path, then runtime-tuning presets.
- Do not mark an item done until a benchmark or test artifact exists showing the expected effect.
