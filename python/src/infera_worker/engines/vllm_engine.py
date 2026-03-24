"""vLLM inference engine implementation."""

from __future__ import annotations

from collections.abc import AsyncGenerator
from datetime import datetime
import inspect
from typing import Any

from ..config import ModelConfig, WorkerConfig
from ..engine import EngineCapabilities, EngineDefinition, register_engine
from ..types import (
    Choice,
    InferenceRequest,
    InferenceResponse,
    LatencyStats,
    LoadedModel,
    TokenChunk,
    UsageStats,
)
from .base import TokenizerPromptEngine

# vLLM imports are optional - only loaded when engine is used
try:
    from vllm import AsyncEngineArgs, AsyncLLMEngine, SamplingParams
    from vllm.outputs import RequestOutput

    VLLM_AVAILABLE = True
except ImportError:
    VLLM_AVAILABLE = False
    AsyncLLMEngine = None
    AsyncEngineArgs = None
    SamplingParams = None
    RequestOutput = None


class VLLMEngine(TokenizerPromptEngine):
    """vLLM-based inference engine for maximum throughput."""

    def __init__(self, config: WorkerConfig) -> None:
        if not VLLM_AVAILABLE:
            raise ImportError("vLLM is not installed. Install with: pip install vllm")

        super().__init__(config)
        self.engines: dict[str, AsyncLLMEngine] = {}
        self.loaded_models: dict[str, LoadedModel] = {}

    async def load_model(self, model_config: ModelConfig) -> LoadedModel:
        """Load a model using vLLM."""
        model_path = self._resolve_model_path(model_config)
        self._record_model_cache_probe(model_config, model_path)

        runtime = self.config.vllm_runtime
        engine_kwargs: dict[str, Any] = {
            "model": model_path,
            "revision": model_config.revision,
            "tensor_parallel_size": runtime.tensor_parallel_size,
            "gpu_memory_utilization": runtime.gpu_memory_utilization,
            "max_model_len": runtime.max_model_len,
            "quantization": model_config.quantization,
            "trust_remote_code": True,
            "enable_prefix_caching": runtime.enable_prefix_caching,
            "enable_chunked_prefill": runtime.enable_chunked_prefill,
        }

        optional_engine_kwargs: dict[str, Any] = {}
        if runtime.max_num_batched_tokens is not None:
            optional_engine_kwargs["max_num_batched_tokens"] = runtime.max_num_batched_tokens
        if runtime.max_num_seqs is not None:
            optional_engine_kwargs["max_num_seqs"] = runtime.max_num_seqs
        if runtime.swap_space is not None:
            optional_engine_kwargs["swap_space"] = runtime.swap_space
        if runtime.enforce_eager:
            optional_engine_kwargs["enforce_eager"] = True
        if runtime.num_scheduler_steps > 0:
            optional_engine_kwargs["num_scheduler_steps"] = runtime.num_scheduler_steps

        spec_model = runtime.speculative_model.strip()
        if spec_model and runtime.num_speculative_tokens > 0:
            optional_engine_kwargs["speculative_model"] = spec_model
            optional_engine_kwargs["num_speculative_tokens"] = runtime.num_speculative_tokens
            if spec_model == "[ngram]" and runtime.ngram_prompt_lookup_num_tokens > 0:
                optional_engine_kwargs["ngram_prompt_lookup_num_tokens"] = runtime.ngram_prompt_lookup_num_tokens

        supported_kwargs = set(inspect.signature(AsyncEngineArgs).parameters)
        for key, value in optional_engine_kwargs.items():
            if key in supported_kwargs:
                engine_kwargs[key] = value

        engine_args = AsyncEngineArgs(**engine_kwargs)

        self._record_stage("vllm_engine_init_started")
        engine = AsyncLLMEngine.from_engine_args(engine_args)
        self._record_stage("vllm_engine_init_finished")
        self.engines[model_config.model_id] = engine
        self._register_model_path(model_config.model_id, model_path)
        self._record_stage("tokenizer_load_deferred")

        model_cfg = engine.model_config
        loaded = LoadedModel(
            model_id=model_config.model_id,
            version=model_config.revision or "latest",
            loaded_at=datetime.now(),
            memory_bytes=self._estimate_memory(),
            max_batch_size=model_config.max_batch_size,
            max_sequence_length=getattr(model_cfg, "max_model_len", model_config.max_sequence_length),
        )
        self.loaded_models[model_config.model_id] = loaded
        return loaded

    async def unload_model(self, model_id: str) -> bool:
        """Unload a model."""
        if model_id in self.engines:
            del self.engines[model_id]
            del self.loaded_models[model_id]
            self._clear_model_path(model_id)
            return True
        return False

    def is_model_loaded(self, model_id: str) -> bool:
        return model_id in self.engines

    def get_loaded_models(self) -> list[LoadedModel]:
        return list(self.loaded_models.values())

    async def infer(self, request: InferenceRequest) -> InferenceResponse:
        """Run inference with vLLM."""
        start_time = datetime.now()

        engine = self.engines.get(request.model_id)
        if engine is None:
            raise ValueError(f"Model {request.model_id} not loaded")

        self.active_requests.add(request.request_id)

        try:
            prompt = self._build_prompt(request)
            sampling_params = SamplingParams(**request.parameters.to_sampling_params())

            results: list[RequestOutput] = []
            first_token_time: datetime | None = None
            async for output in engine.generate(prompt, sampling_params, request.request_id):
                if first_token_time is None:
                    first_token_time = datetime.now()
                results.append(output)

            final_output = results[-1]
            completion_output = final_output.outputs[0]

            end_time = datetime.now()
            latency_ms = int((end_time - start_time).total_seconds() * 1000)
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
                        message=self._build_response_message(completion_output.text),
                        finish_reason=self._map_finish_reason(completion_output.finish_reason),
                    )
                ],
                usage=UsageStats(
                    prompt_tokens=len(final_output.prompt_token_ids),
                    completion_tokens=len(completion_output.token_ids),
                    total_tokens=len(final_output.prompt_token_ids) + len(completion_output.token_ids),
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

    async def infer_stream(
        self, request: InferenceRequest
    ) -> AsyncGenerator[TokenChunk, None]:
        """Stream tokens from vLLM."""
        engine = self.engines.get(request.model_id)
        if engine is None:
            raise ValueError(f"Model {request.model_id} not loaded")

        self.active_requests.add(request.request_id)

        try:
            prompt = self._build_prompt(request)
            sampling_params = SamplingParams(**request.parameters.to_sampling_params())

            prev_text = ""
            chunk_index = 0
            prompt_tokens = 0

            async for output in engine.generate(prompt, sampling_params, request.request_id):
                if prompt_tokens == 0:
                    prompt_tokens = len(output.prompt_token_ids)

                completion_output = output.outputs[0]
                new_text = completion_output.text[len(prev_text):]
                prev_text = completion_output.text

                if not new_text:
                    continue

                is_finished = completion_output.finish_reason is not None
                completion_tokens = len(completion_output.token_ids)

                yield TokenChunk(
                    request_id=request.request_id,
                    index=chunk_index,
                    delta=new_text,
                    finish_reason=self._map_finish_reason(completion_output.finish_reason) if is_finished else None,
                    usage=(
                        UsageStats(
                            prompt_tokens=prompt_tokens,
                            completion_tokens=completion_tokens,
                            total_tokens=prompt_tokens + completion_tokens,
                        )
                        if is_finished
                        else None
                    ),
                )
                chunk_index += 1
        finally:
            self.active_requests.discard(request.request_id)

    async def cancel(self, request_id: str) -> bool:
        """Cancel a request."""
        if request_id not in self.active_requests:
            return False
        for engine in self.engines.values():
            await engine.abort(request_id)
        return True

    def _estimate_memory(self) -> int:
        """Estimate model memory usage."""
        used, _total = self.get_memory_usage()
        if used > 0:
            return used
        return 8 * 1024 * 1024 * 1024


def create_vllm_engine(config: WorkerConfig):
    return VLLMEngine(config)


register_engine(
    EngineDefinition(
        engine_id="vllm",
        display_name="vLLM",
        create=create_vllm_engine,
        optional_dependency="vllm",
        capabilities=EngineCapabilities(
            supports_streaming=True,
            supports_cancellation=True,
            supports_dynamic_model_loading=True,
            supports_runtime_warmup=True,
        ),
    )
)
