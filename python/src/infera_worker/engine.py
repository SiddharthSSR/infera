"""Inference engine interface, registry, and factory."""

from __future__ import annotations

from abc import ABC, abstractmethod
from collections.abc import AsyncGenerator, Callable
from dataclasses import dataclass, field
import importlib
from typing import Any

from .config import ModelConfig, WorkerConfig, normalize_engine_name


class InferenceEngine(ABC):
    """Abstract base class for inference engines."""

    def set_startup_stage_recorder(
        self,
        recorder: Callable[[str], None] | None,
    ) -> None:
        """Install an optional callback for detailed startup-stage reporting."""
        del recorder

    def set_startup_metadata_recorder(
        self,
        recorder: Callable[[str, dict[str, Any]], None] | None,
    ) -> None:
        """Install an optional callback for startup metadata reporting."""
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


class EngineError(RuntimeError):
    """Base class for inference-engine lifecycle and selection errors."""


class EngineNotFoundError(EngineError):
    """Raised when an unknown engine ID is requested."""


class EngineDependencyError(EngineError):
    """Raised when an engine's optional runtime dependency is not installed."""


@dataclass(frozen=True, slots=True)
class EngineCapabilities:
    """Static feature metadata for an inference engine implementation."""

    supports_streaming: bool = True
    supports_cancellation: bool = True
    supports_dynamic_model_loading: bool = True
    supports_runtime_warmup: bool = True


@dataclass(frozen=True, slots=True)
class EngineDefinition:
    """Registry entry for a loadable inference engine."""

    engine_id: str
    display_name: str
    create: Callable[[WorkerConfig], InferenceEngine]
    optional_dependency: str | None = None
    capabilities: EngineCapabilities = field(default_factory=EngineCapabilities)


_REGISTRY: dict[str, EngineDefinition] = {}
_BUILTIN_ENGINE_MODULES = {
    "mock": "infera_worker.engines.mock_engine",
    "vllm": "infera_worker.engines.vllm_engine",
    "sglang": "infera_worker.engines.sglang_engine",
    "tensorrt_llm": "infera_worker.engines.tensorrt_llm_engine",
}


def register_engine(definition: EngineDefinition) -> None:
    """Register a concrete inference engine definition."""
    _REGISTRY[definition.engine_id] = definition


def _ensure_engine_registered(engine_id: str) -> None:
    if engine_id in _REGISTRY:
        return

    module_path = _BUILTIN_ENGINE_MODULES.get(engine_id)
    if module_path is None:
        raise EngineNotFoundError(f"Unknown inference engine: {engine_id}")
    importlib.import_module(module_path)

    if engine_id not in _REGISTRY:
        raise EngineNotFoundError(f"Inference engine {engine_id} failed to register")


def get_engine_definition(engine_id: str) -> EngineDefinition:
    normalized = normalize_engine_name(engine_id)
    _ensure_engine_registered(normalized)
    return _REGISTRY[normalized]


def list_engine_definitions() -> list[EngineDefinition]:
    """Return all builtin engine definitions."""
    for engine_id in _BUILTIN_ENGINE_MODULES:
        _ensure_engine_registered(engine_id)
    return [_REGISTRY[engine_id] for engine_id in sorted(_REGISTRY)]


def create_engine(config: WorkerConfig) -> InferenceEngine:
    """Create an inference engine instance from normalized worker config."""
    definition = get_engine_definition(config.engine)
    return definition.create(config)


from .engines.mock_engine import MockEngine  # noqa: E402
