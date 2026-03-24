"""Configuration for Infera Worker."""

import json
import os
from typing import Any
from pydantic import Field, computed_field
from pydantic_settings import BaseSettings, SettingsConfigDict


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
    engine: str = Field(default="vllm", description="Inference engine: vllm, mlx, mock")
    
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
    
    # Logging
    log_level: str = Field(default="INFO", description="Log level")
    log_format: str = Field(default="json", description="Log format: json, console")

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
