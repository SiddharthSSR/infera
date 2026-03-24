# Engine Benchmark Execution Plan

Use this plan when comparing `vllm`, `sglang`, and `tensorrt_llm`.

## Principle

Benchmark first, tune second.

Do not start with engine-specific tuning sweeps before you have:

- one warm baseline per engine
- one cold-start baseline per engine
- one startup-health capture per engine

Without that baseline, you cannot separate:

- raw engine/runtime differences
- image/build differences
- tuning gains from arbitrary preset choices

## Benchmark Order

### Phase 1: Conservative baseline

Run each engine with a conservative, production-safe preset.

Goal:

- establish comparable starting numbers
- identify the weakest dimension for each engine
- avoid overfitting before the first measurements exist

Artifacts:

- warm benchmark JSON
- cold-start benchmark JSON
- startup-health JSON

### Phase 2: Engine-specific tuning

After Phase 1, tune one engine at a time.

Goal:

- improve the weakest metric for that engine
- keep changes attributable
- avoid changing several unrelated knobs in one step

Artifacts:

- tuned benchmark JSON
- benchmark delta notes vs baseline

### Phase 3: Final comparison

Compare:

- baseline `vllm`
- tuned `vllm`
- baseline `sglang`
- tuned `sglang`
- baseline `tensorrt_llm`
- tuned `tensorrt_llm`

Use the same matrix format in [ENGINE_BENCHMARK_MATRIX_TEMPLATE.md](/Users/siddharthsingh/codingtensor/infera/docs/ENGINE_BENCHMARK_MATRIX_TEMPLATE.md).

## Scope for the First Matrix

Use the same workload across all engines:

- provider: `runpod`
- GPU: `A100_80GB`
- GPU count: `1`
- model: `Qwen/Qwen2.5-7B-Instruct`
- warm workload: `conversation`
- warm runs: `3`
- warmup groups: `2`
- concurrency: `4`
- cache reuse modes:
  - `none`
  - `affinity`

Use the same cold-start scenarios across all engines:

- `fresh_provision`
- `stopped_instance_start`
- `stopped_instance_reuse`

## Phase 1 Commands

### Orchestrated runner

Use the Phase 1 runner when you want one command per engine instead of invoking the three helper scripts manually:

```bash
python3 scripts/run-engine-benchmark-phase1.py \
  https://inferai.co.in \
  --api-key "$INFERA_ADMIN_KEY" \
  --engine vllm \
  --provider runpod \
  --gpu-type A100_80GB \
  --provider-gpu-type-id "NVIDIA A100 80GB PCIe" \
  --gpu-count 1 \
  --model "Qwen/Qwen2.5-7B-Instruct" \
  --cost-per-hour 1.19 \
  --health-insecure
```

This runner:

- executes warm `none`
- executes warm `affinity`
- executes the cold-start benchmark
- executes startup-health capture with `--include-restart`
- writes a manifest for the full Phase 1 run

It does not deploy engine-specific fleets for you. Before each run, ensure the active fleet for the target model is deployed with only the selected engine.

### Warm baseline

Run once per engine:

```bash
python3 scripts/benchmark-chat.py \
  https://inferai.co.in \
  --api-key "$INFERA_ADMIN_KEY" \
  --model "Qwen/Qwen2.5-7B-Instruct" \
  --engine-label "vllm" \
  --provider-label "runpod" \
  --gpu-label "A100_80GB" \
  --preset conversation \
  --runs 3 \
  --warmup 2 \
  --concurrency 4 \
  --cache-reuse-mode none \
  --cost-per-hour 1.19 \
  --json-output /tmp/infera-benchmark-vllm-a100-80-none.json
```

```bash
python3 scripts/benchmark-chat.py \
  https://inferai.co.in \
  --api-key "$INFERA_ADMIN_KEY" \
  --model "Qwen/Qwen2.5-7B-Instruct" \
  --engine-label "vllm" \
  --provider-label "runpod" \
  --gpu-label "A100_80GB" \
  --preset conversation \
  --runs 3 \
  --warmup 2 \
  --concurrency 4 \
  --cache-reuse-mode affinity \
  --cache-key-prefix baseline \
  --cost-per-hour 1.19 \
  --json-output /tmp/infera-benchmark-vllm-a100-80-affinity.json
```

Replace `vllm` with `sglang` and `tensorrt_llm` for the other engines.

### Cold-start baseline

Run once per engine:

```bash
python3 scripts/cold-start-benchmark.py \
  https://inferai.co.in \
  --api-key "$INFERA_ADMIN_KEY" \
  --provider runpod \
  --engine vllm \
  --gpu-type A100_80GB \
  --provider-gpu-type-id "NVIDIA A100 80GB PCIe" \
  --gpu-count 1 \
  --model "Qwen/Qwen2.5-7B-Instruct" \
  --health-insecure \
  --json-output /tmp/cold-start-vllm-a100-80.json
```

### Startup-health capture

Run once per engine:

```bash
python3 scripts/capture-startup-health.py \
  https://inferai.co.in \
  --api-key "$INFERA_ADMIN_KEY" \
  --provider runpod \
  --engine vllm \
  --gpu-type A100_80GB \
  --provider-gpu-type-id "NVIDIA A100 80GB PCIe" \
  --gpu-count 1 \
  --model "Qwen/Qwen2.5-7B-Instruct" \
  --health-insecure \
  --include-restart \
  --json-output /tmp/startup-health-vllm-a100-80.json
```

## Conservative Phase 1 Presets

Use the preset file:

- [/Users/siddharthsingh/codingtensor/infera/scripts/benchmark-presets/engine-phase-1-conservative.json](/Users/siddharthsingh/codingtensor/infera/scripts/benchmark-presets/engine-phase-1-conservative.json)

These presets are intentionally conservative:

- keep tensor parallel simple
- avoid aggressive queue/batch tuning
- set only a small number of explicit runtime knobs
- preserve current stable `vllm` behavior

## Phase 2 Tuning Loop

Tune one engine at a time.

### Step 1: pick the primary bottleneck

Choose one:

- TTFT
- aggregate decode throughput
- memory usage
- startup/load time

### Step 2: change one knob group only

Use the candidate sweep file:

- [/Users/siddharthsingh/codingtensor/infera/scripts/benchmark-presets/engine-phase-2-tuning-space.json](/Users/siddharthsingh/codingtensor/infera/scripts/benchmark-presets/engine-phase-2-tuning-space.json)

Recommended order:

#### vLLM

1. prefill / batching
2. concurrency limits
3. scheduler steps
4. speculative decoding

#### SGLang

1. `chunked_prefill_size`
2. `max_running_requests`
3. `mem_fraction_static`
4. backend selections

#### TensorRT-LLM

1. engine artifact quality / build path
2. `max_batch_size`
3. `max_num_tokens`
4. KV cache sizing
5. chunked context

### Step 3: rerun the same matrix

After each sweep winner:

- rerun warm `none`
- rerun warm `affinity`
- rerun startup-health
- rerun restart/reuse cold-start if startup changed materially

## Decision Rules

Use these priorities:

1. correctness and stability
2. warm request-path performance
3. cold restart/reuse performance
4. fresh-provision performance

Do not accept a tuned preset that improves one metric but causes:

- higher error rate
- unstable streaming
- materially worse memory pressure
- materially worse startup behavior

## Important Implementation Caveat

The public `/api/instances/provision` API now supports engine selection, but it still does **not** expose arbitrary runtime option overrides for ad hoc sweeps.

That means tuned comparisons currently require one of:

- engine-specific benchmark images with the preset baked in
- temporary control-plane runtime-default overrides
- direct provider/manager-side request construction in internal tooling

So:

- Phase 1 can use the conservative defaults already wired into the codebase
- deeper Phase 2 sweeps need either image-level or control-plane-level support

## What to Record Every Time

- branch
- commit
- worker image
- engine runtime version
- exact model revision or artifact path
- explicit runtime env values
- provider and GPU type
- whether the model was loaded from cache or remote artifacts
