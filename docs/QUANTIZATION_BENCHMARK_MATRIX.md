# Infera Quantization Benchmark Matrix

Use this document to evaluate quantized OSS model variants before promoting them into any default runtime path.

Status: candidate matrix prepared, live benchmark execution pending

## Rules

- Do not switch a model to a quantized default until it passes both latency and quality smoke checks.
- Benchmark quantized variants on the same provider, GPU, gateway build, worker image, and routing mode as the base model.
- Record both warm throughput and cold-start behavior when the quantized artifact materially changes image size or model download time.
- Treat quantized candidates as opt-in until benchmark evidence is committed.

## Phase 1: Exact In-Repo Candidates

This repo now includes these exact quantized candidates in the model registry seed:

| Family | Base model ID | Quantized candidate ID | Quantization | Source grounding |
|---|---|---|---|---|
| Mistral | `mistralai/Mistral-7B-Instruct-v0.3` | `solidrust/Mistral-7B-Instruct-v0.3-AWQ` | `awq` | present in [`seed.go`](/Users/siddharthsingh/codingtensor/infera/go/internal/vault/seed.go#L25) |
| Qwen | `Qwen/Qwen2.5-7B-Instruct` | `Qwen/Qwen2.5-7B-Instruct-AWQ` | `awq` | present in [`seed.go`](/Users/siddharthsingh/codingtensor/infera/go/internal/vault/seed.go#L85) |
| Qwen | `Qwen/Qwen2.5-7B-Instruct` | `Qwen/Qwen2.5-7B-Instruct-GPTQ-Int4` | `gptq-int4` | present in [`seed.go`](/Users/siddharthsingh/codingtensor/infera/go/internal/vault/seed.go#L97) |
| Qwen | `Qwen/Qwen2.5-7B-Instruct` | `Qwen/Qwen2.5-7B-Instruct-GPTQ-Int8` | `gptq-int8` | present in [`seed.go`](/Users/siddharthsingh/codingtensor/infera/go/internal/vault/seed.go#L109) |

## Phase 2: Family Expansion Candidates

These are the next model families to benchmark once exact quantized IDs are verified in the registry or chosen explicitly for the environment:

| Family | Base model ID | Quantization targets | Notes |
|---|---|---|---|
| Qwen | `Qwen/Qwen2.5-7B-Instruct` | `awq`, `gptq`, `int4`, `int8` | hot-path benchmark target in current runtime presets |
| Llama | `meta-llama/Meta-Llama-3.1-8B-Instruct` | `awq`, `gptq`, `int4`, `int8` | gated model; benchmark only where license access is configured |
| Gemma | `google/gemma-2-9b-it` | `awq`, `gptq`, `int4`, `int8` | gated model; benchmark only where license access is configured |
| Mistral | `mistralai/Mistral-7B-Instruct-v0.3` | `awq`, `gptq`, `int4`, `int8` | start with the committed AWQ candidate above |

For Phase 2, do not add exact candidate IDs to defaults until:

1. the model artifact is verified to exist and load in the target environment
2. the quantized variant completes a smoke inference run
3. the benchmark results are committed alongside the base-model comparison

## Standard Benchmark Scenarios

Run every base-vs-quantized comparison in both scenarios:

1. Warm throughput, no cache reuse
2. Warm throughput, affinity reuse

Use the same benchmark harness for both:

```bash
python3 scripts/benchmark-chat.py \
  https://your-gateway.example.com \
  --api-key "$INFERA_SMOKE_API_KEY" \
  --model "MODEL_ID_HERE" \
  --preset conversation \
  --runs 3 \
  --warmup 2 \
  --concurrency 4 \
  --cache-reuse-mode none \
  --cost-per-hour COST_PER_HOUR \
  --json-output /tmp/infera-quant-benchmark.json
```

Then rerun with:

```bash
--cache-reuse-mode affinity --cache-key-prefix quant-baseline
```

## Quality Smoke Check

Before accepting a quantized candidate, verify:

- it loads successfully on the target GPU tier
- it returns a coherent response on a short prompt
- it completes a longer conversation prompt without obvious corruption, empty output, or pathological repetition
- streaming and non-streaming responses both succeed

Minimum smoke prompts:

1. `What is the capital of France? Answer in one short sentence.`
2. `Explain how a CDN reduces latency in under 120 words.`
3. Use the `conversation` preset from [`benchmark-chat.py`](/Users/siddharthsingh/codingtensor/infera/scripts/benchmark-chat.py)

## Acceptance Thresholds

Treat a quantized candidate as promotable only if all of these are true relative to the base model:

- TTFT p95 does not regress materially for the target workload
- decode tok/s p50 improves or cost/query improves enough to justify any latency tradeoff
- memory footprint drops enough to unlock a cheaper GPU tier or materially higher concurrency
- no obvious output-quality regression appears in smoke checks
- no load instability or OOM pattern appears during the benchmark run

## Result Table

Commit measured results here once a live benchmark target is available.

| Family | Base model | Quantized model | GPU | Scenario | TTFT p50 | TTFT p95 | Decode tok/s p50 | Decode tok/s p95 | Cost/query | Quality result | Decision |
|---|---|---|---|---|---:|---:|---:|---:|---:|---|---|
| Mistral | `mistralai/Mistral-7B-Instruct-v0.3` | `solidrust/Mistral-7B-Instruct-v0.3-AWQ` | pending | pending | pending | pending | pending | pending | pending | pending | pending |
| Qwen | `Qwen/Qwen2.5-7B-Instruct` | `Qwen/Qwen2.5-7B-Instruct-AWQ` | pending | pending | pending | pending | pending | pending | pending | pending | pending |
| Qwen | `Qwen/Qwen2.5-7B-Instruct` | `Qwen/Qwen2.5-7B-Instruct-GPTQ-Int4` | pending | pending | pending | pending | pending | pending | pending | pending | pending |
| Qwen | `Qwen/Qwen2.5-7B-Instruct` | `Qwen/Qwen2.5-7B-Instruct-GPTQ-Int8` | pending | pending | pending | pending | pending | pending | pending | pending | pending |

## Promotion Rule

Only after a row above is filled with live benchmark data and quality notes should the variant be considered for:

- a runtime preset recommendation
- default model seed promotion
- benchmark baseline inclusion as a supported low-cost profile
