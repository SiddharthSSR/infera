"""Tests for engine registry, config normalization, and adapters."""

from __future__ import annotations

import asyncio
from types import SimpleNamespace

import pytest

from infera_worker.config import ModelConfig, WorkerConfig
from infera_worker.engine import MockEngine, create_engine, get_engine_definition, list_engine_definitions
from infera_worker.engines import sglang_engine as sglang_module
from infera_worker.engines import tensorrt_llm_engine as tensorrt_module
from infera_worker.types import FinishReason, InferenceRequest, Message, Role


@pytest.fixture
def mock_engine():
    config = WorkerConfig(engine="mock")
    return MockEngine(config)


@pytest.fixture
def sample_request():
    return InferenceRequest(
        request_id="test-req-123",
        model_id="test-model",
        messages=[Message(role=Role.USER, content="Hello, how are you?")],
    )


class TestMockEngine:
    @pytest.mark.asyncio
    async def test_load_model(self, mock_engine):
        config = ModelConfig(model_id="test-model")
        model = await mock_engine.load_model(config)

        assert model.model_id == "test-model"
        assert model.version == "1.0.0"
        assert model.memory_bytes > 0

    @pytest.mark.asyncio
    async def test_unload_model(self, mock_engine):
        await mock_engine.load_model(ModelConfig(model_id="to-unload"))

        assert mock_engine.is_model_loaded("to-unload")

        result = await mock_engine.unload_model("to-unload")

        assert result is True
        assert not mock_engine.is_model_loaded("to-unload")

    @pytest.mark.asyncio
    async def test_infer(self, mock_engine, sample_request):
        await mock_engine.load_model(ModelConfig(model_id="test-model"))

        response = await mock_engine.infer(sample_request)

        assert response.request_id == sample_request.request_id
        assert response.model_id == sample_request.model_id
        assert len(response.choices) == 1
        assert response.choices[0].message.role == Role.ASSISTANT
        assert response.choices[0].finish_reason == FinishReason.STOP
        assert "mock response" in response.choices[0].message.content.lower()

    @pytest.mark.asyncio
    async def test_infer_stream(self, mock_engine, sample_request):
        await mock_engine.load_model(ModelConfig(model_id="test-model"))

        chunks = []
        async for chunk in mock_engine.infer_stream(sample_request):
            chunks.append(chunk)

        assert chunks
        assert chunks[-1].finish_reason == FinishReason.STOP
        assert chunks[-1].usage is not None

    def test_get_memory_usage(self, mock_engine):
        used, total = mock_engine.get_memory_usage()

        assert used >= 0
        assert total == 16 * 1024 * 1024 * 1024


class TestEngineRegistry:
    def test_worker_config_normalizes_engine_aliases(self):
        assert WorkerConfig(engine="tensorrt-llm").engine == "tensorrt_llm"
        assert WorkerConfig(engine="TRTLLM").engine == "tensorrt_llm"
        assert WorkerConfig(engine="sglang").engine == "sglang"

    def test_worker_config_rejects_unknown_engine(self):
        with pytest.raises(ValueError, match="Unsupported inference engine"):
            WorkerConfig(engine="unknown")

    def test_list_engine_definitions_includes_builtin_engines(self):
        engine_ids = {definition.engine_id for definition in list_engine_definitions()}

        assert {"mock", "vllm", "sglang", "tensorrt_llm"} <= engine_ids

    def test_create_mock_engine(self):
        engine = create_engine(WorkerConfig(engine="mock"))
        assert isinstance(engine, MockEngine)

    def test_get_engine_definition_for_vllm(self):
        definition = get_engine_definition("vllm")
        assert definition.engine_id == "vllm"
        assert definition.display_name == "vLLM"
        assert definition.capabilities.supports_streaming is True


class TestSGLangEngine:
    @pytest.mark.asyncio
    async def test_sglang_engine_loads_model_and_streams(self, monkeypatch):
        created_kwargs: dict[str, object] = {}

        class FakeSGLangEngine:
            def __init__(self, **kwargs):
                created_kwargs.update(kwargs)
                self.server_args = SimpleNamespace(context_length=16384)

            async def async_generate(self, prompts, sampling_params):
                assert prompts == ["templated:Hello, how are you?"]
                assert sampling_params["temperature"] == 1.0
                return [{"text": "SGLang response", "meta_info": {"prompt_tokens": 5, "completion_tokens": 3}}]

            def shutdown(self):
                return None

        async def fake_async_stream_and_merge(engine, prompt, sampling_params):
            assert prompt == "templated:Hello, how are you?"
            assert sampling_params["temperature"] == 1.0
            yield "SGLang "
            yield "stream"

        monkeypatch.setattr(sglang_module, "SGLANG_AVAILABLE", True)
        monkeypatch.setattr(sglang_module, "sgl", SimpleNamespace(Engine=FakeSGLangEngine))
        monkeypatch.setattr(sglang_module, "async_stream_and_merge", fake_async_stream_and_merge)

        class FakeTokenizer:
            def apply_chat_template(self, messages, *, tokenize, add_generation_prompt):
                assert tokenize is False
                assert add_generation_prompt is True
                return f"templated:{messages[-1]['content']}"

        monkeypatch.setattr(sglang_module.TokenizerPromptEngine, "_get_tokenizer", lambda _self, _model_id: FakeTokenizer())

        engine = sglang_module.SGLangEngine(
            WorkerConfig(
                engine="sglang",
                sglang_tp_size=2,
                sglang_context_length=16384,
                sglang_chunked_prefill_size=4096,
            )
        )

        loaded = await engine.load_model(ModelConfig(model_id="test-model"))
        response = await engine.infer(
            InferenceRequest(
                request_id="sglang-request",
                model_id="test-model",
                messages=[Message(role=Role.USER, content="Hello, how are you?")],
            )
        )
        streamed = [chunk async for chunk in engine.infer_stream(make_sample_request())]

        assert loaded.max_sequence_length == 16384
        assert created_kwargs["tp_size"] == 2
        assert created_kwargs["context_length"] == 16384
        assert created_kwargs["chunked_prefill_size"] == 4096
        assert response.choices[0].message.content == "SGLang response"
        assert response.usage.prompt_tokens == 5
        assert response.usage.completion_tokens == 3
        assert streamed[-1].finish_reason == FinishReason.STOP

    def test_sglang_engine_raises_when_dependency_missing(self, monkeypatch):
        monkeypatch.setattr(sglang_module, "SGLANG_AVAILABLE", False)
        with pytest.raises(ImportError, match="SGLang is not installed"):
            sglang_module.SGLangEngine(WorkerConfig(engine="sglang"))


class TestTensorRTLLMEngine:
    @pytest.mark.asyncio
    async def test_tensorrt_engine_loads_model_and_streams(self, monkeypatch):
        class FakeBuildConfig:
            def __init__(self, **kwargs):
                self.kwargs = kwargs

        class FakeKvCacheConfig:
            def __init__(self, **kwargs):
                self.kwargs = kwargs

        class FakeSamplingParams:
            def __init__(self, **kwargs):
                self.kwargs = kwargs

        created_kwargs: dict[str, object] = {}

        class FakeOutput:
            def __init__(self, text, token_ids=None, finish_reason="stop", finished=False):
                self.prompt_tokens = 7
                self.outputs = [SimpleNamespace(text=text, token_ids=token_ids or [1, 2, 3], finish_reason=finish_reason)]
                self.finished = finished

        class FakeLLM:
            def __init__(self, **kwargs):
                created_kwargs.update(kwargs)
                self.config = SimpleNamespace(max_seq_len=12288)

            def generate(self, prompts, sampling_params):
                assert prompts == ["templated:Hello, how are you?"]
                assert isinstance(sampling_params, FakeSamplingParams)
                return [FakeOutput("TensorRT response")]

            async def generate_async(self, prompt, sampling_params, streaming):
                assert prompt == "templated:Hello, how are you?"
                assert streaming is True
                yield FakeOutput("Tensor", [1], finish_reason=None, finished=False)
                yield FakeOutput("TensorRT", [1, 2], finish_reason=None, finished=False)
                yield FakeOutput("TensorRT stream", [1, 2, 3], finish_reason="stop", finished=True)

            def shutdown(self):
                return None

        monkeypatch.setattr(tensorrt_module, "TENSORRT_LLM_AVAILABLE", True)
        monkeypatch.setattr(tensorrt_module, "LLM", FakeLLM)
        monkeypatch.setattr(tensorrt_module, "SamplingParams", FakeSamplingParams)
        monkeypatch.setattr(tensorrt_module, "BuildConfig", FakeBuildConfig)
        monkeypatch.setattr(tensorrt_module, "KvCacheConfig", FakeKvCacheConfig)

        class FakeTokenizer:
            def apply_chat_template(self, messages, *, tokenize, add_generation_prompt):
                return f"templated:{messages[-1]['content']}"

        monkeypatch.setattr(tensorrt_module.TokenizerPromptEngine, "_get_tokenizer", lambda _self, _model_id: FakeTokenizer())

        engine = tensorrt_module.TensorRTLLMEngine(
            WorkerConfig(
                engine="tensorrt_llm",
                tensorrt_llm_tensor_parallel_size=4,
                tensorrt_llm_max_batch_size=16,
                tensorrt_llm_max_num_tokens=4096,
                tensorrt_llm_kv_cache_free_gpu_memory_fraction=0.1,
            )
        )

        loaded = await engine.load_model(ModelConfig(model_id="test-model"))
        response = await engine.infer(
            InferenceRequest(
                request_id="tensorrt-request",
                model_id="test-model",
                messages=[Message(role=Role.USER, content="Hello, how are you?")],
            )
        )
        chunks = [chunk async for chunk in engine.infer_stream(make_sample_request())]

        assert loaded.max_sequence_length == 12288
        assert created_kwargs["tensor_parallel_size"] == 4
        assert isinstance(created_kwargs["build_config"], FakeBuildConfig)
        assert created_kwargs["build_config"].kwargs["max_batch_size"] == 16
        assert created_kwargs["build_config"].kwargs["max_num_tokens"] == 4096
        assert isinstance(created_kwargs["kv_cache_config"], FakeKvCacheConfig)
        assert response.choices[0].message.content == "TensorRT response"
        assert chunks[-1].finish_reason == FinishReason.STOP

    @pytest.mark.asyncio
    async def test_tensorrt_engine_rejects_pytorch_backend(self, monkeypatch):
        class FakeLLM:
            def __init__(self, **kwargs):
                del kwargs

        class FakeSamplingParams:
            def __init__(self, **kwargs):
                self.kwargs = kwargs

        monkeypatch.setattr(tensorrt_module, "TENSORRT_LLM_AVAILABLE", True)
        monkeypatch.setattr(tensorrt_module, "LLM", FakeLLM)
        monkeypatch.setattr(tensorrt_module, "SamplingParams", FakeSamplingParams)

        engine = tensorrt_module.TensorRTLLMEngine(
            WorkerConfig(
                engine="tensorrt_llm",
                tensorrt_llm_backend="pytorch",
            )
        )

        with pytest.raises(ValueError, match="only supports the TensorRT backend"):
            await engine.load_model(ModelConfig(model_id="test-model"))

    def test_tensorrt_engine_raises_when_dependency_missing(self, monkeypatch):
        monkeypatch.setattr(tensorrt_module, "TENSORRT_LLM_AVAILABLE", False)
        monkeypatch.setattr(tensorrt_module, "TENSORRT_LLM_IMPORT_ERROR", ImportError("libcuda.so.1: cannot open shared object file"))
        with pytest.raises(ImportError, match="TensorRT-LLM import failed"):
            tensorrt_module.TensorRTLLMEngine(WorkerConfig(engine="tensorrt_llm"))


def make_sample_request():
    return InferenceRequest(
        request_id="streaming-request",
        model_id="test-model",
        messages=[Message(role=Role.USER, content="Hello, how are you?")],
    )
