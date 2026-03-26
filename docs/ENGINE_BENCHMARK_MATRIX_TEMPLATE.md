# Engine Benchmark Matrix Template

Use this template to compare inference runtimes on the same hardware and model set.

Before filling this matrix, follow:

- [/Users/siddharthsingh/codingtensor/infera/docs/ENGINE_BENCHMARK_EXECUTION_PLAN.md](/Users/siddharthsingh/codingtensor/infera/docs/ENGINE_BENCHMARK_EXECUTION_PLAN.md)

## Required Dimensions

- engine
- provider
- GPU type
- GPU count
- model
- model path / artifact source
- prompt preset or workload class
- concurrency
- cache reuse mode

## Required Metrics

- TTFT
- total latency
- decode throughput
- aggregate throughput
- memory used
- memory total
- worker startup:
  - server started
  - model load finished
  - worker ready
  - first successful completion
- startup cache metadata:
  - local model path presence
  - Hugging Face cache presence
  - snapshot count

## Matrix

| Engine | Provider | GPU | GPU Count | Model | Workload | Concurrency | Cache Reuse | TTFT p50 | TTFT p95 | Decode tok/s p50 | Aggregate decode tok/s p50 | Memory Used | Startup load ms | First success ms | Notes |
| --- | --- | --- | --- | --- | --- | --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| vllm | runpod | A100_80GB | 1 | Qwen/Qwen2.5-7B-Instruct | conversation | 4 | none |  |  |  |  |  |  |  |  |
| sglang | runpod | A100_80GB | 1 | Qwen/Qwen2.5-7B-Instruct | conversation | 4 | none |  |  |  |  |  |  |  |  |

## Blocked Engines

| Engine | Status | Reason |
| --- | --- | --- |
| tensorrt_llm | blocked | Current RunPod A100_80GB Qwen/Qwen2.5-7B-Instruct path does not have a reliable compatible TensorRT-LLM runtime and host combination. |

## Phase 2 Tuning Matrix

| Engine | Profile | Knob Group | Runtime Overrides | Goal | Status | Notes |
| --- | --- | --- | --- | --- | --- | --- |
| vllm | baseline_conservative | baseline | inherit Phase 1 conservative env | comparison anchor | pending |  |
| vllm | prefill_batching_4096 | prefill_batching | `INFERA_VLLM_MAX_NUM_BATCHED_TOKENS=4096` | improve aggregate throughput without large TTFT regression | pending |  |
| vllm | prefill_batching_8192 | prefill_batching | `INFERA_VLLM_MAX_NUM_BATCHED_TOKENS=8192` | maximize batching efficiency | pending |  |
| vllm | concurrency_32 | concurrency | `INFERA_VLLM_MAX_NUM_SEQS=32` | reduce queueing pressure | pending |  |
| vllm | concurrency_48 | concurrency | `INFERA_VLLM_MAX_NUM_SEQS=48` | improve steady-state throughput | pending |  |
| vllm | scheduler_steps_2 | scheduler | `INFERA_VLLM_NUM_SCHEDULER_STEPS=2` | lower scheduling overhead | pending |  |
| vllm | scheduler_steps_4 | scheduler | `INFERA_VLLM_NUM_SCHEDULER_STEPS=4` | test higher batching efficiency | pending |  |
| sglang | baseline_conservative | baseline | inherit Phase 1 conservative env | comparison anchor | pending |  |
| sglang | chunked_prefill_2048 | chunked_prefill | `INFERA_SGLANG_CHUNKED_PREFILL_SIZE=2048` | smooth TTFT and tails | pending |  |
| sglang | chunked_prefill_4096 | chunked_prefill | `INFERA_SGLANG_CHUNKED_PREFILL_SIZE=4096` | test balanced prefill sizing | pending |  |
| sglang | running_requests_32 | running_requests | `INFERA_SGLANG_MAX_RUNNING_REQUESTS=32` | reduce tail latency and capacity pressure | pending |  |
| sglang | running_requests_48 | running_requests | `INFERA_SGLANG_MAX_RUNNING_REQUESTS=48` | improve throughput | pending |  |
| sglang | memory_fraction_085 | memory_fraction | `INFERA_SGLANG_MEM_FRACTION_STATIC=0.85` | recover memory headroom | pending |  |
| sglang | memory_fraction_094 | memory_fraction | `INFERA_SGLANG_MEM_FRACTION_STATIC=0.94` | push utilization higher | pending |  |

## Collection Notes

- For cold-start runs, use:
  - [/Users/siddharthsingh/codingtensor/infera/scripts/cold-start-benchmark.py](/Users/siddharthsingh/codingtensor/infera/scripts/cold-start-benchmark.py)
  - [/Users/siddharthsingh/codingtensor/infera/scripts/capture-startup-health.py](/Users/siddharthsingh/codingtensor/infera/scripts/capture-startup-health.py)

- For warm request-path runs, use:
  - [/Users/siddharthsingh/codingtensor/infera/scripts/benchmark-chat.py](/Users/siddharthsingh/codingtensor/infera/scripts/benchmark-chat.py)

- To combine all untuned Phase 1 engine runs into one report, use:
  - [/Users/siddharthsingh/codingtensor/infera/scripts/summarize-engine-phase1-baseline.py](/Users/siddharthsingh/codingtensor/infera/scripts/summarize-engine-phase1-baseline.py)

- To execute named Phase 2 tuning profiles, use:
  - [/Users/siddharthsingh/codingtensor/infera/scripts/run-engine-benchmark-phase2.py](/Users/siddharthsingh/codingtensor/infera/scripts/run-engine-benchmark-phase2.py)

- Always record:
  - engine image / runtime version
  - exact worker image
  - pinned model revision if applicable
  - explicit engine runtime options

- Conservative baseline and later tuning sweeps are defined in:
  - [/Users/siddharthsingh/codingtensor/infera/scripts/benchmark-presets/engine-phase-1-conservative.json](/Users/siddharthsingh/codingtensor/infera/scripts/benchmark-presets/engine-phase-1-conservative.json)
  - [/Users/siddharthsingh/codingtensor/infera/scripts/benchmark-presets/engine-phase-2-tuning-space.json](/Users/siddharthsingh/codingtensor/infera/scripts/benchmark-presets/engine-phase-2-tuning-space.json)
