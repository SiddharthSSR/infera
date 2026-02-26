"""Configuration for Infera Worker."""

from pydantic import Field
from pydantic_settings import BaseSettings


class WorkerConfig(BaseSettings):
    """Worker configuration loaded from environment variables."""

    worker_id: str = Field(default="", description="Unique worker identifier")
    grpc_port: int = Field(default=50051, description="gRPC server port")
    router_address: str = Field(default="localhost:50052", description="Router address")
    
    max_concurrent_requests: int = Field(default=32)
    max_batch_size: int = Field(default=8)
    request_timeout_ms: int = Field(default=30000)
    
    model_cache_size: int = Field(default=2)
    preload_models: list[str] = Field(default_factory=list)
    
    device: str = Field(default="auto")
    gpu_memory_fraction: float = Field(default=0.9)
    
    health_report_interval_ms: int = Field(default=5000)
    engine: str = Field(default="vllm")
    
    vllm_tensor_parallel_size: int = Field(default=1)
    vllm_gpu_memory_utilization: float = Field(default=0.9)
    
    log_level: str = Field(default="INFO")
    log_format: str = Field(default="json")

    model_config = {"env_prefix": "INFERA_", "env_file": ".env"}


class ModelConfig(BaseSettings):
    """Per-model configuration."""

    model_id: str
    model_path: str | None = None
    revision: str | None = None
    quantization: str | None = None
    max_batch_size: int = 8
    max_sequence_length: int = 4096

    model_config = {"extra": "allow"}