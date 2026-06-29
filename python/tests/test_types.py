"""Tests for infera_worker types."""

from datetime import datetime

from infera_worker.types import (
    Choice,
    FinishReason,
    InferenceRequest,
    InferenceResponse,
    LatencyStats,
    LoadedModel,
    Message,
    Role,
    TokenChunk,
    UsageStats,
    WorkerState,
    WorkerStats,
)


class TestMessage:
    def test_create_user_message(self):
        msg = Message(role=Role.USER, content="Hello")
        assert msg.role == Role.USER
        assert msg.content == "Hello"
        assert msg.name is None

    def test_create_assistant_message(self):
        msg = Message(role=Role.ASSISTANT, content="Hi there!")
        assert msg.role == Role.ASSISTANT
        assert msg.content == "Hi there!"

    def test_create_system_message(self):
        msg = Message(role=Role.SYSTEM, content="You are helpful.")
        assert msg.role == Role.SYSTEM
        assert msg.content == "You are helpful."

    def test_message_with_name(self):
        msg = Message(role=Role.USER, content="Test", name="alice")
        assert msg.name == "alice"


class TestRole:
    def test_role_values(self):
        assert Role.SYSTEM == "system"
        assert Role.USER == "user"
        assert Role.ASSISTANT == "assistant"


class TestFinishReason:
    def test_finish_reason_values(self):
        assert FinishReason.STOP == "stop"
        assert FinishReason.LENGTH == "length"
        assert FinishReason.CONTENT_FILTER == "content_filter"


class TestInferenceRequest:
    def test_create_request(self):
        messages = [
            Message(role=Role.USER, content="Hello")
        ]
        req = InferenceRequest(
            request_id="test-123",
            model_id="llama-3-8b",
            messages=messages,
        )
        assert req.request_id == "test-123"
        assert req.model_id == "llama-3-8b"
        assert len(req.messages) == 1

    def test_request_with_parameters(self):
        from infera_worker.types import InferenceParameters
        params = InferenceParameters(temperature=0.7, max_tokens=100, top_p=0.9)
        req = InferenceRequest(
            request_id="test-456",
            model_id="gpt-4",
            messages=[Message(role=Role.USER, content="Test")],
            parameters=params,
        )
        assert req.parameters.temperature == 0.7
        assert req.parameters.max_tokens == 100
        assert req.parameters.top_p == 0.9

    def test_token_estimate(self):
        req = InferenceRequest(
            request_id="test",
            model_id="test",
            messages=[
                Message(role=Role.USER, content="Hello world this is a test message")
            ],
        )
        # Rough estimate: ~4 chars per token
        estimate = req.token_estimate()
        assert estimate > 0
        assert estimate < 100  # Shouldn't be huge for this short message


class TestInferenceResponse:
    def test_create_response(self):
        resp = InferenceResponse(
            request_id="resp-123",
            model_id="llama-3-8b",
            choices=[
                Choice(
                    index=0,
                    message=Message(role=Role.ASSISTANT, content="Hello!"),
                    finish_reason=FinishReason.STOP,
                )
            ],
            usage=UsageStats(
                prompt_tokens=10,
                completion_tokens=5,
                total_tokens=15,
            ),
            latency=LatencyStats(
                queue_ms=10,
                inference_ms=100,
                total_ms=110,
                time_to_first_token_ms=50,
            ),
        )
        assert resp.request_id == "resp-123"
        assert len(resp.choices) == 1
        assert resp.choices[0].message.content == "Hello!"
        assert resp.usage.total_tokens == 15


class TestChoice:
    def test_create_choice(self):
        choice = Choice(
            index=0,
            message=Message(role=Role.ASSISTANT, content="Test"),
            finish_reason=FinishReason.STOP,
        )
        assert choice.index == 0
        assert choice.finish_reason == FinishReason.STOP


class TestTokenChunk:
    def test_create_chunk(self):
        chunk = TokenChunk(
            request_id="chunk-123",
            index=5,
            delta="Hello ",
        )
        assert chunk.request_id == "chunk-123"
        assert chunk.index == 5
        assert chunk.delta == "Hello "
        assert chunk.finish_reason is None

    def test_final_chunk(self):
        chunk = TokenChunk(
            request_id="chunk-456",
            index=10,
            delta="",
            finish_reason=FinishReason.STOP,
            usage=UsageStats(
                prompt_tokens=10,
                completion_tokens=20,
                total_tokens=30,
            ),
        )
        assert chunk.is_final()
        assert chunk.usage is not None
        assert chunk.usage.total_tokens == 30

    def test_non_final_chunk(self):
        chunk = TokenChunk(
            request_id="chunk-789",
            index=0,
            delta="Hi",
        )
        assert not chunk.is_final()


class TestUsageStats:
    def test_create_usage(self):
        usage = UsageStats(
            prompt_tokens=100,
            completion_tokens=50,
            total_tokens=150,
        )
        assert usage.prompt_tokens == 100
        assert usage.completion_tokens == 50
        assert usage.total_tokens == 150

    def test_total_equals_sum(self):
        usage = UsageStats(
            prompt_tokens=25,
            completion_tokens=75,
            total_tokens=100,
        )
        assert usage.total_tokens == usage.prompt_tokens + usage.completion_tokens


class TestLatencyStats:
    def test_create_latency(self):
        latency = LatencyStats(
            queue_ms=5,
            inference_ms=200,
            total_ms=205,
            time_to_first_token_ms=50,
        )
        assert latency.queue_ms == 5
        assert latency.inference_ms == 200
        assert latency.total_ms == 205
        assert latency.time_to_first_token_ms == 50


class TestLoadedModel:
    def test_create_loaded_model(self):
        model = LoadedModel(
            model_id="llama-3-8b",
            version="1.0.0",
            loaded_at=datetime.now(),
            memory_bytes=8 * 1024 * 1024 * 1024,  # 8GB
            max_batch_size=32,
            max_sequence_length=4096,
        )
        assert model.model_id == "llama-3-8b"
        assert model.version == "1.0.0"
        assert model.memory_bytes == 8 * 1024 * 1024 * 1024
        assert model.max_batch_size == 32


class TestWorkerStats:
    def test_create_stats(self):
        stats = WorkerStats(
            queue_depth=5,
            active_requests=3,
            gpu_utilization=0.75,
            memory_used_bytes=4 * 1024 * 1024 * 1024,
            memory_total_bytes=8 * 1024 * 1024 * 1024,
            requests_per_second=10.5,
            avg_latency_ms=150.0,
            p50_latency_ms=120.0,
            p99_latency_ms=500.0,
            error_rate=0.01,
        )
        assert stats.queue_depth == 5
        assert stats.gpu_utilization == 0.75
        assert stats.error_rate == 0.01


class TestWorkerState:
    def test_worker_states(self):
        assert WorkerState.INITIALIZING == "initializing"
        assert WorkerState.READY == "ready"
        assert WorkerState.BUSY == "busy"
        assert WorkerState.DRAINING == "draining"
        assert WorkerState.SHUTTING_DOWN == "shutting_down"
        assert WorkerState.ERROR == "error"
