# Modular Inference Backend

This document describes the production backend architecture for plug-and-play inference engines in the worker and control plane.

## Goals

- keep engine selection explicit from provisioning through worker startup
- isolate engine-specific logic inside engine modules
- preserve current `vllm` behavior while making additional runtimes first-class
- allow future engines to be added without changing worker request handling
- keep benchmark metadata aligned with engine, GPU, model, and runtime startup characteristics

## Current Engine Set

- `vllm`
- `sglang`
- `tensorrt_llm`
- `mock`

## Architecture

### Worker-side contracts

The worker uses a single `InferenceEngine` contract in:

- [/Users/siddharthsingh/codingtensor/infera/python/src/infera_worker/engine.py](/Users/siddharthsingh/codingtensor/infera/python/src/infera_worker/engine.py)

Engine implementations must provide:

- model load / unload
- loaded-model inspection
- non-streaming inference
- streaming inference
- request cancellation
- memory usage reporting
- optional startup stage and metadata reporting
- optional post-ready runtime warmup

### Registry and factory

The worker no longer hardcodes engine construction with a simple `if engine == "vllm"` branch.

Instead it uses:

- `EngineDefinition`
- `EngineCapabilities`
- `register_engine(...)`
- `get_engine_definition(...)`
- `create_engine(...)`

Builtin engines are lazily imported by module path, so optional dependencies are only loaded when the selected engine is used.

### Shared engine utilities

Common runtime helpers live in:

- [/Users/siddharthsingh/codingtensor/infera/python/src/infera_worker/engines/base.py](/Users/siddharthsingh/codingtensor/infera/python/src/infera_worker/engines/base.py)

This layer provides:

- startup stage recorder plumbing
- startup metadata recorder plumbing
- cache probe diagnostics for model load analysis
- GPU memory reporting helpers
- lazy tokenizer loading
- chat-template prompt building with a stable fallback format

### Engine modules

Each engine lives in its own module:

- [/Users/siddharthsingh/codingtensor/infera/python/src/infera_worker/engines/vllm_engine.py](/Users/siddharthsingh/codingtensor/infera/python/src/infera_worker/engines/vllm_engine.py)
- [/Users/siddharthsingh/codingtensor/infera/python/src/infera_worker/engines/sglang_engine.py](/Users/siddharthsingh/codingtensor/infera/python/src/infera_worker/engines/sglang_engine.py)
- [/Users/siddharthsingh/codingtensor/infera/python/src/infera_worker/engines/tensorrt_llm_engine.py](/Users/siddharthsingh/codingtensor/infera/python/src/infera_worker/engines/tensorrt_llm_engine.py)
- [/Users/siddharthsingh/codingtensor/infera/python/src/infera_worker/engines/mock_engine.py](/Users/siddharthsingh/codingtensor/infera/python/src/infera_worker/engines/mock_engine.py)

Engine-specific behavior should stay in these modules. The worker, HTTP server, and gateway should not accumulate engine-specific branches beyond selection, metadata, and provisioning/runtime-env plumbing.

## Configuration Model

Worker configuration remains env-driven, but is now explicit per engine in:

- [/Users/siddharthsingh/codingtensor/infera/python/src/infera_worker/config.py](/Users/siddharthsingh/codingtensor/infera/python/src/infera_worker/config.py)

Key design choices:

- `INFERA_ENGINE` is normalized to a canonical engine ID
- legacy `vllm` envs are preserved
- `sglang_*` and `tensorrt_llm_*` settings are explicit instead of overloading `vllm_*`
- typed runtime views exist for each engine:
  - `vllm_runtime`
  - `sglang_runtime`
  - `tensorrt_llm_runtime`

This keeps the external config surface stable while avoiding a single untyped `options` bag inside the worker.

## Control Plane Integration

Engine selection is now first-class in Go:

- [/Users/siddharthsingh/codingtensor/infera/go/internal/providers/types.go](/Users/siddharthsingh/codingtensor/infera/go/internal/providers/types.go)
- [/Users/siddharthsingh/codingtensor/infera/go/internal/gateway/instance_handlers.go](/Users/siddharthsingh/codingtensor/infera/go/internal/gateway/instance_handlers.go)
- [/Users/siddharthsingh/codingtensor/infera/go/internal/providers/runtime.go](/Users/siddharthsingh/codingtensor/infera/go/internal/providers/runtime.go)

`ProvisionRequest` and `Instance` now carry `engine`.

Provider adapters set `INFERA_ENGINE` from the request instead of hardcoding `vllm`.

Runtime defaults and worker env export are engine-aware:

- `vllm` keeps the existing tuned defaults
- `sglang` gets a conservative translated subset
- `tensorrt_llm` gets a conservative translated subset

## Deployment Images

Engine-specific production worker images now live in:

- [deploy/docker/Dockerfile.worker.vllm](/Users/siddharthsingh/codingtensor/infera/deploy/docker/Dockerfile.worker.vllm)
- [deploy/docker/Dockerfile.worker.sglang](/Users/siddharthsingh/codingtensor/infera/deploy/docker/Dockerfile.worker.sglang)
- [deploy/docker/Dockerfile.worker.tensorrt_llm](/Users/siddharthsingh/codingtensor/infera/deploy/docker/Dockerfile.worker.tensorrt_llm)

The build helper in [scripts/build-docker.sh](/Users/siddharthsingh/codingtensor/infera/scripts/build-docker.sh) now exposes separate targets for each engine image.

Gateway image selection now supports:

- `INFERA_WORKER_IMAGE` as the global fallback image
- `INFERA_WORKER_IMAGE_VLLM`
- `INFERA_WORKER_IMAGE_SGLANG`
- `INFERA_WORKER_IMAGE_TENSORRT_LLM`
- `INFERA_WORKER_IMAGE_MOCK`

Resolution rule:

- use the engine-specific image when configured
- otherwise fall back to `INFERA_WORKER_IMAGE`

Why separate images:

- dependency isolation between runtimes
- smaller and easier-to-debug engine-specific builds
- cleaner benchmark attribution
- fewer cross-engine packaging conflicts in production

## Error Handling

The architecture assumes three failure classes:

1. selection/config errors
   - invalid engine name
   - unsupported engine option combinations

2. dependency/runtime availability errors
   - optional engine package not installed
   - runtime API missing expected entry points

3. inference/runtime execution errors
   - model load failure
   - generation failure
   - cancellation or streaming errors

The current implementation guards missing optional dependencies at engine construction time and keeps the worker request path generic.

## Lifecycle

Worker startup sequence:

1. create engine from registry
2. attach startup stage + metadata recorders
3. preload configured models
4. mark worker ready
5. run post-ready runtime warmup where supported

This preserves the existing startup instrumentation model and keeps engine-specific warmup behavior out of the worker lifecycle logic.

## Assumptions and Tradeoffs

- `vllm` remains the most production-exercised engine path.
- `sglang` and `tensorrt_llm` support is intentionally conservative:
  - optional dependency guarded
  - explicit engine module boundaries
  - minimal translated defaults
- `sglang` and `tensorrt_llm` require dedicated worker images; the existing vLLM image is not intended to serve them.
- TensorRT-LLM model loading assumes an LLM API-compatible model or engine path is provided via `model_path` when required by the runtime.
- TensorRT-LLM packaging may require a non-default package source. The Dockerfile exposes build args for the package name and extra Python package index so deployment environments can override them without editing the image definition.
- TensorRT-LLM cannot be fully import-verified during a standard CPU-only Docker build because its import path requires `libcuda.so.1`. Final validation for that image must happen as a runtime smoke check on a GPU-backed host.
- Engine-specific performance tuning is not assumed to be portable across runtimes.

## Next Steps

- add engine-specific conformance tests against real installed runtimes in CI or staging images
- extend deployment presets so engine-specific worker images or entrypoints can be selected if needed
- benchmark `vllm`, `sglang`, and `tensorrt_llm` on the same GPU/model matrix using the template in [ENGINE_BENCHMARK_MATRIX_TEMPLATE.md](/Users/siddharthsingh/codingtensor/infera/docs/ENGINE_BENCHMARK_MATRIX_TEMPLATE.md)
