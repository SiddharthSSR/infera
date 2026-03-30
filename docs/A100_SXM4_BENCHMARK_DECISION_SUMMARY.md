# A100 SXM4 Benchmark Decision Summary

This document captures the current benchmark-backed engine decisions from the `RunPod` `A100 SXM4 80GB` campaign completed on `2026-03-30`.

## Scope

- hardware: `A100 SXM4 80GB`
- provider: `RunPod`
- engines compared: `vllm`, `sglang`
- blocked engine: `tensorrt_llm`
- primary workloads:
  - `mixed`
  - `repeated_prefix`

The SXM results are treated as sufficient for the current rollout. `A100 80GB PCIe` is not part of the immediate decision set because that pool is rarely available and was unstable during earlier attempts.

## Frozen Winners

| Model | Mixed | Repeated Prefix | Notes |
| --- | --- | --- | --- |
| `Qwen/Qwen2.5-7B-Instruct` | `vllm baseline` | `sglang chunked_prefill_4096` | Qwen splits by workload class rather than engine-unifying cleanly. |
| `mistralai/Mistral-7B-Instruct-v0.3` | `sglang baseline` | `sglang chunked_prefill_4096` | SGLang is the cleaner default for both tested traffic classes. |
| `meta-llama/Meta-Llama-3.1-8B-Instruct` | `vllm baseline` | `vllm scheduler_4` | Score-based ranking over-promoted unstable SGLang mixed presets; final decision is based on artifact inspection. |

## Practical Rollout

Use the checked-in rollout policy in [configs/benchmark_lab/rollout_policy.json](../configs/benchmark_lab/rollout_policy.json).

The operational policy is:

1. Keep `vllm` as the general fallback/default.
2. Add benchmark-backed overrides only where results are clearly better.
3. Prefer engine choice at the fleet or deployment level rather than ad hoc per request.
4. Split traffic classes only when the workload class is explicit and benchmark-backed, such as `mixed` versus `repeated_prefix`.

## Models Outside The Active Default Set

- `Qwen/Qwen3.5-9B`
  - runnable on the current `A100 SXM4` `vllm` path
  - remains experimental because startup and ranking were materially worse than the current default set
- `MiniMax-M2.7`
  - added to the benchmark catalog as a tracked candidate
  - currently a provider-managed API model, not a self-hosted `vllm` or `sglang` benchmark target in this repo

## Why This Should Not Become A Single-Engine Policy

The benchmark data does not support a universal engine choice:

- `Qwen2.5 7B` splits by workload
- `Mistral 7B` currently favors `sglang`
- `Llama 3.1 8B` currently favors `vllm`

The right product decision is a benchmark-backed engine selection framework, not a single global engine decision.
