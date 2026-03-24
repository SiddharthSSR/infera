"""Configuration for Infera Worker."""

from dataclasses import dataclass
import json
import os
from typing import Any
from pydantic import Field, computed_field, field_validator
from pydantic_settings import BaseSettings, SettingsConfigDict


SUPPORTED_INFERENCE_ENGINES = (
    "vllm",
    "sglang",
    "tensorrt_llm",
    "mock",
)

_ENGINE_ALIASES = {
    "vllm": "vllm",
    "mock": "mock",
    "sglang": "sglang",
    "tensorrt_llm": "tensorrt_llm",
    "tensorrt-llm": "tensorrt_llm",
    "tensorrt_llm": "tensorrt_llm",
    "trtllm": "tensorrt_llm",
    "trt-llm": "tensorrt_llm",
}


def normalize_engine_name(value: str) -> str:
    """Normalize user-facing engine aliases into a canonical engine ID."""
    normalized = _ENGINE_ALIASES.get(str(value).strip().lower())
    if normalized is None:
        supported = ", ".join(SUPPORTED_INFERENCE_ENGINES)
        raise ValueError(f"Unsupported inference engine {value!r}. Supported engines: {supported}")
    return normalized


@dataclass(frozen=True, slots=True)
class VLLMRuntimeConfig:
    tensor_parallel_size: int
    gpu_memory_utilization: float
    max_model_len: int | None
    enable_prefix_caching: bool
    enable_chunked_prefill: bool
    max_num_batched_tokens: int | None
    max_num_seqs: int | None
    swap_space: float | None
    enforce_eager: bool
    num_scheduler_steps: int
    speculative_model: str
    num_speculative_tokens: int
    ngram_prompt_lookup_num_tokens: int


@dataclass(frozen=True, slots=True)
class SGLangRuntimeConfig:
    tp_size: int
    mem_fraction_static: float
    context_length: int | None
    chunked_prefill_size: int | None
    max_running_requests: int | None
    schedule_policy: str | None
    attention_backend: str | None
    sampling_backend: str | None
    disable_cuda_graph: bool


@dataclass(frozen=True, slots=True)
class TensorRTLLMRuntimeConfig:
    tensor_parallel_size: int
    max_batch_size: int | None
    max_num_tokens: int | None
    max_beam_width: int | None
    kv_cache_free_gpu_memory_fraction: float | None
    enable_chunked_context: bool
    backend: str | None


class WorkerConfig(BaseSettings):
    """Worker configuration loaded from environment variables."""

    # Worker identity
    worker_id: str = Field(default="", description="Unique worker identifier")
    
    # Network
    http_port: int = Field(default=8081, description="HTTP server port")
    grpc_port: int = Field(default=50051, description="gRPC server port")
    router_address: str = Field(default="", description="Router/Gateway address")
    worker_address: str = Field(default="", description="Public address of this worker (for registration)")
    vault_address: str = Field(default="localhost:50053", description="Vault address")
    worker_shared_token: str = Field(default="", description="Shared token for gateway worker auth")
    
    # Capacity
    max_concurrent_requests: int = Field(default=32, description="Max concurrent requests")
    max_batch_size: int = Field(default=8, description="Max batch size")
    request_timeout_ms: int = Field(default=30000, description="Request timeout in ms")
    
    # Model management
    model_cache_size: int = Field(default=2, description="Max models in memory")
    # Optional constructor-level preload list; env parsing still handled via computed field below.
    preload_models_input: list[str] = Field(default_factory=list, alias="preload_models")
    # NOTE: preload_models is handled via computed_field to avoid pydantic-settings JSON parsing issues
    
    # GPU/Device
    device: str = Field(default="auto", description="Device: auto, cuda, mps, cpu")
    gpu_memory_fraction: float = Field(default=0.9, description="GPU memory fraction to use")
    
    # Health reporting
    health_report_interval_ms: int = Field(default=5000, description="Health report interval")
    drain_timeout_s: int = Field(default=30, description="Seconds to wait for in-flight requests to finish on graceful shutdown")
    
    # Inference engine
    engine: str = Field(
        default="vllm",
        description="Inference engine: vllm, sglang, tensorrt_llm, mock",
    )
    
    # vLLM specific
    vllm_tensor_parallel_size: int = Field(default=1, description="Tensor parallel size")
    vllm_gpu_memory_utilization: float = Field(default=0.9, description="GPU memory utilization")
    vllm_max_model_len: int | None = Field(default=None, description="Max model length")

    # vLLM performance flags
    vllm_enable_prefix_caching: bool = Field(default=True, description="Enable automatic prefix caching (KV cache reuse)")
    vllm_enable_chunked_prefill: bool = Field(default=True, description="Enable chunked prefill to avoid blocking decode batches")
    vllm_max_num_batched_tokens: int | None = Field(default=None, description="Max tokens per prefill chunk (None = vLLM default)")
    vllm_max_num_seqs: int | None = Field(default=None, description="Max concurrent sequences per iteration (None = vLLM default)")
    vllm_swap_space: float | None = Field(default=None, description="CPU swap space for KV spill in GiB (None = vLLM default)")
    vllm_enforce_eager: bool = Field(default=False, description="Disable CUDA graphs and force eager execution")
    vllm_num_scheduler_steps: int = Field(default=0, description="Multi-step scheduling steps per iteration (0 = disabled)")

    # vLLM speculative decoding
    # Set to a HuggingFace model ID for draft-model mode, or "[ngram]" for ngram mode.
    # Leave empty (default) to disable speculative decoding.
    vllm_speculative_model: str = Field(default="", description="Draft model ID or '[ngram]' for speculative decoding")
    vllm_num_speculative_tokens: int = Field(default=0, description="Tokens to speculate per step (0 = disabled)")
    vllm_ngram_prompt_lookup_num_tokens: int = Field(default=0, description="Ngram look-back window (ngram mode only)")

    # SGLang specific
    sglang_tp_size: int = Field(default=1, description="Tensor parallel size")
    sglang_mem_fraction_static: float = Field(default=0.9, description="Static GPU memory fraction")
    sglang_context_length: int | None = Field(default=None, description="Context length override")
    sglang_chunked_prefill_size: int | None = Field(default=None, description="Chunked prefill size")
    sglang_max_running_requests: int | None = Field(default=None, description="Max running requests")
    sglang_schedule_policy: str | None = Field(default=None, description="Scheduling policy")
    sglang_attention_backend: str | None = Field(default=None, description="Attention backend")
    sglang_sampling_backend: str | None = Field(default=None, description="Sampling backend")
    sglang_disable_cuda_graph: bool = Field(default=False, description="Disable CUDA graph capture")

    # TensorRT-LLM specific
    tensorrt_llm_tensor_parallel_size: int = Field(default=1, description="Tensor parallel size")
    tensorrt_llm_max_batch_size: int | None = Field(default=None, description="Max batch size")
    tensorrt_llm_max_num_tokens: int | None = Field(default=None, description="Max tokens per iteration")
    tensorrt_llm_max_beam_width: int | None = Field(default=None, description="Max beam width")
    tensorrt_llm_kv_cache_free_gpu_memory_fraction: float | None = Field(
        default=None,
        description="Fraction of GPU memory reserved as free while sizing KV cache",
    )
    tensorrt_llm_enable_chunked_context: bool = Field(
        default=True,
        description="Enable chunked context/prefill where supported",
    )
    tensorrt_llm_backend: str | None = Field(
        default=None,
        description="Optional TensorRT-LLM backend selector",
    )
    
    # Logging
    log_level: str = Field(default="INFO", description="Log level")
    log_format: str = Field(default="json", description="Log format: json, console")

    @field_validator("engine", mode="before")
    @classmethod
    def validate_engine(cls, value: str) -> str:
        """Normalize engine aliases and fail fast on unsupported values."""
        return normalize_engine_name(value)

    @computed_field
    @property
    def preload_models(self) -> list[str]:
        """
        Get preload_models from environment variable.
        
        This is a computed field that reads directly from os.environ
        to avoid pydantic-settings trying to JSON parse the value.
        
        Supports:
        - Empty string: ""
        - Comma-separated: "model1,model2"
        - JSON array: '["model1","model2"]'
        """
        if self.preload_models_input:
            return [m for m in self.preload_models_input if m]

        value = os.environ.get("INFERA_PRELOAD_MODELS", "")
        
        if not value:
            return []
        
        value = value.strip()
        if not value:
            return []
        
        # Try JSON array first
        if value.startswith("["):
            try:
                parsed = json.loads(value)
                if isinstance(parsed, list):
                    return [str(m) for m in parsed if m]
            except json.JSONDecodeError:
                pass
        
        # Comma-separated
        return [m.strip() for m in value.split(",") if m.strip()]

    def gateway_headers(self) -> dict[str, str]:
        """Headers used for worker calls to the gateway."""
        if not self.worker_shared_token:
            return {}
        return {"X-Worker-Token": self.worker_shared_token}

    @property
    def vllm_runtime(self) -> VLLMRuntimeConfig:
        """Typed vLLM runtime configuration view."""
        return VLLMRuntimeConfig(
            tensor_parallel_size=self.vllm_tensor_parallel_size,
            gpu_memory_utilization=self.vllm_gpu_memory_utilization,
            max_model_len=self.vllm_max_model_len,
            enable_prefix_caching=self.vllm_enable_prefix_caching,
            enable_chunked_prefill=self.vllm_enable_chunked_prefill,
            max_num_batched_tokens=self.vllm_max_num_batched_tokens,
            max_num_seqs=self.vllm_max_num_seqs,
            swap_space=self.vllm_swap_space,
            enforce_eager=self.vllm_enforce_eager,
            num_scheduler_steps=self.vllm_num_scheduler_steps,
            speculative_model=self.vllm_speculative_model,
            num_speculative_tokens=self.vllm_num_speculative_tokens,
            ngram_prompt_lookup_num_tokens=self.vllm_ngram_prompt_lookup_num_tokens,
        )

    @property
    def sglang_runtime(self) -> SGLangRuntimeConfig:
        """Typed SGLang runtime configuration view."""
        return SGLangRuntimeConfig(
            tp_size=self.sglang_tp_size,
            mem_fraction_static=self.sglang_mem_fraction_static,
            context_length=self.sglang_context_length,
            chunked_prefill_size=self.sglang_chunked_prefill_size,
            max_running_requests=self.sglang_max_running_requests,
            schedule_policy=self.sglang_schedule_policy,
            attention_backend=self.sglang_attention_backend,
            sampling_backend=self.sglang_sampling_backend,
            disable_cuda_graph=self.sglang_disable_cuda_graph,
        )

    @property
    def tensorrt_llm_runtime(self) -> TensorRTLLMRuntimeConfig:
        """Typed TensorRT-LLM runtime configuration view."""
        return TensorRTLLMRuntimeConfig(
            tensor_parallel_size=self.tensorrt_llm_tensor_parallel_size,
            max_batch_size=self.tensorrt_llm_max_batch_size,
            max_num_tokens=self.tensorrt_llm_max_num_tokens,
            max_beam_width=self.tensorrt_llm_max_beam_width,
            kv_cache_free_gpu_memory_fraction=self.tensorrt_llm_kv_cache_free_gpu_memory_fraction,
            enable_chunked_context=self.tensorrt_llm_enable_chunked_context,
            backend=self.tensorrt_llm_backend,
        )

    model_config = SettingsConfigDict(
        env_prefix="INFERA_",
        env_file=".env",
        extra="ignore",
    )


class ModelConfig(BaseSettings):
    """Per-model configuration."""

    model_id: str
    model_path: str | None = None  # Local path or HF repo
    revision: str | None = None
    quantization: str | None = None  # awq, gptq, int8, int4
    max_batch_size: int = 8
    max_sequence_length: int = 4096
    
    model_config = {
        "extra": "allow",
    }
