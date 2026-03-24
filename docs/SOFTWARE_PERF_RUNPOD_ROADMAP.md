# Infera Software Performance + RunPod Roadmap

> Last updated: 2026-03-17
> Status: Draft execution roadmap
> Branch: user to create and switch locally before implementation continues

## Goal

Max out the current software path before changing hardware.

This roadmap focuses on:

- reducing cold-start time and cost for interactive usage
- increasing effective throughput and tokens/sec on the current stack
- improving routing and cache reuse
- fixing RunPod inventory correctness and pricing fidelity
- expanding the model catalog with newer Qwen/Kimi options

## Why This Order

The current repo is leaving meaningful performance on the table due to software and product-path issues:

- interactive queries currently depend on provisioning a dedicated pod
- RunPod pods re-download model weights on fresh provisioning
- the batcher exists but is not wired end-to-end
- routing is not cache-affinity aware
- worker load metrics are not strong enough for good scheduling
- RunPod offerings can fall back to a static list with stale prices and unusable GPUs

Changing accelerators before fixing these will hide the real bottlenecks and make benchmarking noisy.

## Success Metrics

Track these before and after each phase.

- `TTFT`: time to first token
- `decode tok/s`: completion tokens per second after first token
- `prefill tok/s`: prompt processing throughput
- `tok/s/$`: throughput normalized by hourly infra cost
- `cost/query`: especially for short interactive prompts
- `cold start to ready`: provision click to model-ready
- `model cache hit rate`: local or persistent cache reuse
- `worker utilization`: GPU utilization, queue depth, active requests
- `routing stability`: fraction of related turns routed to the same warm cache

## Current Observations

Relevant current code paths:

- worker preloads models before the HTTP server starts:
  [`python/src/infera_worker/cli.py`](/Users/siddharthsingh/codingtensor/infera/python/src/infera_worker/cli.py)
  [`python/src/infera_worker/worker.py`](/Users/siddharthsingh/codingtensor/infera/python/src/infera_worker/worker.py)
- RunPod provisioning uses container disk and no persistent volume:
  [`go/internal/providers/runpod/runpod.go`](/Users/siddharthsingh/codingtensor/infera/go/internal/providers/runpod/runpod.go)
- batcher exists but request dispatch is still per-request:
  [`go/internal/router/batcher/batcher.go`](/Users/siddharthsingh/codingtensor/infera/go/internal/router/batcher/batcher.go)
  [`go/internal/router/router.go`](/Users/siddharthsingh/codingtensor/infera/go/internal/router/router.go)
  [`go/internal/gateway/gateway.go`](/Users/siddharthsingh/codingtensor/infera/go/internal/gateway/gateway.go)
- model seed list is static and currently includes Qwen2.5 but not newer Qwen/Kimi options:
  [`go/internal/vault/seed.go`](/Users/siddharthsingh/codingtensor/infera/go/internal/vault/seed.go)

## Phases

## Phase 0: Baseline and Measurement

Purpose: establish a clean baseline before behavior changes.

Tasks:

- add a benchmark checklist for one short prompt, one medium prompt, and one long-context prompt
- capture current `TTFT`, `decode tok/s`, `cold start to ready`, and `cost/query`
- verify which metrics already exist and which are missing
- add a simple decision log for benchmark runs

Definition of done:

- one repeatable benchmark script or documented benchmark process exists
- one baseline measurement table is committed to the roadmap or adjacent benchmark doc

## Phase 1: Cold-Start and Cost Optimization

Purpose: make interactive usage viable without waiting for full reprovision + full model download each time.

Tasks:

- move model cache paths to persistent storage on RunPod
  - prefer `/workspace` for HF and model cache paths
  - evaluate network volume only if `/workspace` is insufficient
- stop using full reprovision as the default warm-path for repeat use
  - prefer `stop` / `start` over `terminate` / `provision` for reusable pods
- pin the worker image instead of relying on `latest`
- shorten the worker boot critical path
  - review whether HTTP server and readiness reporting can start before full preload completes
  - consider lazy model load for selected usage paths
- define a warm-pool policy for the top 1-3 models
  - minimum one warm worker per hot model during active hours
  - stop idle warm workers after a timeout instead of terminating immediately

Definition of done:

- model redownload is avoided on warm restart
- repeat interactive sessions no longer require full reprovision
- provision-to-ready time is materially lower for cached restarts

## Phase 2: Throughput and Tokens/Sec

Purpose: increase utilization and effective output rate on the current GPUs.

Tasks:

- wire the batcher end-to-end so routed batched requests are actually dispatched as batches
- audit request path to ensure batching does not silently fall back to per-request worker calls
- fix worker load telemetry
  - track real `active_requests`
  - track real `queue_depth`
  - normalize GPU utilization semantics used by router scoring
- tune vLLM runtime defaults per model or GPU tier
  - `max_model_len`
  - `gpu_memory_utilization`
  - `tensor_parallel_size`
  - quantized variants for hot-path models
- add worker and gateway metrics for
  - `TTFT`
  - prompt tokens
  - completion tokens
  - throughput per worker

Definition of done:

- batch path is exercised in tests
- throughput metrics exist and can be compared before/after
- router decisions reflect real worker pressure instead of placeholder values

## Phase 3: Cache Reuse and Routing Efficiency

Purpose: stop paying prefill cost repeatedly for related requests.

Tasks:

- enable vLLM automatic prefix caching where appropriate
- implement session or prefix affinity routing
- keep related turns on workers with warm cache when capacity allows
- add cache-aware metrics
  - prefix cache hit rate
  - affinity hit rate
  - prefill avoided

Definition of done:

- related chat turns stay on warm workers more often
- repeated prompt prefixes show lower TTFT and lower effective compute cost

## Phase 4: RunPod Offerings Correctness

Purpose: make capacity selection trustworthy and real-time.

Tasks:

- replace static or stale RunPod offerings in the UI path with live API-derived offerings by default
- filter out unusable or unsupported GPU offerings
  - incompatible SKUs
  - broken offerings
  - offerings with missing usable price data
- improve real-time price handling
  - use current API price fields
  - mark stale/fallback prices explicitly if API data is unavailable
- optionally add a provider-level health scoring layer
  - recent provision success rate by GPU type
  - recent registration success rate by GPU type
  - recent inference verification success by GPU type
- show only GPUs that are actually working for the current product flow

Definition of done:

- RunPod offerings shown in the app are live, priced correctly, and filtered for known-bad options
- provisioning failures due to bad GPU inventory drop materially

## Phase 5: Model Catalog Expansion

Purpose: add newer, relevant models once the serving path is more reliable.

Tasks:

- add a Qwen 3.x model candidate to the default vault seed list
- evaluate and add a Kimi 2.5 candidate if licensing, availability, and serving compatibility are acceptable
- validate model metadata
  - VRAM requirement
  - context length
  - family
  - tags
  - quantization recommendations
- verify the models page and deployment flow handle the new entries cleanly

Notes:

- the repo already includes `Qwen2.5 7B Instruct`
- for `Qwen3.5` and `Kimi2.5`, use exact released model names that are currently available and compatible with the worker/runtime path when implementing

Definition of done:

- selected new Qwen/Kimi entries appear in the registry and UI
- each added model has validated metadata and a tested deployment path

## Ordered Backlog

Recommended execution order:

1. Baseline measurement and benchmark checklist
2. Persistent model cache on RunPod
3. Pinned worker image and restart policy improvements
4. Warm-pool policy for hot interactive models
5. Real worker load metrics
6. End-to-end batch dispatch
7. Throughput dashboards and benchmark rerun
8. Prefix caching and affinity routing
9. RunPod live offerings cleanup and filtering
10. Qwen/Kimi model catalog additions

## First Execution Slice

Start here first:

1. persist model cache on RunPod
2. pin worker image
3. define stop/start warm behavior
4. capture before/after cold-start metrics

Reason:

- this addresses the most painful product problem first
- it reduces both latency and cost
- it improves the user experience before deeper throughput work lands

## Branch Setup

User-owned branch command:

```bash
git switch -c soft-task/software-perf-runpod-roadmap
```

Continue implementation on that branch once created locally.
