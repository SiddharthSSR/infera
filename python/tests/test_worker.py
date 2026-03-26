"""Tests for the main worker."""

import asyncio
from types import SimpleNamespace
import pytest

from infera_worker import worker as worker_module
from infera_worker.worker import Worker
from infera_worker.config import WorkerConfig, ModelConfig
from infera_worker.types import (
    InferenceRequest,
    Message,
    Role,
    WorkerState,
)


@pytest.fixture
def worker():
    config = WorkerConfig(engine="mock")
    return Worker(config)


@pytest.fixture
def sample_request():
    return InferenceRequest(
        request_id="worker-test-123",
        model_id="test-model",
        messages=[Message(role=Role.USER, content="Hello!")],
    )


class TestWorker:
    def test_create_worker(self, worker):
        assert worker is not None
        assert worker.state == WorkerState.INITIALIZING
        assert worker.worker_id is not None

    def test_worker_id_generation(self):
        config = WorkerConfig(engine="mock")
        worker1 = Worker(config)
        worker2 = Worker(config)
        
        # Each worker should have unique ID
        assert worker1.worker_id != worker2.worker_id

    def test_worker_custom_id(self):
        config = WorkerConfig(engine="mock", worker_id="custom-worker-id")
        worker = Worker(config)
        
        assert worker.worker_id == "custom-worker-id"

    @pytest.mark.asyncio
    async def test_start_worker(self, worker):
        await worker.start()
        
        assert worker.state == WorkerState.READY
        assert worker.engine is not None
        startup = worker.get_startup_status()
        assert "model_load_started" in startup["stages"]
        assert "engine_create_started" in startup["stages"]
        assert "engine_create_finished" in startup["stages"]
        assert "model_load_finished" in startup["stages"]
        assert "worker_ready" in startup["stages"]
        assert startup["durations_ms"]["worker_ready"] >= 0

    @pytest.mark.asyncio
    async def test_start_worker_records_preload_stages(self):
        worker = Worker(WorkerConfig(engine="mock", preload_models=["test-model"]))

        await worker.start()

        startup = worker.get_startup_status()
        assert "preload_model_started" in startup["stages"]
        assert "preload_model_finished" in startup["stages"]

    @pytest.mark.asyncio
    async def test_start_worker_schedules_post_ready_warmup(self, monkeypatch):
        warmed_models: list[str] = []

        class FakeEngine:
            def __init__(self):
                self.loaded_models = []

            def set_startup_stage_recorder(self, recorder):
                self.recorder = recorder

            def set_startup_metadata_recorder(self, recorder):
                self.metadata_recorder = recorder

            async def load_model(self, config):
                self.metadata_recorder("model_loads", {config.model_id: {"model_source": "mock"}})
                model = SimpleNamespace(model_id=config.model_id)
                self.loaded_models.append(model)
                return SimpleNamespace(
                    model_id=config.model_id,
                    version="1.0.0",
                    loaded_at=None,
                    memory_bytes=1,
                    max_batch_size=config.max_batch_size,
                    max_sequence_length=config.max_sequence_length,
                )

            async def unload_model(self, model_id):
                return True

            def is_model_loaded(self, model_id):
                return True

            def get_loaded_models(self):
                return self.loaded_models

            async def infer(self, request):
                raise NotImplementedError

            async def infer_stream(self, request):
                raise NotImplementedError

            async def cancel(self, request_id):
                return False

            def get_memory_usage(self):
                return (0, 0)

            async def warm_model_runtime(self, model_id):
                warmed_models.append(model_id)

        monkeypatch.setattr(worker_module, "create_engine", lambda _config: FakeEngine())

        worker = Worker(WorkerConfig(engine="mock", preload_models=["test-model"]))
        await worker.start()
        await asyncio.sleep(0)

        assert warmed_models == ["test-model"]
        assert worker.get_startup_status()["metadata"]["model_loads"]["test-model"]["model_source"] == "mock"

    @pytest.mark.asyncio
    async def test_stop_worker(self, worker):
        await worker.start()
        await worker.stop()
        
        # State should be SHUTTING_DOWN at the end

    @pytest.mark.asyncio
    async def test_stop_graceful(self, worker):
        await worker.start()
        await worker.stop(graceful=True)

    @pytest.mark.asyncio
    async def test_stop_immediate(self, worker):
        await worker.start()
        await worker.stop(graceful=False)

    @pytest.mark.asyncio
    async def test_load_model(self, worker):
        await worker.start()
        
        model = await worker.load_model(ModelConfig(model_id="test-model"))
        
        assert model.model_id == "test-model"
        assert model in worker.get_loaded_models()

    @pytest.mark.asyncio
    async def test_unload_model(self, worker):
        await worker.start()
        await worker.load_model(ModelConfig(model_id="to-unload"))
        
        result = await worker.unload_model("to-unload")
        
        assert result is True
        assert len(worker.get_loaded_models()) == 0

    @pytest.mark.asyncio
    async def test_load_model_serializes_duplicate_loads(self):
        class FakeEngine:
            def __init__(self):
                self.loaded = {}
                self.calls = 0

            async def load_model(self, config):
                self.calls += 1
                await asyncio.sleep(0.01)
                model = SimpleNamespace(
                    model_id=config.model_id,
                    version="1.0.0",
                    loaded_at=None,
                    memory_bytes=1,
                    max_batch_size=config.max_batch_size,
                    max_sequence_length=config.max_sequence_length,
                )
                self.loaded[config.model_id] = model
                return model

            async def unload_model(self, model_id):
                self.loaded.pop(model_id, None)
                return True

            def is_model_loaded(self, model_id):
                return model_id in self.loaded

            def get_loaded_models(self):
                return list(self.loaded.values())

            async def infer(self, request):
                raise NotImplementedError

            async def infer_stream(self, request):
                raise NotImplementedError

            async def cancel(self, request_id):
                return False

            def get_memory_usage(self):
                return (0, 0)

        worker = Worker(WorkerConfig(engine="mock"))
        worker.engine = FakeEngine()
        worker.state = WorkerState.READY

        first, second = await asyncio.gather(
            worker.load_model(ModelConfig(model_id="shared-model")),
            worker.load_model(ModelConfig(model_id="shared-model")),
        )

        assert first.model_id == "shared-model"
        assert second.model_id == "shared-model"
        assert worker.engine.calls == 1

    @pytest.mark.asyncio
    async def test_unload_model_waits_for_inflight_requests(self):
        class FakeEngine:
            def __init__(self):
                self.loaded = {"test-model": SimpleNamespace(model_id="test-model")}
                self.unloaded = []

            async def load_model(self, config):
                raise NotImplementedError

            async def unload_model(self, model_id):
                self.unloaded.append(model_id)
                return True

            def is_model_loaded(self, model_id):
                return model_id in self.loaded

            def get_loaded_models(self):
                return [SimpleNamespace(model_id="test-model")]

            async def infer(self, request):
                raise NotImplementedError

            async def infer_stream(self, request):
                raise NotImplementedError

            async def cancel(self, request_id):
                return False

            def get_memory_usage(self):
                return (0, 0)

        worker = Worker(WorkerConfig(engine="mock"))
        worker.engine = FakeEngine()
        worker.state = WorkerState.READY
        worker._active_requests = 1
        worker._all_requests_idle.clear()

        async def release_idle():
            await asyncio.sleep(0.01)
            worker._active_requests = 0
            worker._all_requests_idle.set()

        release_task = asyncio.create_task(release_idle())
        try:
            result = await worker.unload_model("test-model")
        finally:
            await release_task

        assert result is True
        assert worker.engine.unloaded == ["test-model"]

    @pytest.mark.asyncio
    async def test_get_loaded_models_empty(self, worker):
        await worker.start()
        
        models = worker.get_loaded_models()
        
        assert len(models) == 0

    @pytest.mark.asyncio
    async def test_get_loaded_models(self, worker):
        await worker.start()
        await worker.load_model(ModelConfig(model_id="model-1"))
        await worker.load_model(ModelConfig(model_id="model-2"))
        
        models = worker.get_loaded_models()
        
        assert len(models) == 2

    @pytest.mark.asyncio
    async def test_infer(self, worker, sample_request):
        await worker.start()
        await worker.load_model(ModelConfig(model_id="test-model"))
        
        response = await worker.infer(sample_request)
        
        assert response.request_id == sample_request.request_id
        assert len(response.choices) > 0

    @pytest.mark.asyncio
    async def test_infer_not_initialized(self, worker, sample_request):
        # Don't start the worker
        with pytest.raises(RuntimeError, match="not ready"):
            await worker.infer(sample_request)

    @pytest.mark.asyncio
    async def test_infer_model_not_loaded(self, worker, sample_request):
        await worker.start()
        # Don't load the model
        
        with pytest.raises(ValueError, match="not loaded"):
            await worker.infer(sample_request)

    @pytest.mark.asyncio
    async def test_infer_stream(self, worker, sample_request):
        await worker.start()
        await worker.load_model(ModelConfig(model_id="test-model"))
        
        chunks = []
        async for chunk in worker.infer_stream(sample_request):
            chunks.append(chunk)
        
        assert len(chunks) > 0
        # Last chunk should be final
        assert chunks[-1].is_final()

    @pytest.mark.asyncio
    async def test_cancel(self, worker):
        await worker.start()
        
        result = await worker.cancel("some-request-id")
        
        # Mock engine should handle this gracefully
        assert isinstance(result, bool)

    @pytest.mark.asyncio
    async def test_get_stats(self, worker):
        await worker.start()
        
        stats = worker.get_stats()
        
        assert stats.queue_depth >= 0
        assert stats.active_requests >= 0
        assert stats.memory_total_bytes >= 0

    @pytest.mark.asyncio
    async def test_get_stats_prefers_runtime_or_engine_memory(self, monkeypatch):
        worker = Worker(WorkerConfig(engine="mock"))
        await worker.start()

        monkeypatch.setattr(worker, "_get_gpu_memory_usage", lambda: (1024, 4096))

        class FakeEngine:
            def get_memory_usage(self):
                return (2048, 4096)

        worker.engine = FakeEngine()

        stats = worker.get_stats()

        assert stats.memory_used_bytes == 2048
        assert stats.memory_total_bytes == 4096

    @pytest.mark.asyncio
    async def test_get_stats_after_requests(self, worker, sample_request):
        await worker.start()
        await worker.load_model(ModelConfig(model_id="test-model"))
        
        # Make some requests
        for _ in range(5):
            await worker.infer(sample_request)
        
        stats = worker.get_stats()
        
        assert stats.requests_per_second >= 0
        assert stats.avg_latency_ms >= 0

    @pytest.mark.asyncio
    async def test_get_stats_tracks_queued_and_active_requests(self):
        worker = Worker(WorkerConfig(engine="mock", max_concurrent_requests=1))
        await worker.start()
        await worker.load_model(ModelConfig(model_id="test-model"))

        original_infer = worker.engine.infer
        started = asyncio.Event()
        release = asyncio.Event()

        async def blocked_infer(request):
            started.set()
            await release.wait()
            return await original_infer(request)

        worker.engine.infer = blocked_infer

        first = InferenceRequest(
            request_id="req-1",
            model_id="test-model",
            messages=[Message(role=Role.USER, content="First")],
        )
        second = InferenceRequest(
            request_id="req-2",
            model_id="test-model",
            messages=[Message(role=Role.USER, content="Second")],
        )

        task1 = asyncio.create_task(worker.infer(first))
        await started.wait()

        task2 = asyncio.create_task(worker.infer(second))
        await asyncio.sleep(0.05)

        stats = worker.get_stats()

        assert stats.active_requests == 1
        assert stats.queue_depth == 1
        assert worker.get_state() == WorkerState.BUSY

        release.set()
        await asyncio.gather(task1, task2)

        final_stats = worker.get_stats()
        assert final_stats.active_requests == 0
        assert final_stats.queue_depth == 0
        assert worker.get_state() == WorkerState.READY

    def test_get_state(self, worker):
        assert worker.get_state() == WorkerState.INITIALIZING

    @pytest.mark.asyncio
    async def test_get_state_after_start(self, worker):
        await worker.start()
        assert worker.get_state() == WorkerState.READY

    @pytest.mark.asyncio
    async def test_preload_models(self):
        config = WorkerConfig(
            engine="mock",
            preload_models=["preload-model-1", "preload-model-2"],
        )
        worker = Worker(config)
        
        await worker.start()
        
        models = worker.get_loaded_models()
        model_ids = [m.model_id for m in models]
        
        assert "preload-model-1" in model_ids
        assert "preload-model-2" in model_ids
