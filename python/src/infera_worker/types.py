"""Core types for Infera Worker."""

from __future__ import annotations

from dataclasses import dataclass, field
from datetime import datetime
from enum import Enum


class Role(str, Enum):
    SYSTEM = "system"
    USER = "user"
    ASSISTANT = "assistant"
    TOOL = "tool"


class FinishReason(str, Enum):
    STOP = "stop"
    LENGTH = "length"
    CONTENT_FILTER = "content_filter"
    ERROR = "error"
    TOOL_CALLS = "tool_calls"


class Priority(int, Enum):
    LOW = 1
    NORMAL = 2
    HIGH = 3


class WorkerState(str, Enum):
    INITIALIZING = "initializing"
    READY = "ready"
    BUSY = "busy"
    DRAINING = "draining"
    SHUTTING_DOWN = "shutting_down"
    ERROR = "error"


@dataclass
class FunctionCall:
    name: str
    arguments: str


@dataclass
class ToolCall:
    id: str
    type: str
    function: FunctionCall


@dataclass
class ToolDefinition:
    type: str
    function: dict  # JSON schema, pass through as-is


@dataclass
class ToolCallChunkDelta:
    index: int
    id: str | None = None
    type: str | None = None
    function: dict | None = None  # {"name": "...", "arguments": "..."}


@dataclass
class Message:
    role: Role
    content: str
    name: str | None = None
    tool_calls: list[ToolCall] | None = None
    tool_call_id: str | None = None


@dataclass
class InferenceParameters:
    max_tokens: int = 256
    temperature: float = 1.0
    top_p: float = 1.0
    top_k: int | None = None
    stop_sequences: list[str] = field(default_factory=list)
    presence_penalty: float = 0.0
    frequency_penalty: float = 0.0
    seed: int | None = None

    def to_sampling_params(self) -> dict:
        params = {
            "max_tokens": self.max_tokens,
            "temperature": self.temperature,
            "top_p": self.top_p,
        }
        if self.top_k:
            params["top_k"] = self.top_k
        if self.stop_sequences:
            params["stop"] = self.stop_sequences
        return params


@dataclass
class InferenceRequest:
    request_id: str
    model_id: str
    messages: list[Message]
    parameters: InferenceParameters = field(default_factory=InferenceParameters)
    stream: bool = False
    priority: Priority = Priority.NORMAL
    metadata: dict[str, str] = field(default_factory=dict)
    created_at: datetime = field(default_factory=datetime.now)
    tools: list[ToolDefinition] | None = None
    tool_choice: object | None = None  # string or dict, pass through

    def token_estimate(self) -> int:
        return sum(len(msg.content) for msg in self.messages) // 4


@dataclass
class UsageStats:
    prompt_tokens: int
    completion_tokens: int
    total_tokens: int


@dataclass
class LatencyStats:
    queue_ms: int
    inference_ms: int
    total_ms: int
    time_to_first_token_ms: int


@dataclass
class Choice:
    index: int
    message: Message
    finish_reason: FinishReason


@dataclass
class InferenceResponse:
    request_id: str
    model_id: str
    choices: list[Choice]
    usage: UsageStats
    latency: LatencyStats
    created_at: datetime = field(default_factory=datetime.now)


@dataclass
class TokenChunk:
    request_id: str
    index: int
    delta: str
    finish_reason: FinishReason | None = None
    usage: UsageStats | None = None
    tool_calls: list[ToolCallChunkDelta] | None = None
    created_at: datetime = field(default_factory=datetime.now)

    def is_final(self) -> bool:
        return self.finish_reason is not None


@dataclass
class LoadedModel:
    model_id: str
    version: str
    loaded_at: datetime
    memory_bytes: int
    max_batch_size: int
    max_sequence_length: int


@dataclass
class WorkerStats:
    queue_depth: int = 0
    active_requests: int = 0
    gpu_utilization: float = 0.0
    memory_used_bytes: int = 0
    memory_total_bytes: int = 0
    requests_per_second: float = 0.0
    avg_latency_ms: float = 0.0
    p50_latency_ms: float = 0.0
    p99_latency_ms: float = 0.0
    error_rate: float = 0.0
    updated_at: datetime = field(default_factory=datetime.now)