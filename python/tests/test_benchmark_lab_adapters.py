"""Tests for engine runtime adapters."""

from __future__ import annotations

from infera_bench.adapters import build_adapter_registry
from infera_bench.catalog import default_catalog_root, load_catalog_bundle


def test_vllm_adapter_translates_generic_parameters() -> None:
    bundle = load_catalog_bundle(default_catalog_root())
    adapter = build_adapter_registry(bundle)["vllm"]
    model = bundle.resolve_model("Qwen/Qwen2.5-7B-Instruct")
    hardware = bundle.resolve_hardware("a100_80gb")

    resolution = adapter.resolve(
        model=model,
        hardware=hardware,
        gpu_count=1,
        parameters={
            "tensor_parallelism": 1,
            "context_length": 32768,
            "gpu_memory_utilization": 0.94,
            "prefix_caching": True,
            "batch_token_budget": 8192,
        },
    )

    assert resolution.status == "ready"
    assert resolution.runtime_options == {
        "INFERA_VLLM_ENABLE_PREFIX_CACHING": "true",
        "INFERA_VLLM_GPU_MEMORY_UTILIZATION": "0.94",
        "INFERA_VLLM_MAX_MODEL_LEN": "32768",
        "INFERA_VLLM_MAX_NUM_BATCHED_TOKENS": "8192",
        "INFERA_VLLM_TENSOR_PARALLEL_SIZE": "1",
    }


def test_adapter_marks_invalid_tensor_parallelism() -> None:
    bundle = load_catalog_bundle(default_catalog_root())
    adapter = build_adapter_registry(bundle)["sglang"]
    model = bundle.resolve_model("Qwen/Qwen2.5-7B-Instruct")
    hardware = bundle.resolve_hardware("a100_80gb")

    resolution = adapter.resolve(
        model=model,
        hardware=hardware,
        gpu_count=1,
        parameters={"tensor_parallelism": 2},
    )

    assert resolution.status == "invalid"
    assert any(issue.field == "tensor_parallelism" for issue in resolution.issues)
