"""SGLang inference engine implementation."""

from __future__ import annotations

from collections.abc import AsyncGenerator
from datetime import datetime
import asyncio
import inspect
from typing import Any

import structlog

from ..config import ModelConfig, WorkerConfig
from ..engine import EngineCapabilities, EngineDefinition, register_engine
from ..types import Choice, InferenceRequest, InferenceResponse, LatencyStats, LoadedModel, TokenChunk, UsageStats
from .base import TokenizerPromptEngine

try:
    import sglang as sgl
    from sglang.utils import async_stream_and_merge

    SGLANG_AVAILABLE = True
except ImportError:
    SGLANG_AVAILABLE = False
    sgl = None
    async_stream_and_merge = None

logger = structlog.get_logger()


class SGLangEngine(TokenizerPromptEngine):
    """SGLang offline engine adapter."""

    def __init__(self, config: WorkerConfig) -> None:
        if not SGLANG_AVAILABLE:
            raise ImportError("SGLang is not installed. Install with: pip install sglang")

        super().__init__(config)
        self.engines: dict[str, Any] = {}
        self.loaded_models: dict[str, LoadedModel] = {}

    async def load_model(self, model_config: ModelConfig) -> LoadedModel:
        model_path = self._resolve_model_path(model_config)
        self._record_model_cache_probe(model_config, model_path)

        runtime = self.config.sglang_runtime
        engine_kwargs: dict[str, Any] = {"model_path": model_path}
        optional_kwargs: dict[str, Any] = {
            "tp_size": runtime.tp_size,
            "mem_fraction_static": runtime.mem_fraction_static,
            "context_length": runtime.context_length,
            "chunked_prefill_size": runtime.chunked_prefill_size,
            "max_running_requests": runtime.max_running_requests,
            "schedule_policy": runtime.schedule_policy,
            "attention_backend": runtime.attention_backend,
            "sampling_backend": runtime.sampling_backend,
            "disable_cuda_graph": runtime.disable_cuda_graph,
            "quantization": model_config.quantization,
        }

        tool_call_parser = self.config.tool_call_parser.strip()
        if tool_call_parser:
            optional_kwargs["tool_call_parser"] = tool_call_parser
            logger.info(
                "SGLang tool calling enabled",
                tool_call_parser=tool_call_parser,
                model_id=model_config.model_id,
            )

        signature = inspect.signature(sgl.Engine)
        supported_kwargs = set(signature.parameters)
        accepts_variadic_kwargs = any(
            parameter.kind == inspect.Parameter.VAR_KEYWORD
            for parameter in signature.parameters.values()
        )
        for key, value in optional_kwargs.items():
            if value is not None and (accepts_variadic_kwargs or key in supported_kwargs):
                engine_kwargs[key] = value

        self._record_stage("sglang_engine_init_started")
        # SGLang configures signal handlers during engine startup, which must
        # happen on the main interpreter thread.
        engine = sgl.Engine(**engine_kwargs)
        self._record_stage("sglang_engine_init_finished")
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

        shutdown = getattr(engine, "shutdown", None)
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
            prompt = self._build_prompt_with_tools(request)
            sampling_params = self._build_sampling_params(request)
            if async_stream_and_merge is None:
                outputs = await engine.async_generate([prompt], sampling_params)
                final_output = outputs[0]
                text = self._extract_text(final_output)
                first_token_time: datetime | None = None
                prompt_tokens, completion_tokens = self._estimate_usage(final_output, request, prompt, text)
            else:
                first_token_time = None
                chunks: list[str] = []
                async for chunk_text in async_stream_and_merge(engine, prompt, sampling_params):
                    if not chunk_text:
                        continue
                    if first_token_time is None:
                        first_token_time = datetime.now()
                    chunks.append(chunk_text)
                final_output = None
                text = "".join(chunks)
                prompt_tokens = self._count_prompt_tokens_from_prompt(request.model_id, prompt, request)
                completion_tokens = self._count_completion_tokens(request.model_id, text)

            latency_ms = int((datetime.now() - start_time).total_seconds() * 1000)
            ttft_ms = (
                int((first_token_time - start_time).total_seconds() * 1000)
                if first_token_time is not None
                else latency_ms
            )

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
        if async_stream_and_merge is None:
            raise RuntimeError("SGLang streaming utilities are unavailable")

        self.active_requests.add(request.request_id)
        try:
            prompt = self._build_prompt_with_tools(request)
            sampling_params = self._build_sampling_params(request)
            chunk_index = 0
            accumulated = ""

            async for chunk_text in async_stream_and_merge(engine, prompt, sampling_params):
                if not chunk_text:
                    continue
                accumulated += chunk_text
                yield TokenChunk(
                    request_id=request.request_id,
                    index=chunk_index,
                    delta=chunk_text,
                )
                chunk_index += 1

            prompt_tokens = self._count_prompt_tokens_from_prompt(request.model_id, prompt, request)
            completion_tokens = self._count_completion_tokens(request.model_id, accumulated)
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

    def _infer_max_sequence_length(self, engine: Any, model_config: ModelConfig) -> int:
        for attr_chain in (
            ("server_args", "context_length"),
            ("model_config", "context_length"),
            ("model_config", "max_model_len"),
            ("max_model_len",),
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

    def _build_sampling_params(self, request: InferenceRequest) -> dict[str, Any]:
        sampling_params = request.parameters.to_sampling_params()
        max_tokens = sampling_params.pop("max_tokens", None)
        if max_tokens is not None:
            sampling_params["max_new_tokens"] = max_tokens
        return sampling_params

    def _extract_text(self, output: Any) -> str:
        if isinstance(output, dict):
            return str(output.get("text", ""))
        if hasattr(output, "text"):
            return str(getattr(output, "text"))
        if hasattr(output, "outputs"):
            outputs = getattr(output, "outputs")
            if outputs:
                return str(getattr(outputs[0], "text", ""))
        return str(output)

    def _extract_finish_reason(self, output: Any):
        if isinstance(output, dict):
            reason = output.get("finish_reason") or output.get("meta_info", {}).get("finish_reason")
            return self._map_finish_reason(reason)
        if hasattr(output, "finish_reason"):
            return self._map_finish_reason(getattr(output, "finish_reason"))
        return self._map_finish_reason("stop")

    def _estimate_usage(self, output: Any, request: InferenceRequest, prompt: str, text: str) -> tuple[int, int]:
        prompt_tokens = self._count_prompt_tokens_from_prompt(request.model_id, prompt, request)
        completion_tokens = self._count_completion_tokens(request.model_id, text)
        if isinstance(output, dict):
            meta_info = output.get("meta_info", {})
            prompt_tokens = int(meta_info.get("prompt_tokens", prompt_tokens))
            completion_tokens = int(meta_info.get("completion_tokens", completion_tokens))
        return prompt_tokens, completion_tokens


def create_sglang_engine(config: WorkerConfig):
    return SGLangEngine(config)


register_engine(
    EngineDefinition(
        engine_id="sglang",
        display_name="SGLang",
        create=create_sglang_engine,
        optional_dependency="sglang",
        capabilities=EngineCapabilities(
            supports_streaming=True,
            supports_cancellation=True,
            supports_dynamic_model_loading=True,
            supports_runtime_warmup=True,
        ),
    )
)
