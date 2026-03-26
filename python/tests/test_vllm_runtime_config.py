"""Tests for vLLM runtime knob wiring."""

from __future__ import annotations

import sys
from types import SimpleNamespace

import pytest

from infera_worker.config import ModelConfig, WorkerConfig
from infera_worker.engines import vllm_engine as vllm_module


class FakeAsyncEngineArgs:
    """Capture kwargs passed into AsyncEngineArgs."""

    last_kwargs: dict[str, object] | None = None

    def __init__(
        self,
        *,
        model=None,
        revision=None,
        tensor_parallel_size=None,
        gpu_memory_utilization=None,
        max_model_len=None,
        quantization=None,
        trust_remote_code=None,
        enable_prefix_caching=None,
        enable_chunked_prefill=None,
        max_num_batched_tokens=None,
        max_num_seqs=None,
        swap_space=None,
        enforce_eager=None,
        num_scheduler_steps=None,
        speculative_model=None,
        num_speculative_tokens=None,
        ngram_prompt_lookup_num_tokens=None,
    ):
        kwargs = {
            "model": model,
            "revision": revision,
            "tensor_parallel_size": tensor_parallel_size,
            "gpu_memory_utilization": gpu_memory_utilization,
            "max_model_len": max_model_len,
            "quantization": quantization,
            "trust_remote_code": trust_remote_code,
            "enable_prefix_caching": enable_prefix_caching,
            "enable_chunked_prefill": enable_chunked_prefill,
            "max_num_batched_tokens": max_num_batched_tokens,
            "max_num_seqs": max_num_seqs,
            "swap_space": swap_space,
            "enforce_eager": enforce_eager,
            "num_scheduler_steps": num_scheduler_steps,
            "speculative_model": speculative_model,
            "num_speculative_tokens": num_speculative_tokens,
            "ngram_prompt_lookup_num_tokens": ngram_prompt_lookup_num_tokens,
        }
        self.kwargs = kwargs
        FakeAsyncEngineArgs.last_kwargs = kwargs


class FakeAsyncEngineArgsWithoutScheduler:
    """Capture kwargs for a vLLM build without num_scheduler_steps support."""

    last_kwargs: dict[str, object] | None = None

    def __init__(
        self,
        *,
        model=None,
        revision=None,
        tensor_parallel_size=None,
        gpu_memory_utilization=None,
        max_model_len=None,
        quantization=None,
        trust_remote_code=None,
        enable_prefix_caching=None,
        enable_chunked_prefill=None,
        max_num_batched_tokens=None,
        max_num_seqs=None,
        swap_space=None,
        enforce_eager=None,
        speculative_model=None,
        num_speculative_tokens=None,
        ngram_prompt_lookup_num_tokens=None,
    ):
        kwargs = {
            "model": model,
            "revision": revision,
            "tensor_parallel_size": tensor_parallel_size,
            "gpu_memory_utilization": gpu_memory_utilization,
            "max_model_len": max_model_len,
            "quantization": quantization,
            "trust_remote_code": trust_remote_code,
            "enable_prefix_caching": enable_prefix_caching,
            "enable_chunked_prefill": enable_chunked_prefill,
            "max_num_batched_tokens": max_num_batched_tokens,
            "max_num_seqs": max_num_seqs,
            "swap_space": swap_space,
            "enforce_eager": enforce_eager,
            "speculative_model": speculative_model,
            "num_speculative_tokens": num_speculative_tokens,
            "ngram_prompt_lookup_num_tokens": ngram_prompt_lookup_num_tokens,
        }
        self.kwargs = kwargs
        FakeAsyncEngineArgsWithoutScheduler.last_kwargs = kwargs


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


@pytest.mark.asyncio
async def test_vllm_engine_can_disable_trust_remote_code(monkeypatch):
    monkeypatch.setattr(vllm_module, "VLLM_AVAILABLE", True)
    monkeypatch.setattr(vllm_module, "AsyncEngineArgs", FakeAsyncEngineArgs)
    monkeypatch.setattr(vllm_module, "AsyncLLMEngine", FakeAsyncLLMEngine)

    engine = vllm_module.VLLMEngine(WorkerConfig(engine="vllm", trust_remote_code=False))

    await engine.load_model(ModelConfig(model_id="Qwen/Qwen2.5-7B-Instruct"))

    assert FakeAsyncEngineArgs.last_kwargs is not None
    assert FakeAsyncEngineArgs.last_kwargs["trust_remote_code"] is False


@pytest.mark.asyncio
async def test_vllm_engine_skips_unsupported_optional_knobs(monkeypatch):
    monkeypatch.setattr(vllm_module, "VLLM_AVAILABLE", True)
    monkeypatch.setattr(vllm_module, "AsyncEngineArgs", FakeAsyncEngineArgsWithoutScheduler)
    monkeypatch.setattr(vllm_module, "AsyncLLMEngine", FakeAsyncLLMEngine)

    config = WorkerConfig(
        engine="vllm",
        vllm_max_num_batched_tokens=4096,
        vllm_max_num_seqs=64,
        vllm_swap_space=12.5,
        vllm_enforce_eager=True,
        vllm_num_scheduler_steps=6,
    )
    engine = vllm_module.VLLMEngine(config)

    await engine.load_model(ModelConfig(model_id="Qwen/Qwen2.5-7B-Instruct"))

    assert FakeAsyncEngineArgsWithoutScheduler.last_kwargs is not None
    assert FakeAsyncEngineArgsWithoutScheduler.last_kwargs["max_num_batched_tokens"] == 4096
    assert FakeAsyncEngineArgsWithoutScheduler.last_kwargs["max_num_seqs"] == 64
    assert FakeAsyncEngineArgsWithoutScheduler.last_kwargs["swap_space"] == 12.5
    assert FakeAsyncEngineArgsWithoutScheduler.last_kwargs["enforce_eager"] is True
    assert "num_scheduler_steps" not in FakeAsyncEngineArgsWithoutScheduler.last_kwargs


@pytest.mark.asyncio
async def test_vllm_engine_defers_tokenizer_load_until_prompt_build(monkeypatch):
    monkeypatch.setattr(vllm_module, "VLLM_AVAILABLE", True)
    monkeypatch.setattr(vllm_module, "AsyncEngineArgs", FakeAsyncEngineArgs)
    monkeypatch.setattr(vllm_module, "AsyncLLMEngine", FakeAsyncLLMEngine)

    created_tokenizers: list[str] = []

    class FakeTokenizer:
        def apply_chat_template(self, messages, *, tokenize, add_generation_prompt):
            assert tokenize is False
            assert add_generation_prompt is True
            return f"templated:{messages[-1]['content']}"

    class FakeAutoTokenizer:
        @staticmethod
        def from_pretrained(model_path, trust_remote_code):
            assert trust_remote_code is True
            created_tokenizers.append(model_path)
            return FakeTokenizer()

    monkeypatch.setitem(sys.modules, "transformers", SimpleNamespace(AutoTokenizer=FakeAutoTokenizer))

    config = WorkerConfig(engine="vllm")
    engine = vllm_module.VLLMEngine(config)

    await engine.load_model(ModelConfig(model_id="Qwen/Qwen2.5-7B-Instruct"))

    assert created_tokenizers == []
    prompt = engine._build_prompt(
        SimpleNamespace(
            model_id="Qwen/Qwen2.5-7B-Instruct",
            messages=[SimpleNamespace(role=SimpleNamespace(value="user"), content="Hello")],
        )
    )
    assert created_tokenizers == ["Qwen/Qwen2.5-7B-Instruct"]
    assert prompt == "templated:Hello"


@pytest.mark.asyncio
async def test_vllm_tokenizer_respects_trust_remote_code_setting(monkeypatch):
    monkeypatch.setattr(vllm_module, "VLLM_AVAILABLE", True)
    monkeypatch.setattr(vllm_module, "AsyncEngineArgs", FakeAsyncEngineArgs)
    monkeypatch.setattr(vllm_module, "AsyncLLMEngine", FakeAsyncLLMEngine)

    observed_values: list[bool] = []

    class FakeTokenizer:
        def apply_chat_template(self, messages, *, tokenize, add_generation_prompt):
            return f"templated:{messages[-1]['content']}"

    class FakeAutoTokenizer:
        @staticmethod
        def from_pretrained(model_path, trust_remote_code):
            del model_path
            observed_values.append(trust_remote_code)
            return FakeTokenizer()

    monkeypatch.setitem(sys.modules, "transformers", SimpleNamespace(AutoTokenizer=FakeAutoTokenizer))

    engine = vllm_module.VLLMEngine(WorkerConfig(engine="vllm", trust_remote_code=False))
    await engine.load_model(ModelConfig(model_id="Qwen/Qwen2.5-7B-Instruct"))

    prompt = engine._build_prompt(
        SimpleNamespace(
            model_id="Qwen/Qwen2.5-7B-Instruct",
            messages=[SimpleNamespace(role=SimpleNamespace(value="user"), content="Hello")],
        )
    )

    assert observed_values == [False]
    assert prompt == "templated:Hello"


@pytest.mark.asyncio
async def test_vllm_engine_records_detailed_load_stages(monkeypatch):
    monkeypatch.setattr(vllm_module, "VLLM_AVAILABLE", True)
    monkeypatch.setattr(vllm_module, "AsyncEngineArgs", FakeAsyncEngineArgs)
    monkeypatch.setattr(vllm_module, "AsyncLLMEngine", FakeAsyncLLMEngine)

    recorded_stages: list[str] = []
    engine = vllm_module.VLLMEngine(WorkerConfig(engine="vllm"))
    engine.set_startup_stage_recorder(recorded_stages.append)

    await engine.load_model(ModelConfig(model_id="Qwen/Qwen2.5-7B-Instruct"))

    assert "vllm_engine_init_started" in recorded_stages
    assert "vllm_engine_init_finished" in recorded_stages
    assert "tokenizer_load_deferred" in recorded_stages


@pytest.mark.asyncio
async def test_vllm_engine_warm_model_runtime_loads_deferred_tokenizer(monkeypatch):
    monkeypatch.setattr(vllm_module, "VLLM_AVAILABLE", True)
    monkeypatch.setattr(vllm_module, "AsyncEngineArgs", FakeAsyncEngineArgs)
    monkeypatch.setattr(vllm_module, "AsyncLLMEngine", FakeAsyncLLMEngine)

    created_tokenizers: list[str] = []
    recorded_stages: list[str] = []

    class FakeAutoTokenizer:
        @staticmethod
        def from_pretrained(model_path, trust_remote_code):
            assert trust_remote_code is True
            created_tokenizers.append(model_path)
            return object()

    monkeypatch.setitem(sys.modules, "transformers", SimpleNamespace(AutoTokenizer=FakeAutoTokenizer))

    engine = vllm_module.VLLMEngine(WorkerConfig(engine="vllm"))
    engine.set_startup_stage_recorder(recorded_stages.append)

    await engine.load_model(ModelConfig(model_id="Qwen/Qwen2.5-7B-Instruct"))
    await engine.warm_model_runtime("Qwen/Qwen2.5-7B-Instruct")

    assert created_tokenizers == ["Qwen/Qwen2.5-7B-Instruct"]
    assert "tokenizer_warmup_started" in recorded_stages
    assert "tokenizer_warmup_finished" in recorded_stages


@pytest.mark.asyncio
async def test_vllm_engine_records_cache_probe_metadata(monkeypatch, tmp_path):
    monkeypatch.setattr(vllm_module, "VLLM_AVAILABLE", True)
    monkeypatch.setattr(vllm_module, "AsyncEngineArgs", FakeAsyncEngineArgs)
    monkeypatch.setattr(vllm_module, "AsyncLLMEngine", FakeAsyncLLMEngine)

    hub_cache = tmp_path / "huggingface" / "hub"
    snapshot_dir = (
        hub_cache
        / "models--Qwen--Qwen2.5-7B-Instruct"
        / "snapshots"
        / "snapshot-1"
    )
    snapshot_dir.mkdir(parents=True)
    (snapshot_dir / "config.json").write_text("{}", encoding="utf-8")
    (snapshot_dir / "tokenizer_config.json").write_text("{}", encoding="utf-8")

    monkeypatch.setenv("HF_HOME", str(tmp_path / "huggingface"))
    monkeypatch.setenv("HUGGINGFACE_HUB_CACHE", str(hub_cache))
    monkeypatch.setenv("TRANSFORMERS_CACHE", str(hub_cache))
    monkeypatch.setenv("TORCH_HOME", str(tmp_path / "torch"))

    recorded_metadata: list[tuple[str, dict[str, object]]] = []
    engine = vllm_module.VLLMEngine(WorkerConfig(engine="vllm"))
    engine.set_startup_metadata_recorder(lambda key, payload: recorded_metadata.append((key, payload)))

    await engine.load_model(ModelConfig(model_id="Qwen/Qwen2.5-7B-Instruct"))

    assert recorded_metadata
    key, payload = recorded_metadata[-1]
    assert key == "model_loads"
    model_probe = payload["Qwen/Qwen2.5-7B-Instruct"]
    assert model_probe["model_source"] == "huggingface_repo"
    assert model_probe["inferred_hf_repo_cache_exists"] is True
    assert model_probe["inferred_hf_snapshot_count"] == 1
    assert model_probe["inferred_latest_snapshot_has_config_json"] is True
    assert model_probe["inferred_latest_snapshot_has_tokenizer_config_json"] is True
