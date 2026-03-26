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

### Engine image preparation

Before benchmarking an engine, build and deploy its dedicated worker image.

If your gateway deployment already uses engine-specific image env vars, you do not need to keep swapping the global `INFERA_WORKER_IMAGE`. Set the appropriate `INFERA_WORKER_IMAGE_<ENGINE>` values once and provision with the target engine.

Examples:

```bash
VERSION=engine-phase1 ./scripts/build-docker.sh --worker-vllm --push
```

```bash
VERSION=engine-phase1 ./scripts/build-docker.sh --worker-sglang --push
```

```bash
VERSION=engine-phase1 ./scripts/build-docker.sh --worker-tensorrt-llm --push
```

For TensorRT-LLM, use NVIDIA's official NGC TensorRT-LLM container as the base image. If you need a different official release tag, override it at build time:

```bash
WORKER_TENSORRT_LLM_BASE_IMAGE=nvcr.io/nvidia/tritonserver:24.08-trtllm-python-py3 \
VERSION=engine-phase1 \
./scripts/build-docker.sh --worker-tensorrt-llm --push
```

You may need to authenticate to NGC first:

```bash
docker login nvcr.io
```

### GitHub Actions build path

If local or ad-hoc VM builds are unreliable, use the manual GitHub Actions workflow at
[.github/workflows/build-worker-image.yml](/Users/siddharthsingh/codingtensor/infera/.github/workflows/build-worker-image.yml).

Required repository secrets:

- `DOCKERHUB_USERNAME`
- `DOCKERHUB_TOKEN`
- `NGC_API_KEY` for `tensorrt_llm`

Recommended dispatch inputs for TensorRT-LLM:

- `engine`: `tensorrt_llm`
- `docker_namespace`: `codingtensor`
- `cleanup_runner`: `true`
- `runs_on`: `["ubuntu-latest"]` for hosted runners, or a self-hosted label array if you have a larger runner
- `tensorrt_base_image`: `nvcr.io/nvidia/tritonserver:24.08-trtllm-python-py3`

The workflow aggressively frees disk on GitHub-hosted runners before building, but TensorRT-LLM images are still large enough that a larger self-hosted runner may be preferable.

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

- executes the cold-start benchmark
- executes startup-health capture with `--include-restart`
- executes warm `none`
- executes warm `affinity`
- writes a manifest for the full Phase 1 run

It does not deploy engine-specific fleets for you. Before each run, ensure the active fleet for the target model is deployed with only the selected engine.

If warm steps are included after lifecycle steps, the runner keeps the final provisioned instance alive for the warm requests even when `--terminate-final-instance` is set. That avoids tearing down the only active worker immediately before the warm benchmark. Clean up the retained instance separately after the run if needed.

### Baseline report generation

After collecting Phase 1 outputs for each engine, combine them into one untuned baseline report:

```bash
python3 scripts/summarize-engine-phase1-baseline.py \
  /tmp/infera-engine-benchmarks \
  --markdown-output /tmp/engine-phase1-baseline.md \
  --json-output /tmp/engine-phase1-baseline.json
```

The summarizer auto-discovers `phase1-*-manifest.json` files, renders warm/cold/startup tables, and calls out missing artifacts so incomplete baselines are explicit.

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
