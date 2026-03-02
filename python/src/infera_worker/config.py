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
    
    # Capacity
    max_concurrent_requests: int = Field(default=32, description="Max concurrent requests")
    max_batch_size: int = Field(default=8, description="Max batch size")
    request_timeout_ms: int = Field(default=30000, description="Request timeout in ms")
    
    # Model management
    model_cache_size: int = Field(default=2, description="Max models in memory")
    # NOTE: preload_models is handled via computed_field to avoid pydantic-settings JSON parsing issues
    
    # GPU/Device
    device: str = Field(default="auto", description="Device: auto, cuda, mps, cpu")
    gpu_memory_fraction: float = Field(default=0.9, description="GPU memory fraction to use")
    
    # Health reporting
    health_report_interval_ms: int = Field(default=5000, description="Health report interval")
    
    # Inference engine
    engine: str = Field(default="vllm", description="Inference engine: vllm, mlx, mock")
    
    # vLLM specific
    vllm_tensor_parallel_size: int = Field(default=1, description="Tensor parallel size")
    vllm_gpu_memory_utilization: float = Field(default=0.9, description="GPU memory utilization")
    vllm_max_model_len: int | None = Field(default=None, description="Max model length")
    
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