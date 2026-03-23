# Infera Current Branch Benchmark Baseline

This document is the committed baseline record for the current branch once live measurements are captured.

Status: partial `RunPod A100_80GB` warm and cold baseline captured on `2026-03-23`; standard `RunPod A100_40GB` warm rows still pending; exploratory live `L40S` quantization results captured on `2026-03-21`

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

Automated helper example:

```bash
python3 scripts/cold-start-benchmark.py \
  https://inferai.co.in \
  --api-key "$INFERA_ADMIN_KEY" \
  --provider runpod \
  --gpu-type A100_80GB \
  --provider-gpu-type-id "NVIDIA A100 80GB PCIe" \
  --gpu-count 1 \
  --model "Qwen/Qwen2.5-7B-Instruct" \
  --health-insecure \
  --json-output /tmp/cold-start-a100-80.json
```

## Results

Fill this table only with live measurements from the standard matrix above.

| Provider | GPU | Scenario | Routing mode | TTFT p50 | TTFT p95 | TTFT p99 | Decode tok/s p50 | Decode tok/s p95 | Aggregate decode tok/s p50 | Cost/query | Notes |
|---|---|---|---|---:|---:|---:|---:|---:|---:|---:|---|
| RunPod | A100 40GB | Warm, conversation | least_loaded + no reuse | pending | pending | pending | pending | pending | pending | pending |  |
| RunPod | A100 40GB | Warm, conversation | affinity reuse | pending | pending | pending | pending | pending | pending | pending |  |
| RunPod | A100 80GB | Warm, conversation | least_loaded + no reuse | `937.8ms` | `1010.8ms` | `1010.9ms` | `152.84` | `236.97` | `587.77` | `$0.000493` | `Qwen/Qwen2.5-7B-Instruct`, `runs=3`, `warmup=2`, `concurrency=4`, `cost_per_hour=1.19`; current best warm `A100_80GB` baseline |
| RunPod | A100 80GB | Warm, conversation | affinity reuse | `1055.5ms` | `1378.5ms` | `1380.2ms` | `134.32` | `320.40` | `494.35` | `$0.000512` | same workload as above; affinity was worse on TTFT, median decode, aggregate decode, and cost/query in this sample |

## Cold-Start Results

| Provider | GPU | Scenario | Request to running | Request to server started | Server to model ready | Request to registered | Request to first success | Registered to first success | Notes |
|---|---|---|---:|---:|---:|---:|---:|---:|---|
| RunPod | A100 80GB PCIe | fresh_provision | `5,005 ms` | `155,248 ms` | `176,795 ms` | `333,731 ms` | `335,547 ms` | `1,816 ms` | automated helper run; `instance_id=4jal1lkguq9mut`; worker `4f3ab86a-ae39-4070-9b5e-8eed3ec45734`; gateway worker-list visibility lag makes this row directionally useful only |
| RunPod | A100 80GB PCIe | stopped_instance_start | `8,945 ms` | `7,385 ms` | `47,320 ms` | `56,616 ms` | `58,113 ms` | `1,497 ms` | same reused instance after explicit `stop`/`start`; worker `bdb62ba8-fb97-4c49-8dec-89e25a11ea57` |
| RunPod | A100 80GB PCIe | stopped_instance_reuse | `9,573 ms` | `5,331 ms` | `47,010 ms` | `55,210 ms` | `56,764 ms` | `1,554 ms` | same `instance_id=4jal1lkguq9mut` returned by a matching provision request; worker `1aee620e-deca-4c13-8277-accce15df911` |

### Cold-Start Sample Details

- Date: `2026-03-23`
- Gateway: `https://inferai.co.in`
- Provider: `runpod`
- GPU: `A100_80GB` with `provider_gpu_type_id="NVIDIA A100 80GB PCIe"`
- Model: `Qwen/Qwen2.5-7B-Instruct`
- Cold-start helper: [`scripts/cold-start-benchmark.py`](/Users/siddharthsingh/codingtensor/infera/scripts/cold-start-benchmark.py)
- Helper invocation:
  `python3 scripts/cold-start-benchmark.py https://inferai.co.in --api-key "$INFERA_ADMIN_KEY" --provider runpod --gpu-type A100_80GB --provider-gpu-type-id "NVIDIA A100 80GB PCIe" --gpu-count 1 --model "Qwen/Qwen2.5-7B-Instruct" --health-insecure --json-output /tmp/cold-start-a100-80.json`
- First-success probe: single completion request using the same model through `/v1/chat/completions`
- Worker image: updated image compatible with optional vLLM engine args, detailed startup substages, and async tokenizer warmup after readiness
- Worker `/health` now exposes `startup.stages` and `startup.durations_ms`, so `server_started` and `model_load_finished` are read from worker-emitted timestamps instead of being inferred from later observation time.
- This sample also includes finer-grained worker load substages such as `engine_create_finished`, `vllm_engine_init_finished`, and `tokenizer_load_deferred`, which isolates the actual vLLM init cost from the rest of startup.
- `request_to_running_ms` is still a coarse gateway milestone from `/api/instances/*`; it can lag the worker’s own `server_started` timestamp.
- Direct scripted `urllib` access to the RunPod proxy health URL can be blocked with `HTTP 403 / error code 1010`; the helper script now falls back to local `curl` for the health fetch path.
- Worker registration is now polled independently from health stages so a slow RunPod proxy `/health` path does not block `T4` or the first-success probe.
- The `fresh_provision` row remains skewed because the gateway `/api/workers` view lagged the worker’s own `gateway_registered` timestamp by a large margin on this sample.

### Early Read on the Data

- The current warm `A100_80GB` sample shows `least_loaded + no reuse` outperforming `affinity reuse` for `Qwen/Qwen2.5-7B-Instruct` at `concurrency=4`:
  - no reuse: `TTFT p50=937.8ms`, aggregate decode `587.77 tok/s`, cost/query about `$0.000493`
  - affinity reuse: `TTFT p50=1055.5ms`, aggregate decode `494.35 tok/s`, cost/query about `$0.000512`
- On this branch and workload, affinity should not be considered a default win; it needs further investigation under a more obviously cache-friendly multi-turn pattern before changing routing defaults.

- `fresh_provision` is still directionally useful, but not apples-to-apples, because gateway worker-list visibility lag inflated `request_to_registered_ms` and `request_to_first_success_ms`.
- The reliable current baselines are the warm-cache restart paths:
  - `stopped_instance_start`: `58,113 ms` to first success
  - `stopped_instance_reuse`: `56,764 ms` to first success
- `stopped_instance_start` and `stopped_instance_reuse` both spend about `47 s` in model load on this image/model combination.
- The fine-grained startup substages show the real cost center:
  - `engine_create_finished`: about `4.2-4.7 s`
  - `vllm_engine_init_finished`: about `47.0 s` on start/reuse and `176.8 s` on the fresh sample
- That means vLLM engine/model initialization is still the dominant cold-start bottleneck, not the extra Python-side setup.
- Async tokenizer warmup now completes in the background on the measured sample:
  - `tokenizer_warmup_finished` appears in `fresh_provision` and `stopped_instance_reuse`
  - `registered_to_first_success_ms` is now about `1.5-1.8 s`
- The current bottleneck is no longer infrastructure boot alone; vLLM model initialization is the dominant cold-start cost even on the reuse/start paths.

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
