"""Main Infera Worker implementation."""

import asyncio
from collections.abc import AsyncGenerator
from datetime import datetime
import uuid
import structlog

from .config import WorkerConfig, ModelConfig
from .types import InferenceRequest, InferenceResponse, TokenChunk, LoadedModel, WorkerStats, WorkerState
from .engine import InferenceEngine, create_engine

logger = structlog.get_logger()


class Worker:
    """Infera Worker - manages model inference."""

    def __init__(self, config: WorkerConfig) -> None:
        self.config = config
        self.worker_id = config.worker_id or str(uuid.uuid4())
        self.state = WorkerState.INITIALIZING
        self.engine: InferenceEngine | None = None
        
        self._request_count = 0
        self._error_count = 0
        self._total_latency_ms = 0.0
        self._latencies: list[float] = []
        self._started_at = datetime.now()

    async def start(self) -> None:
        logger.info("Starting worker", worker_id=self.worker_id, engine=self.config.engine)
        
        try:
            self.engine = create_engine(self.config)
            
            for model_id in self.config.preload_models:
                await self.load_model(ModelConfig(model_id=model_id))
            
            self.state = WorkerState.READY
            logger.info("Worker ready", worker_id=self.worker_id)
        except Exception as e:
            logger.error("Failed to start worker", error=str(e))
            self.state = WorkerState.ERROR
            raise

    async def stop(self, graceful: bool = True) -> None:
        logger.info("Stopping worker", worker_id=self.worker_id)
        
        if graceful:
            self.state = WorkerState.DRAINING
            await asyncio.sleep(1)
        
        self.state = WorkerState.SHUTTING_DOWN
        
        if self.engine:
            for model in self.engine.get_loaded_models():
                await self.engine.unload_model(model.model_id)

    async def load_model(self, config: ModelConfig) -> LoadedModel:
        if self.engine is None:
            raise RuntimeError("Worker not initialized")
        return await self.engine.load_model(config)

    async def unload_model(self, model_id: str) -> bool:
        if self.engine is None:
            raise RuntimeError("Worker not initialized")
        return await self.engine.unload_model(model_id)

    def get_loaded_models(self) -> list[LoadedModel]:
        return self.engine.get_loaded_models() if self.engine else []

    async def infer(self, request: InferenceRequest) -> InferenceResponse:
        if self.engine is None or self.state != WorkerState.READY:
            raise RuntimeError("Worker not ready")
        
        if not self.engine.is_model_loaded(request.model_id):
            raise ValueError(f"Model {request.model_id} not loaded")
        
        start_time = datetime.now()
        self._request_count += 1
        
        try:
            response = await self.engine.infer(request)
            latency_ms = (datetime.now() - start_time).total_seconds() * 1000
            self._record_latency(latency_ms)
            return response
        except Exception as e:
            self._error_count += 1
            raise

    async def infer_stream(self, request: InferenceRequest) -> AsyncGenerator[TokenChunk, None]:
        if self.engine is None or self.state != WorkerState.READY:
            raise RuntimeError("Worker not ready")
        
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
        except Exception:
            self._error_count += 1
            raise

    async def cancel(self, request_id: str) -> bool:
        return await self.engine.cancel(request_id) if self.engine else False

    def get_stats(self) -> WorkerStats:
        used_memory, total_memory = self.engine.get_memory_usage() if self.engine else (0, 0)
        uptime = (datetime.now() - self._started_at).total_seconds()
        
        return WorkerStats(
            memory_used_bytes=used_memory,
            memory_total_bytes=total_memory,
            requests_per_second=self._request_count / uptime if uptime > 0 else 0,
            avg_latency_ms=self._total_latency_ms / len(self._latencies) if self._latencies else 0,
            p50_latency_ms=self._percentile(50),
            p99_latency_ms=self._percentile(99),
            error_rate=self._error_count / self._request_count if self._request_count > 0 else 0,
        )

    def get_state(self) -> WorkerState:
        return self.state

    def _record_latency(self, latency_ms: float) -> None:
        self._total_latency_ms += latency_ms
        self._latencies.append(latency_ms)
        if len(self._latencies) > 1000:
            removed = self._latencies.pop(0)
            self._total_latency_ms -= removed

    def _percentile(self, p: int) -> float:
        if not self._latencies:
            return 0.0
        sorted_latencies = sorted(self._latencies)
        index = min(int(len(sorted_latencies) * p / 100), len(sorted_latencies) - 1)
        return sorted_latencies[index]