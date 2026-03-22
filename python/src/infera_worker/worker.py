"""Main Infera Worker implementation."""

import asyncio
from collections import deque
from collections.abc import AsyncGenerator
from datetime import datetime
import uuid
import structlog

from .config import WorkerConfig, ModelConfig
from .types import (
    InferenceRequest,
    InferenceResponse,
    TokenChunk,
    LoadedModel,
    WorkerStats,
    WorkerState,
)
from .engine import InferenceEngine, create_engine

logger = structlog.get_logger()


class Worker:
    """Infera Worker - manages model inference."""

    def __init__(self, config: WorkerConfig) -> None:
        self.config = config
        self.worker_id = config.worker_id or str(uuid.uuid4())
        self.state = WorkerState.INITIALIZING
        self.engine: InferenceEngine | None = None
        self._startup_events: dict[str, datetime] = {
            "worker_created": datetime.now(),
        }
        
        # Stats tracking
        self._request_count = 0
        self._error_count = 0
        self._total_latency_ms = 0.0
        self._latencies: deque[float] = deque(maxlen=1000)
        self._started_at = datetime.now()
        self._active_requests = 0
        self._queued_requests = 0
        self._request_semaphore = asyncio.Semaphore(
            max(1, self.config.max_concurrent_requests)
        )
        self._all_requests_idle = asyncio.Event()
        self._all_requests_idle.set()
        
        # Lifecycle
        self._shutdown_event = asyncio.Event()

    async def start(self) -> None:
        """Initialize the worker."""
        logger.info("Starting worker", worker_id=self.worker_id, engine=self.config.engine)
        
        try:
            self.record_startup_stage("model_load_started")

            # Create inference engine
            self.engine = create_engine(self.config)
            
            # Preload models if configured
            for model_id in self.config.preload_models:
                logger.info("Preloading model", model_id=model_id)
                await self.load_model(ModelConfig(model_id=model_id))

            self.record_startup_stage("model_load_finished")
            self.state = WorkerState.READY
            self.record_startup_stage("worker_ready")
            logger.info("Worker ready", worker_id=self.worker_id)
            
        except Exception as e:
            logger.error("Failed to start worker", error=str(e))
            self.state = WorkerState.ERROR
            raise

    async def stop(self, graceful: bool = True) -> None:
        """Shutdown the worker."""
        logger.info("Stopping worker", worker_id=self.worker_id, graceful=graceful)
        
        if graceful:
            self.state = WorkerState.DRAINING
            try:
                await asyncio.wait_for(
                    self._all_requests_idle.wait(),
                    timeout=self.config.drain_timeout_s,
                )
            except asyncio.TimeoutError:
                logger.warning(
                    "Timed out waiting for in-flight requests to drain",
                    worker_id=self.worker_id,
                    active_requests=self._active_requests,
                    queued_requests=self._queued_requests,
                    drain_timeout_s=self.config.drain_timeout_s,
                )
        
        self.state = WorkerState.SHUTTING_DOWN
        self._shutdown_event.set()
        
        # Unload all models
        if self.engine:
            for model in self.engine.get_loaded_models():
                await self.engine.unload_model(model.model_id)
        
        logger.info("Worker stopped", worker_id=self.worker_id)

    def request_shutdown(self) -> None:
        """Signal the worker shutdown event."""
        self._shutdown_event.set()

    async def wait_for_shutdown(self) -> None:
        """Wait until shutdown is requested."""
        await self._shutdown_event.wait()

    async def load_model(self, config: ModelConfig) -> LoadedModel:
        """Load a model."""
        if self.engine is None:
            raise RuntimeError("Worker not ready: not initialized")
        
        logger.info("Loading model", model_id=config.model_id)
        model = await self.engine.load_model(config)
        logger.info("Model loaded", model_id=config.model_id, memory_bytes=model.memory_bytes)
        return model

    async def unload_model(self, model_id: str) -> bool:
        """Unload a model."""
        if self.engine is None:
            raise RuntimeError("Worker not ready: not initialized")
        
        logger.info("Unloading model", model_id=model_id)
        result = await self.engine.unload_model(model_id)
        if result:
            logger.info("Model unloaded", model_id=model_id)
        return result

    def get_loaded_models(self) -> list[LoadedModel]:
        """Get list of loaded models."""
        if self.engine is None:
            return []
        return self.engine.get_loaded_models()

    async def infer(self, request: InferenceRequest) -> InferenceResponse:
        """Process an inference request."""
        if self.engine is None:
            raise RuntimeError("Worker not ready: not initialized")
        
        if self.state not in {WorkerState.READY, WorkerState.BUSY}:
            raise RuntimeError(f"Worker not ready: {self.state}")
        
        if not self.engine.is_model_loaded(request.model_id):
            raise ValueError(f"Model {request.model_id} not loaded")
        
        start_time = datetime.now()
        await self._acquire_request_slot()
        self._request_count += 1
        
        try:
            response = await self.engine.infer(request)
            
            # Track latency
            latency_ms = (datetime.now() - start_time).total_seconds() * 1000
            self._record_latency(latency_ms)
            
            logger.debug(
                "Inference complete",
                request_id=request.request_id,
                latency_ms=latency_ms,
                tokens=response.usage.total_tokens,
            )
            
            return response
            
        except Exception as e:
            self._error_count += 1
            logger.error(
                "Inference failed",
                request_id=request.request_id,
                error=str(e),
            )
            raise
        finally:
            self._release_request_slot()

    async def infer_stream(
        self, request: InferenceRequest
    ) -> AsyncGenerator[TokenChunk, None]:
        """Process a streaming inference request."""
        if self.engine is None:
            raise RuntimeError("Worker not ready: not initialized")
        
        if self.state not in {WorkerState.READY, WorkerState.BUSY}:
            raise RuntimeError(f"Worker not ready: {self.state}")
        
        if not self.engine.is_model_loaded(request.model_id):
            raise ValueError(f"Model {request.model_id} not loaded")
        
        start_time = datetime.now()
        await self._acquire_request_slot()
        self._request_count += 1
        
        try:
            async for chunk in self.engine.infer_stream(request):
                yield chunk
                
                if chunk.is_final():
                    latency_ms = (datetime.now() - start_time).total_seconds() * 1000
                    self._record_latency(latency_ms)
                    logger.debug(
                        "Streaming inference complete",
                        request_id=request.request_id,
                        latency_ms=latency_ms,
                    )
                    
        except Exception as e:
            self._error_count += 1
            logger.error(
                "Streaming inference failed",
                request_id=request.request_id,
                error=str(e),
            )
            raise
        finally:
            self._release_request_slot()

    async def cancel(self, request_id: str) -> bool:
        """Cancel an in-progress request."""
        if self.engine is None:
            return False
        return await self.engine.cancel(request_id)

    def _get_gpu_utilization(self) -> float:
        """Get GPU compute utilization percentage (0-100).

        Tries pynvml first for actual GPU core utilization,
        falls back to memory-based estimation.
        """
        try:
            import pynvml
            pynvml.nvmlInit()
            handle = pynvml.nvmlDeviceGetHandleByIndex(0)
            util = pynvml.nvmlDeviceGetUtilizationRates(handle)
            return float(util.gpu)
        except Exception:
            pass

        # Fallback: derive from GPU memory usage
        used, total = self._get_gpu_memory_usage()
        if total > 0:
            return round((used / total) * 100, 1)

        return 0.0

    def _get_gpu_memory_usage(self) -> tuple[int, int]:
        """Get best-effort GPU memory usage in bytes."""
        try:
            import pynvml
            pynvml.nvmlInit()
            handle = pynvml.nvmlDeviceGetHandleByIndex(0)
            mem = pynvml.nvmlDeviceGetMemoryInfo(handle)
            return int(mem.used), int(mem.total)
        except Exception:
            pass

        try:
            import torch
            if torch.cuda.is_available():
                allocated = torch.cuda.memory_allocated()
                reserved = torch.cuda.memory_reserved()
                total = torch.cuda.get_device_properties(0).total_memory
                return int(max(allocated, reserved)), int(total)
        except Exception:
            pass

        return 0, 0

    def get_stats(self) -> WorkerStats:
        """Get current worker statistics."""
        used_memory, total_memory = self._get_gpu_memory_usage()
        if self.engine:
            engine_used, engine_total = self.engine.get_memory_usage()
            used_memory = max(used_memory, engine_used)
            total_memory = max(total_memory, engine_total)

        # Calculate percentiles
        p50 = self._percentile(50)
        p99 = self._percentile(99)

        # Calculate RPS
        uptime = (datetime.now() - self._started_at).total_seconds()
        rps = self._request_count / uptime if uptime > 0 else 0

        # Error rate
        error_rate = (
            self._error_count / self._request_count
            if self._request_count > 0
            else 0
        )

        gpu_util = self._get_gpu_utilization()

        return WorkerStats(
            queue_depth=self._queued_requests,
            active_requests=self._active_requests,
            gpu_utilization=gpu_util,
            memory_used_bytes=used_memory,
            memory_total_bytes=total_memory,
            requests_per_second=rps,
            avg_latency_ms=(
                self._total_latency_ms / len(self._latencies)
                if self._latencies
                else 0
            ),
            p50_latency_ms=p50,
            p99_latency_ms=p99,
            error_rate=error_rate,
        )

    def get_state(self) -> WorkerState:
        """Get current worker state."""
        return self.state

    def record_startup_stage(self, stage: str) -> None:
        """Record a startup-stage timestamp if it has not been recorded yet."""
        self._startup_events.setdefault(stage, datetime.now())

    def get_startup_status(self) -> dict[str, dict[str, str | int]]:
        """Get startup stage timestamps and durations from worker creation."""
        created_at = self._startup_events["worker_created"]
        stages = {
            stage: timestamp.isoformat()
            for stage, timestamp in sorted(self._startup_events.items(), key=lambda item: item[1])
        }
        durations_ms = {
            stage: int((timestamp - created_at).total_seconds() * 1000)
            for stage, timestamp in self._startup_events.items()
        }
        return {
            "stages": stages,
            "durations_ms": durations_ms,
        }

    def _record_latency(self, latency_ms: float) -> None:
        """Record a latency measurement."""
        if len(self._latencies) == self._latencies.maxlen:
            self._total_latency_ms -= self._latencies[0]
        self._total_latency_ms += latency_ms
        self._latencies.append(latency_ms)

    def _percentile(self, p: int) -> float:
        """Calculate percentile of latencies."""
        if not self._latencies:
            return 0.0
        
        sorted_latencies = sorted(self._latencies)
        index = int(len(sorted_latencies) * p / 100)
        index = min(index, len(sorted_latencies) - 1)
        return sorted_latencies[index]

    async def _acquire_request_slot(self) -> None:
        """Track queued work until a concurrency slot is available."""
        self._queued_requests += 1
        self._refresh_runtime_state()
        try:
            await self._request_semaphore.acquire()
        except Exception:
            self._queued_requests -= 1
            self._refresh_runtime_state()
            raise

        self._queued_requests -= 1
        self._active_requests += 1
        self._refresh_runtime_state()

    def _release_request_slot(self) -> None:
        """Release a concurrency slot and update runtime state."""
        if self._active_requests > 0:
            self._active_requests -= 1
            self._request_semaphore.release()
        self._refresh_runtime_state()

    def _refresh_runtime_state(self) -> None:
        """Update worker state based on in-flight demand."""
        if self._active_requests == 0 and self._queued_requests == 0:
            self._all_requests_idle.set()
        else:
            self._all_requests_idle.clear()

        if self.state not in {WorkerState.READY, WorkerState.BUSY}:
            return

        if self._active_requests >= max(1, self.config.max_concurrent_requests):
            self.state = WorkerState.BUSY
            return

        self.state = WorkerState.READY
