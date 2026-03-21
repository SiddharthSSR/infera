# Infera Current Branch Benchmark Baseline

This document is the committed baseline record for the current branch once live measurements are captured.

Status: pending live benchmark execution

## Standard Matrix

Use this matrix before any worker runtime-default changes land on top of the current branch.

- Provider: `runpod`
- Primary GPU: `A100_40GB`
- Secondary GPU: `A100_80GB`
- Model: `Qwen/Qwen2.5-7B-Instruct`
- Worker image: pinned production-like image tag or digest
- Default routing behavior: `least_loaded`
- Cache-locality scenario: explicit affinity via `X-Infera-Affinity-Key`
- Benchmark script: [`scripts/benchmark-chat.py`](/Users/siddharthsingh/codingtensor/infera/scripts/benchmark-chat.py)

Why this matrix:

- `Qwen/Qwen2.5-7B-Instruct` already has a concrete runtime preset in [runtime.go](/Users/siddharthsingh/codingtensor/infera/go/internal/providers/runtime.go#L181).
- RunPod is a production-ready provider in [README.md](/Users/siddharthsingh/codingtensor/infera/README.md#L453).
- `A100_40GB` and `A100_80GB` are standard RunPod SKUs already documented in [README.md](/Users/siddharthsingh/codingtensor/infera/README.md#L465).
- The router default remains `least_loaded`, while affinity is layered on top when an affinity key is present in [router.go](/Users/siddharthsingh/codingtensor/infera/go/internal/router/router.go#L417).

## Baseline Runs

Capture these runs with the same gateway build, worker image, and model revision:

1. Warm throughput baseline, no cache reuse
2. Warm throughput baseline, affinity reuse enabled
3. Cold-start `fresh_provision`
4. Cold-start `stopped_instance_start`
5. Cold-start `stopped_instance_reuse`

## Commands

### Warm Baseline, Default Routing

```bash
python3 scripts/benchmark-chat.py \
  https://your-gateway.example.com \
  --api-key "$INFERA_SMOKE_API_KEY" \
  --model "Qwen/Qwen2.5-7B-Instruct" \
  --preset conversation \
  --runs 3 \
  --warmup 2 \
  --concurrency 4 \
  --cache-reuse-mode none \
  --cost-per-hour 0.79 \
  --json-output /tmp/infera-benchmark-a100-40-no-reuse.json
```

Use `--cost-per-hour 1.19` for `A100_80GB`.

### Warm Baseline, Affinity Reuse

```bash
python3 scripts/benchmark-chat.py \
  https://your-gateway.example.com \
  --api-key "$INFERA_SMOKE_API_KEY" \
  --model "Qwen/Qwen2.5-7B-Instruct" \
  --preset conversation \
  --runs 3 \
  --warmup 2 \
  --concurrency 4 \
  --cache-reuse-mode affinity \
  --cache-key-prefix baseline \
  --cost-per-hour 0.79 \
  --json-output /tmp/infera-benchmark-a100-40-affinity.json
```

### Cold-Start Workflow

Follow [`docs/COLD_START_BENCHMARK_WORKFLOW.md`](/Users/siddharthsingh/codingtensor/infera/docs/COLD_START_BENCHMARK_WORKFLOW.md) for:

- `fresh_provision`
- `stopped_instance_start`
- `stopped_instance_reuse`

## Results

Fill this table only with live measurements from the standard matrix above.

| Provider | GPU | Scenario | Routing mode | TTFT p50 | TTFT p95 | TTFT p99 | Decode tok/s p50 | Decode tok/s p95 | Aggregate decode tok/s p50 | Cost/query | Notes |
|---|---|---|---|---:|---:|---:|---:|---:|---:|---:|---|
| RunPod | A100 40GB | Warm, conversation | least_loaded + no reuse | pending | pending | pending | pending | pending | pending | pending |  |
| RunPod | A100 40GB | Warm, conversation | affinity reuse | pending | pending | pending | pending | pending | pending | pending |  |
| RunPod | A100 80GB | Warm, conversation | least_loaded + no reuse | pending | pending | pending | pending | pending | pending | pending | optional if inventory available |
| RunPod | A100 80GB | Warm, conversation | affinity reuse | pending | pending | pending | pending | pending | pending | pending | optional if inventory available |

## Cold-Start Results

| Provider | GPU | Scenario | Provision to running | Provision to registered | Provision to first success | Registered to first success | Notes |
|---|---|---|---:|---:|---:|---:|---|
| RunPod | A100 40GB | fresh_provision | pending | pending | pending | pending |  |
| RunPod | A100 40GB | stopped_instance_start | pending | pending | pending | pending |  |
| RunPod | A100 40GB | stopped_instance_reuse | pending | pending | pending | pending | confirm reused instance ID |

## Execution Notes

- Do not update this document with local non-GPU or mock-provider numbers.
- Keep the worker image pinned for the entire baseline capture.
- Record the git commit and worker image digest alongside the filled-in results.
- If `A100_80GB` inventory is not available, capture `A100_40GB` first and leave the 80GB rows pending.
