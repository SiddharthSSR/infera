"""Typed API and scenario models for Hermes API testing."""

from __future__ import annotations

from dataclasses import dataclass
from typing import Any, Generic, Literal, TypeVar

from pydantic import BaseModel, ConfigDict, Field

RunMode = Literal["operations", "research", "multimodal"]
AnalysisDepth = Literal["standard", "deep"]
RunStatus = Literal["queued", "running", "succeeded", "failed", "canceled"]
StepType = Literal["tool_call", "tool_result", "final", "error"]


class SchemaModel(BaseModel):
    """Base model that ignores forward-compatible fields from the API."""

    model_config = ConfigDict(extra="ignore")


class ToolDescriptor(SchemaModel):
    name: str
    description: str
    modes: list[RunMode] = Field(default_factory=list)


class AgentDescriptor(SchemaModel):
    id: str
    name: str
    description: str
    default_max_steps: int
    tools: list[ToolDescriptor]


class AgentsListResponse(SchemaModel):
    agents: list[AgentDescriptor]
    default_agent_id: str


class RunRecord(SchemaModel):
    id: str
    workspace_id: str
    created_by_key_id: str | None = None
    agent_id: str
    mode: RunMode
    analysis_depth: AnalysisDepth
    model: str
    input: str
    status: RunStatus
    max_steps: int
    current_step: int
    final_output: str | None = None
    failure_reason: str | None = None
    created_at: str
    updated_at: str
    started_at: str | None = None
    finished_at: str | None = None


class RunStep(SchemaModel):
    id: int
    run_id: str
    index: int
    type: StepType
    tool_name: str | None = None
    payload: Any
    created_at: str


class Attachment(SchemaModel):
    id: str
    workspace_id: str
    created_by_key_id: str | None = None
    run_id: str | None = None
    file_name: str
    mime_type: str
    size_bytes: int
    width: int | None = None
    height: int | None = None
    sha256: str
    created_at: str


class ResearchSource(SchemaModel):
    title: str
    url: str
    domain: str
    snippet: str | None = None


class RunDetailResponse(SchemaModel):
    run: RunRecord
    steps: list[RunStep]
    attachments: list[Attachment] = Field(default_factory=list)
    sources: list[ResearchSource] = Field(default_factory=list)
    timed_out: bool = False


class RunEnvelope(SchemaModel):
    run: RunRecord


class AttachmentEnvelope(SchemaModel):
    attachment: Attachment


class RunsListResponse(SchemaModel):
    runs: list[RunRecord]
    total: int


class LiveModelRecord(SchemaModel):
    id: str
    loaded: bool = False
    object: str | None = None
    owned_by: str | None = None
    family: str | None = None
    quantization: str | None = None


class ModelsListResponse(SchemaModel):
    data: list[LiveModelRecord]
    object: str | None = None


class ErrorEnvelope(SchemaModel):
    type: str
    message: str


class ErrorResponse(SchemaModel):
    error: ErrorEnvelope


class ScenarioCase(SchemaModel):
    """Prompt-driven scenario for a Hermes run."""

    id: str
    title: str
    category: str
    prompt: str
    mode: RunMode = "operations"
    analysis_depth: AnalysisDepth = "standard"
    max_steps: int = 8
    needs_attachment: bool = False
    expected_status: RunStatus = "succeeded"
    expected_tools: list[str] = Field(default_factory=list)
    allowed_tools: list[str] = Field(default_factory=list)
    disallowed_tools: list[str] = Field(default_factory=list)
    expected_arguments: dict[str, dict[str, Any]] = Field(default_factory=dict)
    response_keywords: list[str] = Field(default_factory=list)
    final_output: str
    mock_behavior: str = "success"
    notes: str = ""


class ConditionCase(SchemaModel):
    """Low-level request/response and failure-mode coverage."""

    id: str
    title: str
    category: str
    operation: Literal[
        "create_run",
        "upload_attachment",
        "wait_for_run",
        "get_run_detail",
        "external_run_wait",
    ]
    prompt: str | None = None
    mode: RunMode = "operations"
    analysis_depth: AnalysisDepth = "standard"
    needs_attachment: bool = False
    request_body: dict[str, Any] = Field(default_factory=dict)
    expected_status_code: int = 200
    expected_error_type: str | None = None
    expected_exception: str | None = None
    retry_on_rate_limit: bool = False
    expected_tools: list[str] = Field(default_factory=list)
    expected_arguments: dict[str, dict[str, Any]] = Field(default_factory=dict)
    response_keywords: list[str] = Field(default_factory=list)
    final_output: str | None = None
    mock_behavior: str = "request_validation"
    notes: str = ""


class ScenarioCatalog(SchemaModel):
    tool_cases: list[ScenarioCase]
    prompt_cases: list[ScenarioCase]
    condition_cases: list[ConditionCase]
    regression_cases: list[ScenarioCase]


ResponseModelT = TypeVar("ResponseModelT", bound=BaseModel)


@dataclass(slots=True)
class APICallResult(Generic[ResponseModelT]):
    """HTTP result plus parsed payload for resilient assertions."""

    status_code: int
    headers: dict[str, str]
    text: str
    json_payload: Any | None
    data: ResponseModelT | None


@dataclass(slots=True)
class ScenarioReportRecord:
    """Compact per-scenario report row."""

    test_name: str
    scenario_id: str
    category: str
    prompt: str
    expected_tools: list[str]
    actual_tools: list[str]
    outcome: str
    reason: str
