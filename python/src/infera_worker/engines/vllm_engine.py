"""vLLM inference engine implementation."""

import asyncio
import inspect
import os
from collections.abc import AsyncGenerator, Callable
from datetime import datetime
from pathlib import Path
from typing import Any

import structlog

from ..config import ModelConfig, WorkerConfig
from ..engine import InferenceEngine
from ..types import (
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
)

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


_TOKENIZER_UNINITIALIZED = object()
logger = structlog.get_logger()


class VLLMEngine(InferenceEngine):
    """vLLM-based inference engine for maximum throughput."""

    def __init__(self, config: WorkerConfig) -> None:
        if not VLLM_AVAILABLE:
            raise ImportError(
                "vLLM is not installed. Install with: pip install vllm"
            )

        self.config = config
        self.engines: dict[str, AsyncLLMEngine] = {}
        self.loaded_models: dict[str, LoadedModel] = {}
        self.tokenizers: dict[str, Any] = {}  # Store tokenizers for chat templates
        self.model_paths: dict[str, str] = {}
        self.active_requests: set[str] = set()
        self._startup_stage_recorder: Callable[[str], None] | None = None
        self._startup_metadata_recorder: Callable[[str, dict[str, Any]], None] | None = None

    def set_startup_stage_recorder(
        self,
        recorder: Callable[[str], None] | None,
    ) -> None:
        """Install an optional callback for detailed startup-stage reporting."""
        self._startup_stage_recorder = recorder

    def set_startup_metadata_recorder(
        self,
        recorder: Callable[[str, dict[str, Any]], None] | None,
    ) -> None:
        """Install an optional callback for startup metadata reporting."""
        self._startup_metadata_recorder = recorder

    def _record_stage(self, stage: str) -> None:
        """Record a startup substage if a recorder is installed."""
        if self._startup_stage_recorder is not None:
            self._startup_stage_recorder(stage)

    def _record_metadata(self, key: str, payload: dict[str, Any]) -> None:
        """Record startup metadata if a recorder is installed."""
        if self._startup_metadata_recorder is not None:
            self._startup_metadata_recorder(key, payload)

    async def warm_model_runtime(self, model_id: str) -> None:
        """Warm deferred tokenizer/chat-template state after readiness."""
        self._record_stage("tokenizer_warmup_started")
        await asyncio.to_thread(self._get_tokenizer, model_id)
        self._record_stage("tokenizer_warmup_finished")

    async def load_model(self, model_config: ModelConfig) -> LoadedModel:
        """Load a model using vLLM."""
        model_path = model_config.model_path or model_config.model_id
        cache_probe = self._collect_model_cache_probe(model_config.model_id, model_path)
        self._record_metadata("model_loads", {model_config.model_id: cache_probe})
        logger.info(
            "Model load cache probe",
            model_id=model_config.model_id,
            model_path=model_path,
            model_source=cache_probe["model_source"],
            local_model_path_exists=cache_probe["local_model_path_exists"],
            inferred_hf_repo_cache_exists=cache_probe["inferred_hf_repo_cache_exists"],
            inferred_hf_snapshot_count=cache_probe["inferred_hf_snapshot_count"],
        )

        engine_kwargs: dict = {
            "model": model_path,
            "revision": model_config.revision,
            "tensor_parallel_size": self.config.vllm_tensor_parallel_size,
            "gpu_memory_utilization": self.config.vllm_gpu_memory_utilization,
            "max_model_len": self.config.vllm_max_model_len,
            "quantization": model_config.quantization,
            "trust_remote_code": True,
            "enable_prefix_caching": self.config.vllm_enable_prefix_caching,
            "enable_chunked_prefill": self.config.vllm_enable_chunked_prefill,
        }

        optional_engine_kwargs: dict[str, Any] = {}
        if self.config.vllm_max_num_batched_tokens is not None:
            optional_engine_kwargs["max_num_batched_tokens"] = self.config.vllm_max_num_batched_tokens

        if self.config.vllm_max_num_seqs is not None:
            optional_engine_kwargs["max_num_seqs"] = self.config.vllm_max_num_seqs

        if self.config.vllm_swap_space is not None:
            optional_engine_kwargs["swap_space"] = self.config.vllm_swap_space

        if self.config.vllm_enforce_eager:
            optional_engine_kwargs["enforce_eager"] = True

        if self.config.vllm_num_scheduler_steps > 0:
            optional_engine_kwargs["num_scheduler_steps"] = self.config.vllm_num_scheduler_steps

        spec_model = self.config.vllm_speculative_model.strip()
        num_spec_tokens = self.config.vllm_num_speculative_tokens
        if spec_model and num_spec_tokens > 0:
            optional_engine_kwargs["speculative_model"] = spec_model
            optional_engine_kwargs["num_speculative_tokens"] = num_spec_tokens
            if spec_model == "[ngram]" and self.config.vllm_ngram_prompt_lookup_num_tokens > 0:
                optional_engine_kwargs["ngram_prompt_lookup_num_tokens"] = (
                    self.config.vllm_ngram_prompt_lookup_num_tokens
                )

        supported_kwargs = set(inspect.signature(AsyncEngineArgs).parameters)
        for key, value in optional_engine_kwargs.items():
            if key in supported_kwargs:
                engine_kwargs[key] = value

        engine_args = AsyncEngineArgs(**engine_kwargs)

        self._record_stage("vllm_engine_init_started")
        engine = AsyncLLMEngine.from_engine_args(engine_args)
        self._record_stage("vllm_engine_init_finished")
        self.engines[model_config.model_id] = engine
        self.model_paths[model_config.model_id] = model_path

        # Get model config for metadata
        # vLLM V1: model_config is a property, not an async method
        model_cfg = engine.model_config

        # Defer tokenizer creation until the first request that needs chat templating.
        self.tokenizers[model_config.model_id] = _TOKENIZER_UNINITIALIZED
        self._record_stage("tokenizer_load_deferred")

        loaded = LoadedModel(
            model_id=model_config.model_id,
            version=model_config.revision or "latest",
            loaded_at=datetime.now(),
            memory_bytes=self._estimate_memory(model_cfg),
            max_batch_size=model_config.max_batch_size,
            max_sequence_length=model_cfg.max_model_len,
        )
        self.loaded_models[model_config.model_id] = loaded
        return loaded

    async def unload_model(self, model_id: str) -> bool:
        """Unload a model."""
        if model_id in self.engines:
            # vLLM doesn't have explicit unload, we just remove reference
            del self.engines[model_id]
            del self.loaded_models[model_id]
            self.tokenizers.pop(model_id, None)
            self.model_paths.pop(model_id, None)
            return True
        return False

    def is_model_loaded(self, model_id: str) -> bool:
        """Check if model is loaded."""
        return model_id in self.engines

    def get_loaded_models(self) -> list[LoadedModel]:
        """Get loaded models."""
        return list(self.loaded_models.values())

    async def infer(self, request: InferenceRequest) -> InferenceResponse:
        """Run inference with vLLM."""
        start_time = datetime.now()

        engine = self.engines.get(request.model_id)
        if engine is None:
            raise ValueError(f"Model {request.model_id} not loaded")

        self.active_requests.add(request.request_id)

        try:
            # Build prompt from messages
            prompt = self._build_prompt(request)

            # Create sampling params
            sampling_params = SamplingParams(
                **request.parameters.to_sampling_params()
            )

            # Generate — capture first-token time on the first iteration.
            results: list[RequestOutput] = []
            first_token_time: datetime | None = None
            async for output in engine.generate(
                prompt,
                sampling_params,
                request.request_id,
            ):
                if first_token_time is None:
                    first_token_time = datetime.now()
                results.append(output)

            final_output = results[-1]
            completion_output = final_output.outputs[0]

            end_time = datetime.now()
            latency_ms = int((end_time - start_time).total_seconds() * 1000)
            ttft_ms = int((first_token_time - start_time).total_seconds() * 1000) if first_token_time else latency_ms

            return InferenceResponse(
                request_id=request.request_id,
                model_id=request.model_id,
                choices=[
                    Choice(
                        index=0,
                        message=Message(
                            role=Role.ASSISTANT,
                            content=completion_output.text,
                        ),
                        finish_reason=self._map_finish_reason(
                            completion_output.finish_reason
                        ),
                    )
                ],
                usage=UsageStats(
                    prompt_tokens=len(final_output.prompt_token_ids),
                    completion_tokens=len(completion_output.token_ids),
                    total_tokens=(
                        len(final_output.prompt_token_ids)
                        + len(completion_output.token_ids)
                    ),
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
        first_token_time: datetime | None = None

        engine = self.engines.get(request.model_id)
        if engine is None:
            raise ValueError(f"Model {request.model_id} not loaded")

        self.active_requests.add(request.request_id)

        try:
            prompt = self._build_prompt(request)
            sampling_params = SamplingParams(
                **request.parameters.to_sampling_params()
            )

            prev_text = ""
            chunk_index = 0
            prompt_tokens = 0

            async for output in engine.generate(
                prompt,
                sampling_params,
                request.request_id,
            ):
                if first_token_time is None:
                    first_token_time = datetime.now()
                    prompt_tokens = len(output.prompt_token_ids)

                completion_output = output.outputs[0]
                new_text = completion_output.text[len(prev_text):]
                prev_text = completion_output.text

                if new_text:
                    is_finished = completion_output.finish_reason is not None
                    completion_tokens = len(completion_output.token_ids)

                    yield TokenChunk(
                        request_id=request.request_id,
                        index=chunk_index,
                        delta=new_text,
                        finish_reason=(
                            self._map_finish_reason(completion_output.finish_reason)
                            if is_finished
                            else None
                        ),
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
        if request_id in self.active_requests:
            # vLLM supports abort
            for engine in self.engines.values():
                await engine.abort(request_id)
            return True
        return False

    def get_memory_usage(self) -> tuple[int, int]:
        """Get GPU memory usage."""
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
                used = max(torch.cuda.memory_allocated(), torch.cuda.memory_reserved())
                total = torch.cuda.get_device_properties(0).total_memory
                return used, total
        except ImportError:
            pass
        return 0, 0

    def _build_prompt(self, request: InferenceRequest) -> str:
        """Build prompt from messages using the model's chat template."""
        # Convert messages to the format expected by tokenizer
        messages = [
            {"role": msg.role.value, "content": msg.content}
            for msg in request.messages
        ]

        # Try to use tokenizer's chat template
        tokenizer = self._get_tokenizer(request.model_id)
        if tokenizer is not None and hasattr(tokenizer, 'apply_chat_template'):
            try:
                prompt = tokenizer.apply_chat_template(
                    messages,
                    tokenize=False,
                    add_generation_prompt=True
                )
                return prompt
            except Exception:
                pass

        # Fallback: Mistral-style format
        # Format: <s>[INST] {user_message} [/INST] {assistant_response}</s>[INST] {next_user} [/INST]
        parts = []
        system_prompt = ""

        i = 0
        while i < len(request.messages):
            msg = request.messages[i]

            if msg.role == Role.SYSTEM:
                system_prompt = msg.content
                i += 1
                continue

            if msg.role == Role.USER:
                user_content = msg.content
                if system_prompt:
                    user_content = f"{system_prompt}\n\n{user_content}"
                    system_prompt = ""

                # Check if next message is assistant
                assistant_content = ""
                if i + 1 < len(request.messages) and request.messages[i + 1].role == Role.ASSISTANT:
                    assistant_content = request.messages[i + 1].content
                    i += 1

                if assistant_content:
                    parts.append(f"<s>[INST] {user_content} [/INST] {assistant_content}</s>")
                else:
                    parts.append(f"<s>[INST] {user_content} [/INST]")

            i += 1

        return "".join(parts)

    def _get_tokenizer(self, model_id: str) -> Any:
        """Load a tokenizer lazily so startup is not blocked on chat-template setup."""
        tokenizer = self.tokenizers.get(model_id)
        if tokenizer is not _TOKENIZER_UNINITIALIZED:
            return tokenizer

        model_path = self.model_paths.get(model_id)
        if not model_path:
            self.tokenizers[model_id] = None
            return None

        self._record_stage("tokenizer_load_started")
        try:
            from transformers import AutoTokenizer

            tokenizer = AutoTokenizer.from_pretrained(model_path, trust_remote_code=True)
        except Exception:
            tokenizer = None
        self.tokenizers[model_id] = tokenizer
        self._record_stage("tokenizer_load_finished")
        return tokenizer

    def _collect_model_cache_probe(self, model_id: str, model_path: str) -> dict[str, Any]:
        """Collect lightweight cache/path diagnostics for startup analysis."""
        resolved_model_path = Path(model_path).expanduser()
        local_model_path_exists = resolved_model_path.exists()
        local_model_path_is_dir = resolved_model_path.is_dir()
        local_model_config_exists = (resolved_model_path / "config.json").exists() if local_model_path_is_dir else False
        local_tokenizer_config_exists = (
            (resolved_model_path / "tokenizer_config.json").exists() if local_model_path_is_dir else False
        )

        huggingface_hub_cache = (
            os.getenv("HUGGINGFACE_HUB_CACHE")
            or os.getenv("TRANSFORMERS_CACHE")
            or self._default_huggingface_hub_cache()
        )
        inferred_repo_cache_dir = self._infer_huggingface_repo_cache_dir(model_path, huggingface_hub_cache)
        inferred_repo_cache_exists = inferred_repo_cache_dir.exists() if inferred_repo_cache_dir is not None else False
        inferred_snapshot_dir = self._latest_snapshot_dir(inferred_repo_cache_dir)
        inferred_snapshot_count = self._snapshot_count(inferred_repo_cache_dir)

        return {
            "model_id": model_id,
            "requested_model_path": model_path,
            "model_source": "local_path" if local_model_path_exists else "huggingface_repo",
            "local_model_path": str(resolved_model_path),
            "local_model_path_exists": local_model_path_exists,
            "local_model_path_is_dir": local_model_path_is_dir,
            "local_model_has_config_json": local_model_config_exists,
            "local_model_has_tokenizer_config_json": local_tokenizer_config_exists,
            "cache_dirs": {
                "hf_home": os.getenv("HF_HOME", ""),
                "huggingface_hub": huggingface_hub_cache or "",
                "transformers": os.getenv("TRANSFORMERS_CACHE", ""),
                "torch": os.getenv("TORCH_HOME", ""),
            },
            "inferred_hf_repo_cache_dir": str(inferred_repo_cache_dir) if inferred_repo_cache_dir is not None else None,
            "inferred_hf_repo_cache_exists": inferred_repo_cache_exists,
            "inferred_hf_snapshot_count": inferred_snapshot_count,
            "inferred_latest_snapshot_dir": str(inferred_snapshot_dir) if inferred_snapshot_dir is not None else None,
            "inferred_latest_snapshot_has_config_json": (
                (inferred_snapshot_dir / "config.json").exists() if inferred_snapshot_dir is not None else False
            ),
            "inferred_latest_snapshot_has_tokenizer_config_json": (
                (inferred_snapshot_dir / "tokenizer_config.json").exists()
                if inferred_snapshot_dir is not None
                else False
            ),
        }

    def _default_huggingface_hub_cache(self) -> str:
        hf_home = os.getenv("HF_HOME", "")
        if not hf_home:
            return ""
        return str(Path(hf_home).expanduser() / "hub")

    def _infer_huggingface_repo_cache_dir(self, model_path: str, hub_cache: str) -> Path | None:
        if not hub_cache or "/" not in model_path:
            return None
        normalized_repo = model_path.replace("/", "--")
        return Path(hub_cache).expanduser() / f"models--{normalized_repo}"

    def _latest_snapshot_dir(self, repo_cache_dir: Path | None) -> Path | None:
        if repo_cache_dir is None:
            return None
        snapshots_dir = repo_cache_dir / "snapshots"
        if not snapshots_dir.exists() or not snapshots_dir.is_dir():
            return None
        snapshot_dirs = sorted((path for path in snapshots_dir.iterdir() if path.is_dir()), key=lambda path: path.name)
        if not snapshot_dirs:
            return None
        return snapshot_dirs[-1]

    def _snapshot_count(self, repo_cache_dir: Path | None) -> int:
        if repo_cache_dir is None:
            return 0
        snapshots_dir = repo_cache_dir / "snapshots"
        if not snapshots_dir.exists() or not snapshots_dir.is_dir():
            return 0
        return sum(1 for path in snapshots_dir.iterdir() if path.is_dir())

    def _map_finish_reason(self, reason: str | None) -> FinishReason:
        """Map vLLM finish reason to our enum."""
        if reason is None:
            return FinishReason.STOP
        reason_map = {
            "stop": FinishReason.STOP,
            "length": FinishReason.LENGTH,
            "abort": FinishReason.ERROR,
        }
        return reason_map.get(reason, FinishReason.STOP)

    def _estimate_memory(self, model_config: Any) -> int:
        """Estimate model memory usage."""
        # Rough estimate based on model size
        # In production, query actual GPU memory
        try:
            import torch
            if torch.cuda.is_available():
                return max(torch.cuda.memory_allocated(), torch.cuda.memory_reserved())
        except ImportError:
            pass
        return 8 * 1024 * 1024 * 1024  # Default 8GB estimate
