# Infera Quantization Benchmark Matrix

Use this document to evaluate quantized OSS model variants before promoting them into any default runtime path.

Status: live `L40S` benchmark results captured for Mistral and Qwen; manual smoke validation and standard `A100` confirmation still pending

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

Live warm-run results captured on `2026-03-21` against the live gateway on `L40S` with `--runs 3 --warmup 2 --concurrency 4 --cost-per-hour 0.99`. These rows are enough to make provisional throughput decisions, but not enough to promote a default runtime without the manual smoke checks above.

| Family | Base model | Quantized model | GPU | Scenario | TTFT p50 | TTFT p95 | Decode tok/s p50 | Decode tok/s p95 | Cost/query | Quality result | Decision |
|---|---|---|---|---|---:|---:|---:|---:|---:|---|---|
| Mistral | `mistralai/Mistral-7B-Instruct-v0.3` | `solidrust/Mistral-7B-Instruct-v0.3-AWQ` | `L40S` | no reuse | `641.0ms` | `735.8ms` | `144.58` | `154.20` | `$0.000417` | benchmark conversation passed; manual smoke pending | provisional winner over base on throughput and TTFT |
| Mistral | `mistralai/Mistral-7B-Instruct-v0.3` | `solidrust/Mistral-7B-Instruct-v0.3-AWQ` | `L40S` | affinity reuse | `663.3ms` | `1463.8ms` | `140.39` | `149.59` | `$0.000423` | benchmark conversation passed; manual smoke pending | throughput good, but tail TTFT too spiky to prefer over no reuse |
| Qwen | `Qwen/Qwen2.5-7B-Instruct` | `Qwen/Qwen2.5-7B-Instruct-AWQ` | `L40S` | no reuse | `421.0ms` | `462.5ms` | `133.88` | `154.47` | `$0.000468` | benchmark conversation passed; manual smoke pending | provisional winner over base on throughput with slightly better TTFT |
| Qwen | `Qwen/Qwen2.5-7B-Instruct` | `Qwen/Qwen2.5-7B-Instruct-AWQ` | `L40S` | affinity reuse | `430.8ms` | `1217.0ms` | `135.20` | `165.33` | `$0.000273` | benchmark conversation passed; manual smoke pending | promising throughput, but rerun needed because of P95 spike |
| Qwen | `Qwen/Qwen2.5-7B-Instruct` | `Qwen/Qwen2.5-7B-Instruct-GPTQ-Int4` | `L40S` | no reuse | `1127.3ms` | `1624.0ms` | `131.50` | `155.45` | `$0.000449` | benchmark conversation passed; manual smoke pending | do not promote for interactive traffic; TTFT regresses too much |
| Qwen | `Qwen/Qwen2.5-7B-Instruct` | `Qwen/Qwen2.5-7B-Instruct-GPTQ-Int4` | `L40S` | affinity reuse | `1157.0ms` | `1639.0ms` | `141.31` | `158.63` | `$0.000474` | benchmark conversation passed; manual smoke pending | do not promote; affinity does not fix tail latency |
| Qwen | `Qwen/Qwen2.5-7B-Instruct` | `Qwen/Qwen2.5-7B-Instruct-GPTQ-Int8` | pending | pending | pending | pending | pending | pending | pending | smoke and live benchmark pending | pending |

## Observed Conclusions

- `solidrust/Mistral-7B-Instruct-v0.3-AWQ` is the strongest Mistral candidate so far on `L40S`.
  Base `no reuse` was `1168.9ms` TTFT p50 and `46.33` decode tok/s p50, while AWQ `no reuse` improved to `641.0ms` and `144.58`.
- `Qwen/Qwen2.5-7B-Instruct-AWQ` is the strongest Qwen candidate so far on `L40S`.
  Base `no reuse` was `462.6ms` TTFT p50 and `50.27` decode tok/s p50, while AWQ `no reuse` improved to `421.0ms` and `133.88`.
- `Qwen/Qwen2.5-7B-Instruct-GPTQ-Int4` has good throughput on `L40S`, but its TTFT regression is too large for interactive defaults.
- Affinity reuse is not a confirmed win for these quantized profiles yet.
  Both Mistral AWQ and Qwen AWQ showed strong throughput, but each had a high-tail TTFT run that needs rerun and explanation before we recommend affinity here.

## Promotion Rule

Only after a row above is filled with live benchmark data and quality notes should the variant be considered for:

- a runtime preset recommendation
- default model seed promotion
- benchmark baseline inclusion as a supported low-cost profile
