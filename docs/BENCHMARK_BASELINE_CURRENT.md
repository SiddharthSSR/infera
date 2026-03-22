# Infera Current Branch Benchmark Baseline

This document is the committed baseline record for the current branch once live measurements are captured.

Status: standard `RunPod A100` baseline still pending; exploratory live `L40S` quantization results captured on `2026-03-21`

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

## Exploratory Live Results

These are real live-gateway measurements, but they are not the standard baseline matrix above.

- Date: `2026-03-21`
- Gateway: `https://inferai.co.in`
- GPU target: `L40S`
- Benchmark flags: `--preset conversation --runs 3 --warmup 2 --concurrency 4 --cost-per-hour 0.99`
- Use: quantization comparison and directional tuning only
- Do not use: to close `B5-04` or replace the `RunPod A100` baseline rows above

| Family | Model | Scenario | TTFT p50 | TTFT p95 | TTFT p99 | Decode tok/s p50 | Decode tok/s p95 | Aggregate decode tok/s p50 | Cost/query | Notes |
|---|---|---|---:|---:|---:|---:|---:|---:|---:|---|
| Mistral | `mistralai/Mistral-7B-Instruct-v0.3` | no reuse | `1168.9ms` | `1215.0ms` | `1218.3ms` | `46.33` | `57.96` | `149.62` | `$0.001084` | base reference for AWQ comparison |
| Mistral | `solidrust/Mistral-7B-Instruct-v0.3-AWQ` | no reuse | `641.0ms` | `735.8ms` | `739.4ms` | `144.58` | `154.20` | `524.24` | `$0.000417` | clear win over base |
| Mistral | `mistralai/Mistral-7B-Instruct-v0.3` | affinity reuse | `1172.3ms` | `1212.5ms` | `1213.3ms` | `45.63` | `68.52` | `135.28` | `$0.001219` | affinity did not improve base profile |
| Mistral | `solidrust/Mistral-7B-Instruct-v0.3-AWQ` | affinity reuse | `663.3ms` | `1463.8ms` | `1497.1ms` | `140.39` | `149.59` | `522.96` | `$0.000423` | throughput good, but TTFT tail spike |
| Qwen | `Qwen/Qwen2.5-7B-Instruct` | no reuse | `462.6ms` | `485.6ms` | `489.0ms` | `50.27` | `52.92` | `183.11` | `$0.000637` | base reference for AWQ and GPTQ |
| Qwen | `Qwen/Qwen2.5-7B-Instruct-AWQ` | no reuse | `421.0ms` | `462.5ms` | `468.9ms` | `133.88` | `154.47` | `487.98` | `$0.000468` | strongest current Qwen quantized profile |
| Qwen | `Qwen/Qwen2.5-7B-Instruct-GPTQ-Int4` | no reuse | `1127.3ms` | `1624.0ms` | `1628.2ms` | `131.50` | `155.45` | `511.13` | `$0.000449` | throughput good, TTFT too slow for interactive |
| Qwen | `Qwen/Qwen2.5-7B-Instruct` | affinity reuse | `457.4ms` | `595.2ms` | `612.9ms` | `45.62` | `52.79` | `181.47` | `$0.000584` | affinity roughly flat versus no reuse |
| Qwen | `Qwen/Qwen2.5-7B-Instruct-AWQ` | affinity reuse | `430.8ms` | `1217.0ms` | `1217.0ms` | `135.20` | `165.33` | `539.82` | `$0.000273` | needs rerun because of P95 spike |
| Qwen | `Qwen/Qwen2.5-7B-Instruct-GPTQ-Int4` | affinity reuse | `1157.0ms` | `1639.0ms` | `1657.4ms` | `141.31` | `158.63` | `519.14` | `$0.000474` | affinity does not rescue TTFT |

## Execution Notes

- Do not update this document with local non-GPU or mock-provider numbers.
- Keep the worker image pinned for the entire baseline capture.
- Record the git commit and worker image digest alongside the filled-in results.
- If `A100_80GB` inventory is not available, capture `A100_40GB` first and leave the 80GB rows pending.
- Keep exploratory `L40S` numbers separate from the standard matrix so baseline regressions remain apples-to-apples.
