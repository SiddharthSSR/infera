"""TensorRT-LLM inference engine implementation."""

from __future__ import annotations

from collections.abc import AsyncGenerator
from datetime import datetime
import asyncio
import inspect
from typing import Any

from ..config import ModelConfig, WorkerConfig
from ..engine import EngineCapabilities, EngineDefinition, register_engine
from ..types import Choice, InferenceRequest, InferenceResponse, LatencyStats, LoadedModel, TokenChunk, UsageStats
from .base import TokenizerPromptEngine

try:
    from tensorrt_llm import SamplingParams
    try:
        from tensorrt_llm import BuildConfig
    except ImportError:
        from tensorrt_llm.llmapi import BuildConfig
    try:
        from tensorrt_llm import KvCacheConfig
    except ImportError:
        try:
            from tensorrt_llm.bindings.executor import KvCacheConfig
        except ImportError:
            from tensorrt_llm.llmapi import KvCacheConfig
    try:
        from tensorrt_llm._tensorrt_engine import LLM
    except ImportError:
        try:
            from tensorrt_llm import LLM
        except ImportError:
            from tensorrt_llm.hlapi.llm import LLM

    TENSORRT_LLM_AVAILABLE = True
    TENSORRT_LLM_IMPORT_ERROR: ImportError | None = None
except ImportError as exc:
    TENSORRT_LLM_AVAILABLE = False
    TENSORRT_LLM_IMPORT_ERROR = exc
    LLM = None
    SamplingParams = None
    BuildConfig = None
    KvCacheConfig = None


class TensorRTLLMEngine(TokenizerPromptEngine):
    """TensorRT-LLM LLM API adapter."""

    def __init__(self, config: WorkerConfig) -> None:
        if not TENSORRT_LLM_AVAILABLE:
            detail = f" Original import error: {TENSORRT_LLM_IMPORT_ERROR}" if TENSORRT_LLM_IMPORT_ERROR else ""
            raise ImportError(
                "TensorRT-LLM import failed. Ensure the worker image includes tensorrt_llm and its runtime dependencies."
                f"{detail}"
            ) from TENSORRT_LLM_IMPORT_ERROR

        super().__init__(config)
        self.engines: dict[str, Any] = {}
        self.loaded_models: dict[str, LoadedModel] = {}

    async def load_model(self, model_config: ModelConfig) -> LoadedModel:
        model_path = self._resolve_model_path(model_config)
        self._record_model_cache_probe(model_config, model_path)

        runtime = self.config.tensorrt_llm_runtime
        requested_backend = (runtime.backend or "").strip().lower()
        if requested_backend and requested_backend not in {"tensorrt", "tensorrt_llm", "trt", "trtllm"}:
            raise ValueError(
                "TensorRTLLMEngine only supports the TensorRT backend. "
                "Leave INFERA_TENSORRT_LLM_BACKEND unset or set it to 'tensorrt'."
            )

        llm_kwargs: dict[str, Any] = {"model": model_path}
        optional_kwargs = {
            "tensor_parallel_size": runtime.tensor_parallel_size,
        }

        llm_signature = inspect.signature(LLM)
        supported_llm_kwargs = set(llm_signature.parameters)
        llm_accepts_variadic_kwargs = any(
            parameter.kind == inspect.Parameter.VAR_KEYWORD
            for parameter in llm_signature.parameters.values()
        )
        for key, value in optional_kwargs.items():
            if value is not None and (llm_accepts_variadic_kwargs or key in supported_llm_kwargs):
                llm_kwargs[key] = value

        build_config = self._build_build_config(runtime)
        if build_config is not None and (llm_accepts_variadic_kwargs or "build_config" in supported_llm_kwargs):
            llm_kwargs["build_config"] = build_config

        kv_cache_config = self._build_kv_cache_config(runtime)
        if kv_cache_config is not None and (llm_accepts_variadic_kwargs or "kv_cache_config" in supported_llm_kwargs):
            llm_kwargs["kv_cache_config"] = kv_cache_config

        self._record_stage("tensorrt_llm_engine_init_started")
        engine = await asyncio.to_thread(LLM, **llm_kwargs)
        self._record_stage("tensorrt_llm_engine_init_finished")
        self.engines[model_config.model_id] = engine
        self._register_model_path(model_config.model_id, model_path)
        self._record_stage("tokenizer_load_deferred")

        loaded = LoadedModel(
            model_id=model_config.model_id,
            version=model_config.revision or "latest",
            loaded_at=datetime.now(),
            memory_bytes=self._estimate_memory(),
            max_batch_size=model_config.max_batch_size,
            max_sequence_length=self._infer_max_sequence_length(engine, model_config),
        )
        self.loaded_models[model_config.model_id] = loaded
        return loaded

    async def unload_model(self, model_id: str) -> bool:
        engine = self.engines.pop(model_id, None)
        if engine is None:
            return False
        self.loaded_models.pop(model_id, None)
        self._clear_model_path(model_id)

        shutdown = getattr(engine, "shutdown", None) or getattr(engine, "close", None)
        if callable(shutdown):
            await asyncio.to_thread(shutdown)
        return True

    def is_model_loaded(self, model_id: str) -> bool:
        return model_id in self.engines

    def get_loaded_models(self) -> list[LoadedModel]:
        return list(self.loaded_models.values())

    async def infer(self, request: InferenceRequest) -> InferenceResponse:
        start_time = datetime.now()
        engine = self.engines.get(request.model_id)
        if engine is None:
            raise ValueError(f"Model {request.model_id} not loaded")

        self.active_requests.add(request.request_id)
        try:
            prompt = self._build_prompt(request)
            sampling_params = self._build_sampling_params(request)
            first_token_time = datetime.now()
            outputs = await asyncio.to_thread(engine.generate, [prompt], sampling_params)
            final_output = outputs[0]
            text = self._extract_text(final_output)

            latency_ms = int((datetime.now() - start_time).total_seconds() * 1000)
            ttft_ms = int((first_token_time - start_time).total_seconds() * 1000)
            prompt_tokens, completion_tokens = self._estimate_usage(final_output, request, text)

            return InferenceResponse(
                request_id=request.request_id,
                model_id=request.model_id,
                choices=[
                    Choice(
                        index=0,
                        message=self._build_response_message(text),
                        finish_reason=self._extract_finish_reason(final_output),
                    )
                ],
                usage=UsageStats(
                    prompt_tokens=prompt_tokens,
                    completion_tokens=completion_tokens,
                    total_tokens=prompt_tokens + completion_tokens,
                ),
                latency=LatencyStats(
                    queue_ms=0,
                    inference_ms=latency_ms,
                    total_ms=latency_ms,
                    time_to_first_token_ms=ttft_ms,
                ),
            )
        finally:
            self.active_requests.discard(request.request_id)

    async def infer_stream(self, request: InferenceRequest) -> AsyncGenerator[TokenChunk, None]:
        engine = self.engines.get(request.model_id)
        if engine is None:
            raise ValueError(f"Model {request.model_id} not loaded")

        generate_async = getattr(engine, "generate_async", None)
        if not callable(generate_async):
            raise RuntimeError("TensorRT-LLM runtime does not expose generate_async for streaming")

        self.active_requests.add(request.request_id)
        try:
            prompt = self._build_prompt(request)
            sampling_params = self._build_sampling_params(request)
            prev_text = ""
            chunk_index = 0

            async for output in generate_async(prompt, sampling_params, streaming=True):
                text = self._extract_text(output)
                new_text = text[len(prev_text):]
                prev_text = text
                if not new_text:
                    continue

                finish_reason = self._extract_finish_reason(output)
                is_final = finish_reason is not None and finish_reason.value != "stop" or bool(
                    getattr(output, "finished", False)
                )
                yield TokenChunk(
                    request_id=request.request_id,
                    index=chunk_index,
                    delta=new_text,
                    finish_reason=finish_reason if is_final else None,
                    usage=self._stream_usage(output, request, text) if is_final else None,
                )
                chunk_index += 1

            if chunk_index == 0 or prev_text:
                prompt_tokens, completion_tokens = self._estimate_usage(None, request, prev_text)
                yield TokenChunk(
                    request_id=request.request_id,
                    index=chunk_index,
                    delta="",
                    finish_reason=self._map_finish_reason("stop"),
                    usage=UsageStats(
                        prompt_tokens=prompt_tokens,
                        completion_tokens=completion_tokens,
                        total_tokens=prompt_tokens + completion_tokens,
                    ),
                )
        finally:
            self.active_requests.discard(request.request_id)

    async def cancel(self, request_id: str) -> bool:
        if request_id not in self.active_requests:
            return False

        for engine in self.engines.values():
            abort = getattr(engine, "abort", None) or getattr(engine, "cancel", None)
            if abort is None:
                continue
            result = abort(request_id)
            if inspect.isawaitable(result):
                await result
            else:
                await asyncio.to_thread(lambda: result)
        return True

    def _build_sampling_params(self, request: InferenceRequest) -> Any:
        return SamplingParams(**request.parameters.to_sampling_params())

    def _build_build_config(self, runtime) -> Any | None:
        if BuildConfig is None:
            return None
        kwargs: dict[str, Any] = {}
        signature = inspect.signature(BuildConfig)
        supported = set(signature.parameters)
        accepts_variadic_kwargs = any(
            parameter.kind == inspect.Parameter.VAR_KEYWORD
            for parameter in signature.parameters.values()
        )
        if runtime.max_batch_size is not None and (accepts_variadic_kwargs or "max_batch_size" in supported):
            kwargs["max_batch_size"] = runtime.max_batch_size
        if runtime.max_num_tokens is not None and (accepts_variadic_kwargs or "max_num_tokens" in supported):
            kwargs["max_num_tokens"] = runtime.max_num_tokens
        if runtime.max_beam_width is not None and (accepts_variadic_kwargs or "max_beam_width" in supported):
            kwargs["max_beam_width"] = runtime.max_beam_width
        if runtime.enable_chunked_context and (accepts_variadic_kwargs or "enable_chunked_context" in supported):
            kwargs["enable_chunked_context"] = runtime.enable_chunked_context
        return BuildConfig(**kwargs) if kwargs else None

    def _build_kv_cache_config(self, runtime) -> Any | None:
        if KvCacheConfig is None or runtime.kv_cache_free_gpu_memory_fraction is None:
            return None
        signature = inspect.signature(KvCacheConfig)
        supported = set(signature.parameters)
        accepts_variadic_kwargs = any(
            parameter.kind == inspect.Parameter.VAR_KEYWORD
            for parameter in signature.parameters.values()
        )
        if not accepts_variadic_kwargs and "free_gpu_memory_fraction" not in supported:
            return None
        return KvCacheConfig(free_gpu_memory_fraction=runtime.kv_cache_free_gpu_memory_fraction)

    def _infer_max_sequence_length(self, engine: Any, model_config: ModelConfig) -> int:
        for attr_chain in (
            ("max_seq_len",),
            ("config", "max_seq_len"),
            ("config", "max_input_len"),
        ):
            current = engine
            found = True
            for attr in attr_chain:
                current = getattr(current, attr, None)
                if current is None:
                    found = False
                    break
            if found and isinstance(current, int):
                return current
        return model_config.max_sequence_length

    def _estimate_memory(self) -> int:
        used, _total = self.get_memory_usage()
        if used > 0:
            return used
        return 8 * 1024 * 1024 * 1024

    def _extract_text(self, output: Any) -> str:
        if output is None:
            return ""
        if isinstance(output, dict):
            if "text" in output:
                return str(output["text"])
            outputs = output.get("outputs")
            if outputs:
                return str(getattr(outputs[0], "text", outputs[0].get("text", "")))
        if hasattr(output, "text"):
            return str(getattr(output, "text"))
        outputs = getattr(output, "outputs", None)
        if outputs:
            first = outputs[0]
            if isinstance(first, dict):
                return str(first.get("text", ""))
            return str(getattr(first, "text", ""))
        return ""

    def _extract_finish_reason(self, output: Any):
        if output is None:
            return self._map_finish_reason("stop")
        if isinstance(output, dict):
            reason = output.get("finish_reason")
            if reason is None and output.get("outputs"):
                first = output["outputs"][0]
                reason = first.get("finish_reason") if isinstance(first, dict) else getattr(first, "finish_reason", None)
            return self._map_finish_reason(reason)
        if hasattr(output, "finish_reason"):
            return self._map_finish_reason(getattr(output, "finish_reason"))
        outputs = getattr(output, "outputs", None)
        if outputs:
            first = outputs[0]
            if isinstance(first, dict):
                return self._map_finish_reason(first.get("finish_reason"))
            return self._map_finish_reason(getattr(first, "finish_reason", None))
        return self._map_finish_reason("stop")

    def _estimate_usage(self, output: Any, request: InferenceRequest, text: str) -> tuple[int, int]:
        prompt_tokens = request.token_estimate()
        completion_tokens = max(1, len(text) // 4) if text else 0
        if output is None:
            return prompt_tokens, completion_tokens

        for container in (output, getattr(output, "outputs", [None])[0] if getattr(output, "outputs", None) else None):
            if container is None:
                continue
            if isinstance(container, dict):
                prompt_tokens = int(container.get("prompt_tokens", prompt_tokens))
                token_ids = container.get("token_ids")
                if token_ids is not None:
                    completion_tokens = len(token_ids)
            else:
                prompt_tokens = int(getattr(container, "prompt_tokens", prompt_tokens))
                token_ids = getattr(container, "token_ids", None)
                if token_ids is not None:
                    completion_tokens = len(token_ids)
        return prompt_tokens, completion_tokens

    def _stream_usage(self, output: Any, request: InferenceRequest, text: str) -> UsageStats:
        prompt_tokens, completion_tokens = self._estimate_usage(output, request, text)
        return UsageStats(
            prompt_tokens=prompt_tokens,
            completion_tokens=completion_tokens,
            total_tokens=prompt_tokens + completion_tokens,
        )


def create_tensorrt_llm_engine(config: WorkerConfig):
    return TensorRTLLMEngine(config)


register_engine(
    EngineDefinition(
        engine_id="tensorrt_llm",
        display_name="TensorRT-LLM",
        create=create_tensorrt_llm_engine,
        optional_dependency="tensorrt_llm",
        capabilities=EngineCapabilities(
            supports_streaming=True,
            supports_cancellation=True,
            supports_dynamic_model_loading=True,
            supports_runtime_warmup=True,
        ),
    )
)
