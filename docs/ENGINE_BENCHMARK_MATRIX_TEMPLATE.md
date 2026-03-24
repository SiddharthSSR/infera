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
| tensorrt_llm | runpod | A100_80GB | 1 | Qwen/Qwen2.5-7B-Instruct | conversation | 4 | none |  |  |  |  |  |  |  |  |

## Collection Notes

- For cold-start runs, use:
  - [/Users/siddharthsingh/codingtensor/infera/scripts/cold-start-benchmark.py](/Users/siddharthsingh/codingtensor/infera/scripts/cold-start-benchmark.py)
  - [/Users/siddharthsingh/codingtensor/infera/scripts/capture-startup-health.py](/Users/siddharthsingh/codingtensor/infera/scripts/capture-startup-health.py)

- For warm request-path runs, use:
  - [/Users/siddharthsingh/codingtensor/infera/scripts/benchmark-chat.py](/Users/siddharthsingh/codingtensor/infera/scripts/benchmark-chat.py)

- Always record:
  - engine image / runtime version
  - exact worker image
  - pinned model revision if applicable
  - explicit engine runtime options

- Conservative baseline and later tuning sweeps are defined in:
  - [/Users/siddharthsingh/codingtensor/infera/scripts/benchmark-presets/engine-phase-1-conservative.json](/Users/siddharthsingh/codingtensor/infera/scripts/benchmark-presets/engine-phase-1-conservative.json)
  - [/Users/siddharthsingh/codingtensor/infera/scripts/benchmark-presets/engine-phase-2-tuning-space.json](/Users/siddharthsingh/codingtensor/infera/scripts/benchmark-presets/engine-phase-2-tuning-space.json)
