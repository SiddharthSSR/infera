"""Tests for the mock inference engine."""


import pytest

from infera_worker.config import ModelConfig, WorkerConfig
from infera_worker.engine import MockEngine, create_engine
from infera_worker.types import (
    FinishReason,
    InferenceRequest,
    Message,
    Role,
)


@pytest.fixture
def mock_engine():
    config = WorkerConfig(engine="mock")
    return MockEngine(config)


@pytest.fixture
def sample_request():
    return InferenceRequest(
        request_id="test-req-123",
        model_id="test-model",
        messages=[
            Message(role=Role.USER, content="Hello, how are you?")
        ],
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
        # Load first
        config = ModelConfig(model_id="to-unload")
        await mock_engine.load_model(config)

        assert mock_engine.is_model_loaded("to-unload")

        # Unload
        result = await mock_engine.unload_model("to-unload")

        assert result is True
        assert not mock_engine.is_model_loaded("to-unload")

    @pytest.mark.asyncio
    async def test_unload_nonexistent_model(self, mock_engine):
        result = await mock_engine.unload_model("nonexistent")
        assert result is False

    def test_is_model_loaded(self, mock_engine):
        assert not mock_engine.is_model_loaded("not-loaded")

    @pytest.mark.asyncio
    async def test_is_model_loaded_after_load(self, mock_engine):
        config = ModelConfig(model_id="loaded-model")
        await mock_engine.load_model(config)

        assert mock_engine.is_model_loaded("loaded-model")

    @pytest.mark.asyncio
    async def test_get_loaded_models(self, mock_engine):
        # Initially empty
        assert len(mock_engine.get_loaded_models()) == 0

        # Load some models
        await mock_engine.load_model(ModelConfig(model_id="model-1"))
        await mock_engine.load_model(ModelConfig(model_id="model-2"))

        models = mock_engine.get_loaded_models()
        assert len(models) == 2
        model_ids = [m.model_id for m in models]
        assert "model-1" in model_ids
        assert "model-2" in model_ids

    @pytest.mark.asyncio
    async def test_infer(self, mock_engine, sample_request):
        # Load model first
        await mock_engine.load_model(ModelConfig(model_id="test-model"))

        response = await mock_engine.infer(sample_request)

        assert response.request_id == sample_request.request_id
        assert response.model_id == sample_request.model_id
        assert len(response.choices) == 1
        assert response.choices[0].message.role == Role.ASSISTANT
        assert response.choices[0].finish_reason == FinishReason.STOP
        assert "mock response" in response.choices[0].message.content.lower()

    @pytest.mark.asyncio
    async def test_infer_usage_stats(self, mock_engine, sample_request):
        await mock_engine.load_model(ModelConfig(model_id="test-model"))

        response = await mock_engine.infer(sample_request)

        assert response.usage.prompt_tokens > 0
        assert response.usage.completion_tokens > 0
        assert response.usage.total_tokens == response.usage.prompt_tokens + response.usage.completion_tokens

    @pytest.mark.asyncio
    async def test_infer_latency_stats(self, mock_engine, sample_request):
        await mock_engine.load_model(ModelConfig(model_id="test-model"))

        response = await mock_engine.infer(sample_request)

        assert response.latency.inference_ms > 0
        assert response.latency.total_ms > 0

    @pytest.mark.asyncio
    async def test_infer_stream(self, mock_engine, sample_request):
        await mock_engine.load_model(ModelConfig(model_id="test-model"))

        chunks = []
        async for chunk in mock_engine.infer_stream(sample_request):
            chunks.append(chunk)

        assert len(chunks) > 0

        # Check first chunk
        assert chunks[0].request_id == sample_request.request_id
        assert chunks[0].delta != ""

        # Check last chunk
        last_chunk = chunks[-1]
        assert last_chunk.finish_reason == FinishReason.STOP
        assert last_chunk.usage is not None

    @pytest.mark.asyncio
    async def test_infer_stream_content(self, mock_engine, sample_request):
        await mock_engine.load_model(ModelConfig(model_id="test-model"))

        content = ""
        async for chunk in mock_engine.infer_stream(sample_request):
            content += chunk.delta

        assert "mock" in content.lower()
        assert "response" in content.lower()

    @pytest.mark.asyncio
    async def test_cancel(self, mock_engine, sample_request):
        await mock_engine.load_model(ModelConfig(model_id="test-model"))

        # Start a request
        mock_engine.active_requests.add(sample_request.request_id)

        # Cancel it
        result = await mock_engine.cancel(sample_request.request_id)

        assert result is True

    @pytest.mark.asyncio
    async def test_cancel_nonexistent(self, mock_engine):
        result = await mock_engine.cancel("nonexistent-request")
        assert result is False

    def test_get_memory_usage(self, mock_engine):
        used, total = mock_engine.get_memory_usage()

        assert used >= 0
        assert total > 0
        assert total == 16 * 1024 * 1024 * 1024  # 16GB mock total

    @pytest.mark.asyncio
    async def test_memory_usage_after_loading(self, mock_engine):
        # Load a model
        await mock_engine.load_model(ModelConfig(model_id="memory-test"))

        used, total = mock_engine.get_memory_usage()

        assert used > 0  # Should use some memory now


class TestCreateEngine:
    def test_create_mock_engine(self):
        config = WorkerConfig(engine="mock")
        engine = create_engine(config)

        assert isinstance(engine, MockEngine)

    def test_create_unknown_engine(self):
        config = WorkerConfig(engine="unknown")

        with pytest.raises(ValueError, match="Unknown engine"):
            create_engine(config)

    def test_create_vllm_engine_import_error(self):
        """Test that vllm engine import is attempted."""
        config = WorkerConfig(engine="vllm")

        # This will either work (if vllm is installed) or raise ImportError
        # We're mainly testing that the code path exists
        try:
            create_engine(config)
            # If vllm is installed, this should succeed
        except ImportError:
            # Expected if vllm is not installed
            pass
        except Exception:
            # Some other error from vllm initialization
            pass
