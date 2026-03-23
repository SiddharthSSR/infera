"""Inference engine interface and implementations."""

from abc import ABC, abstractmethod
from collections.abc import AsyncGenerator
from datetime import datetime
import asyncio
import random
from typing import Callable

from .types import (
    InferenceRequest, InferenceResponse, TokenChunk, Choice, Message,
    Role, FinishReason, UsageStats, LatencyStats, LoadedModel,
)
from .config import WorkerConfig, ModelConfig


class InferenceEngine(ABC):
    """Abstract base class for inference engines."""

    def set_startup_stage_recorder(
        self,
        recorder: Callable[[str], None] | None,
    ) -> None:
        """Install an optional callback for detailed startup-stage reporting."""
        del recorder

    async def warm_model_runtime(self, model_id: str) -> None:
        """Warm optional runtime artifacts after readiness without blocking startup."""
        del model_id

    @abstractmethod
    async def load_model(self, config: ModelConfig) -> LoadedModel:
        pass

    @abstractmethod
    async def unload_model(self, model_id: str) -> bool:
        pass

    @abstractmethod
    def is_model_loaded(self, model_id: str) -> bool:
        pass

    @abstractmethod
    def get_loaded_models(self) -> list[LoadedModel]:
        pass

    @abstractmethod
    async def infer(self, request: InferenceRequest) -> InferenceResponse:
        pass

    @abstractmethod
    async def infer_stream(self, request: InferenceRequest) -> AsyncGenerator[TokenChunk, None]:
        pass

    @abstractmethod
    async def cancel(self, request_id: str) -> bool:
        pass

    @abstractmethod
    def get_memory_usage(self) -> tuple[int, int]:
        pass


class MockEngine(InferenceEngine):
    """Mock inference engine for testing."""

    def __init__(self, config: WorkerConfig) -> None:
        self.config = config
        self.loaded_models: dict[str, LoadedModel] = {}
        self.active_requests: set[str] = set()
        self._cancelled: set[str] = set()

    async def load_model(self, config: ModelConfig) -> LoadedModel:
        await asyncio.sleep(0.1)
        model = LoadedModel(
            model_id=config.model_id,
            version="1.0.0",
            loaded_at=datetime.now(),
            memory_bytes=8 * 1024 * 1024 * 1024,
            max_batch_size=config.max_batch_size,
            max_sequence_length=config.max_sequence_length,
        )
        self.loaded_models[config.model_id] = model
        return model

    async def unload_model(self, model_id: str) -> bool:
        if model_id in self.loaded_models:
            del self.loaded_models[model_id]
            return True
        return False

    def is_model_loaded(self, model_id: str) -> bool:
        return model_id in self.loaded_models

    def get_loaded_models(self) -> list[LoadedModel]:
        return list(self.loaded_models.values())

    async def infer(self, request: InferenceRequest) -> InferenceResponse:
        start_time = datetime.now()
        self.active_requests.add(request.request_id)

        try:
            await asyncio.sleep(0.05 + random.random() * 0.1)

            response_text = f"Mock response to: {request.messages[-1].content}"
            prompt_tokens = request.token_estimate()
            completion_tokens = len(response_text) // 4

            end_time = datetime.now()
            latency_ms = int((end_time - start_time).total_seconds() * 1000)

            return InferenceResponse(
                request_id=request.request_id,
                model_id=request.model_id,
                choices=[Choice(
                    index=0,
                    message=Message(role=Role.ASSISTANT, content=response_text),
                    finish_reason=FinishReason.STOP,
                )],
                usage=UsageStats(
                    prompt_tokens=prompt_tokens,
                    completion_tokens=completion_tokens,
                    total_tokens=prompt_tokens + completion_tokens,
                ),
                latency=LatencyStats(
                    queue_ms=0,
                    inference_ms=latency_ms,
                    total_ms=latency_ms,
                    time_to_first_token_ms=latency_ms // 2,
                ),
            )
        finally:
            self.active_requests.discard(request.request_id)

    async def infer_stream(self, request: InferenceRequest) -> AsyncGenerator[TokenChunk, None]:
        self.active_requests.add(request.request_id)

        try:
            response_text = f"Mock streaming response to: {request.messages[-1].content}"
            tokens = response_text.split()
            prompt_tokens = request.token_estimate()

            for i, token in enumerate(tokens):
                await asyncio.sleep(0.02)
                is_last = i == len(tokens) - 1
                yield TokenChunk(
                    request_id=request.request_id,
                    index=i,
                    delta=token + " ",
                    finish_reason=FinishReason.STOP if is_last else None,
                    usage=UsageStats(prompt_tokens, i + 1, prompt_tokens + i + 1) if is_last else None,
                )
        finally:
            self.active_requests.discard(request.request_id)

    async def cancel(self, request_id: str) -> bool:
        if request_id in self.active_requests:
            self._cancelled.add(request_id)
            return True
        return False

    def get_memory_usage(self) -> tuple[int, int]:
        used = sum(m.memory_bytes for m in self.loaded_models.values())
        return used, 16 * 1024 * 1024 * 1024


def create_engine(config: WorkerConfig) -> InferenceEngine:
    if config.engine == "mock":
        return MockEngine(config)
    elif config.engine == "vllm":
        from .engines.vllm_engine import VLLMEngine
        return VLLMEngine(config)
    else:
        raise ValueError(f"Unknown engine: {config.engine}")
