"""Main Infera Worker implementation."""

import asyncio
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
        
        # Stats tracking
        self._request_count = 0
        self._error_count = 0
        self._total_latency_ms = 0.0
        self._latencies: list[float] = []  # For percentile calculation
        self._started_at = datetime.now()
        
        # Lifecycle
        self._shutdown_event = asyncio.Event()

    async def start(self) -> None:
        """Initialize the worker."""
        logger.info("Starting worker", worker_id=self.worker_id, engine=self.config.engine)
        
        try:
            # Create inference engine
            self.engine = create_engine(self.config)
            
            # Preload models if configured
            for model_id in self.config.preload_models:
                logger.info("Preloading model", model_id=model_id)
                await self.load_model(ModelConfig(model_id=model_id))
            
            self.state = WorkerState.READY
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
            # Wait for active requests to complete (with timeout)
            # In production, track active requests and wait
            await asyncio.sleep(1)
        
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
        
        if self.state != WorkerState.READY:
            raise RuntimeError(f"Worker not ready: {self.state}")
        
        if not self.engine.is_model_loaded(request.model_id):
            raise ValueError(f"Model {request.model_id} not loaded")
        
        start_time = datetime.now()
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

    async def infer_stream(
        self, request: InferenceRequest
    ) -> AsyncGenerator[TokenChunk, None]:
        """Process a streaming inference request."""
        if self.engine is None:
            raise RuntimeError("Worker not ready: not initialized")
        
        if self.state != WorkerState.READY:
            raise RuntimeError(f"Worker not ready: {self.state}")
        
        if not self.engine.is_model_loaded(request.model_id):
            raise ValueError(f"Model {request.model_id} not loaded")
        
        start_time = datetime.now()
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
        try:
            import torch
            if torch.cuda.is_available():
                used = torch.cuda.memory_allocated()
                total = torch.cuda.get_device_properties(0).total_memory
                if total > 0:
                    return round((used / total) * 100, 1)
        except Exception:
            pass

        return 0.0

    def get_stats(self) -> WorkerStats:
        """Get current worker statistics."""
        used_memory, total_memory = (0, 0)
        if self.engine:
            used_memory, total_memory = self.engine.get_memory_usage()

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
            queue_depth=0,
            active_requests=0,
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

    def _record_latency(self, latency_ms: float) -> None:
        """Record a latency measurement."""
        self._total_latency_ms += latency_ms
        self._latencies.append(latency_ms)
        
        # Keep only last 1000 measurements
        if len(self._latencies) > 1000:
            removed = self._latencies.pop(0)
            self._total_latency_ms -= removed

    def _percentile(self, p: int) -> float:
        """Calculate percentile of latencies."""
        if not self._latencies:
            return 0.0
        
        sorted_latencies = sorted(self._latencies)
        index = int(len(sorted_latencies) * p / 100)
        index = min(index, len(sorted_latencies) - 1)
        return sorted_latencies[index]
