"""Tests for vLLM runtime knob wiring."""

from __future__ import annotations

from types import SimpleNamespace

import pytest

from infera_worker.config import ModelConfig, WorkerConfig
from infera_worker.engines import vllm_engine as vllm_module


class FakeAsyncEngineArgs:
    """Capture kwargs passed into AsyncEngineArgs."""

    last_kwargs: dict[str, object] | None = None

    def __init__(self, **kwargs):
        self.kwargs = kwargs
        FakeAsyncEngineArgs.last_kwargs = kwargs


class FakeAsyncLLMEngine:
    """Minimal fake AsyncLLMEngine for load_model tests."""

    @classmethod
    def from_engine_args(cls, engine_args):
        return SimpleNamespace(
            model_config=SimpleNamespace(
                max_model_len=engine_args.kwargs.get("max_model_len") or 4096,
            )
        )


@pytest.mark.asyncio
async def test_vllm_engine_passes_runtime_tuning_knobs(monkeypatch):
    monkeypatch.setattr(vllm_module, "VLLM_AVAILABLE", True)
    monkeypatch.setattr(vllm_module, "AsyncEngineArgs", FakeAsyncEngineArgs)
    monkeypatch.setattr(vllm_module, "AsyncLLMEngine", FakeAsyncLLMEngine)

    config = WorkerConfig(
        engine="vllm",
        vllm_tensor_parallel_size=4,
        vllm_gpu_memory_utilization=0.93,
        vllm_max_model_len=8192,
        vllm_max_num_batched_tokens=4096,
        vllm_max_num_seqs=64,
        vllm_swap_space=12.5,
        vllm_enforce_eager=True,
        vllm_num_scheduler_steps=6,
    )
    engine = vllm_module.VLLMEngine(config)

    loaded = await engine.load_model(ModelConfig(model_id="Qwen/Qwen2.5-7B-Instruct"))

    assert loaded.max_sequence_length == 8192
    assert FakeAsyncEngineArgs.last_kwargs is not None
    assert FakeAsyncEngineArgs.last_kwargs["tensor_parallel_size"] == 4
    assert FakeAsyncEngineArgs.last_kwargs["gpu_memory_utilization"] == 0.93
    assert FakeAsyncEngineArgs.last_kwargs["max_model_len"] == 8192
    assert FakeAsyncEngineArgs.last_kwargs["max_num_batched_tokens"] == 4096
    assert FakeAsyncEngineArgs.last_kwargs["max_num_seqs"] == 64
    assert FakeAsyncEngineArgs.last_kwargs["swap_space"] == 12.5
    assert FakeAsyncEngineArgs.last_kwargs["enforce_eager"] is True
    assert FakeAsyncEngineArgs.last_kwargs["num_scheduler_steps"] == 6
