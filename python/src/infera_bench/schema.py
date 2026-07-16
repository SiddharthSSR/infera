"""Schema models for the generic inference benchmark lab."""

from __future__ import annotations

from datetime import datetime
from typing import Any, Literal

from pydantic import BaseModel, ConfigDict, Field, field_validator, model_validator

CompatibilityStatus = Literal["invalid", "unsupported", "unverified", "blocked", "skipped", "ready"]
ExecutionMode = Literal["provision", "attach"]
StageName = Literal["cold_start", "startup_health", "warm_none", "warm_affinity"]
CacheReuseMode = Literal["none", "affinity"]
RankingObjective = Literal["max_throughput", "lowest_ttft", "best_tpot", "balanced"]
ValueType = Literal["int", "float", "bool", "string"]


class BenchmarkBaseModel(BaseModel):
    """Base model with strict validation and predictable serialization."""

    model_config = ConfigDict(extra="forbid", populate_by_name=True, str_strip_whitespace=True)


class RuntimeParameterSpec(BenchmarkBaseModel):
    id: str
    env_key: str
    value_type: ValueType = "string"
    description: str = ""
    default_value: str | None = None
    min_value: float | None = None
    max_value: float | None = None
    allowed_values: list[str] = Field(default_factory=list)
    supports_sweep: bool = True

    @field_validator("id", "env_key")
    @classmethod
    def validate_required_tokens(cls, value: str) -> str:
        if not value:
            raise ValueError("value must not be empty")
        return value


class EngineSpec(BenchmarkBaseModel):
    id: str
    display_name: str
    adapter: str
    description: str = ""
    runtime_parameters: list[RuntimeParameterSpec] = Field(default_factory=list)
    supported_features: list[str] = Field(default_factory=list)
    unsupported_features: list[str] = Field(default_factory=list)
    default_benchmark_profiles: list[str] = Field(default_factory=list)


class EngineCatalog(BenchmarkBaseModel):
    version: int = 1
    engines: list[EngineSpec]


class ProviderSelector(BenchmarkBaseModel):
    provider: str
    gpu_type: str
    provider_gpu_type_id: str = ""
    allowed_cuda_versions: list[str] = Field(default_factory=list)
    regions: list[str] = Field(default_factory=list)


class AcceleratorSpec(BenchmarkBaseModel):
    kind: str = "gpu"
    family: str
    memory_gb: int
    memory_bandwidth_gbps: int | None = None
    tensor_core: bool = True


class HardwareSpec(BenchmarkBaseModel):
    hardware_id: str
    display_name: str
    aliases: list[str] = Field(default_factory=list)
    accelerators: list[AcceleratorSpec]
    provider_selectors: list[ProviderSelector] = Field(default_factory=list)
    attributes: dict[str, Any] = Field(default_factory=dict)
    notes: list[str] = Field(default_factory=list)


class HardwareCatalog(BenchmarkBaseModel):
    version: int = 1
    hardware: list[HardwareSpec]


class EngineCompatibilityHint(BenchmarkBaseModel):
    status: Literal["supported", "unsupported", "unverified"] = "unverified"
    notes: list[str] = Field(default_factory=list)
    supported_precisions: list[str] = Field(default_factory=list)
    supported_quantizations: list[str] = Field(default_factory=list)


class ModelVariantSpec(BenchmarkBaseModel):
    model_id: str
    display_name: str
    source_uri: str
    revision: str | None = None
    family: str
    parameter_scale: str
    parameter_count_billions: float | None = None
    precision: str
    quantization: str = "none"
    max_context: int
    artifact_kind: str = "hf_model"
    aliases: list[str] = Field(default_factory=list)
    tags: list[str] = Field(default_factory=list)
    engine_compatibility: dict[str, EngineCompatibilityHint] = Field(default_factory=dict)


class ModelCatalog(BenchmarkBaseModel):
    version: int = 1
    models: list[ModelVariantSpec]


class WorkloadProfile(BenchmarkBaseModel):
    id: str
    display_name: str
    description: str = ""
    prompt_preset: str | None = None
    prompt_text: str | None = None
    stream: bool = True
    max_tokens: int = 256
    temperature: float = 0.2
    concurrency: int = 1
    warmup_runs: int = 0
    measured_runs: int = 3
    cache_reuse_modes: list[CacheReuseMode] = Field(default_factory=lambda: ["none"])
    arrival_pattern: str = "closed_loop"
    session_pattern: str = "stateless"
    headers: dict[str, str] = Field(default_factory=dict)
    tags: list[str] = Field(default_factory=list)

    @model_validator(mode="after")
    def validate_prompt_source(self) -> WorkloadProfile:
        if not self.prompt_preset and not self.prompt_text:
            raise ValueError("workload must define prompt_preset or prompt_text")
        return self


class WorkloadCatalog(BenchmarkBaseModel):
    version: int = 1
    workloads: list[WorkloadProfile]


class ObjectiveWeights(BenchmarkBaseModel):
    ttft: float = 0.0
    throughput: float = 0.0
    tpot: float = 0.0
    startup: float = 0.0
    failures: float = 0.0


class BenchmarkProfile(BenchmarkBaseModel):
    id: str
    display_name: str
    description: str = ""
    execution_mode: ExecutionMode = "provision"
    stages: list[StageName] = Field(
        default_factory=lambda: ["cold_start", "startup_health", "warm_none", "warm_affinity"]
    )
    lifecycle_timeout_s: int = 900
    objective_weights: dict[RankingObjective, ObjectiveWeights] = Field(default_factory=dict)
    health_sample_interval_ms: int = 5000
    warm_ready_timeout_s: int = 180
    registry_drain_timeout_s: int = 45
    registry_drain_poll_interval_ms: int = 2000
    benchmark_headers: dict[str, str] = Field(default_factory=dict)


class BenchmarkProfileCatalog(BenchmarkBaseModel):
    version: int = 1
    benchmark_profiles: list[BenchmarkProfile]


class RuntimePresetSpec(BenchmarkBaseModel):
    id: str
    display_name: str
    description: str = ""
    engine_ids: list[str] = Field(default_factory=list)
    parameters: dict[str, Any] = Field(default_factory=dict)
    tags: list[str] = Field(default_factory=list)


class SuiteMatrix(BenchmarkBaseModel):
    engines: list[str]
    hardware: list[str]
    gpu_counts: list[int] = Field(default_factory=lambda: [1])
    models: list[str]
    workloads: list[str]
    benchmark_profiles: list[str]
    runtime_presets: list[str] = Field(default_factory=lambda: ["baseline"])


class SuiteFilter(BenchmarkBaseModel):
    engine: str | None = None
    hardware: str | None = None
    gpu_count: int | None = None
    model: str | None = None
    workload: str | None = None
    benchmark_profile: str | None = None
    runtime_preset: str | None = None


class AttachTargetSpec(BenchmarkBaseModel):
    health_url: str | None = None
    headers: dict[str, str] = Field(default_factory=dict)
    cache_key_prefix: str = "benchmark"


class ExperimentSuite(BenchmarkBaseModel):
    suite_id: str
    description: str = ""
    matrix: SuiteMatrix
    runtime_presets: list[RuntimePresetSpec] = Field(default_factory=list)
    default_runtime_parameters: dict[str, Any] = Field(default_factory=dict)
    include: list[SuiteFilter] = Field(default_factory=list)
    exclude: list[SuiteFilter] = Field(default_factory=list)
    output_root: str = "/tmp/infera-benchmark-lab"
    attach_target: AttachTargetSpec | None = None
    default_provider: str | None = None
    labels: dict[str, str] = Field(default_factory=dict)


class BenchmarkCatalogEnvelope(BenchmarkBaseModel):
    engines: EngineCatalog
    hardware: HardwareCatalog
    models: ModelCatalog
    workloads: WorkloadCatalog
    benchmark_profiles: BenchmarkProfileCatalog


class CompatibilityIssue(BenchmarkBaseModel):
    status: CompatibilityStatus
    message: str
    field: str | None = None


class ResolvedRunSpec(BenchmarkBaseModel):
    suite_id: str
    run_id: str
    engine_id: str
    hardware_id: str
    gpu_count: int
    model_id: str
    workload_id: str
    benchmark_profile_id: str
    runtime_preset_id: str
    provider: str | None = None
    provider_gpu_type_id: str | None = None
    provider_gpu_type: str | None = None
    allowed_cuda_versions: list[str] = Field(default_factory=list)
    execution_mode: ExecutionMode
    compatibility_status: CompatibilityStatus
    compatibility_issues: list[CompatibilityIssue] = Field(default_factory=list)
    generic_parameters: dict[str, Any] = Field(default_factory=dict)
    runtime_options: dict[str, str] = Field(default_factory=dict)
    output_dir: str
    benchmark_headers: dict[str, str] = Field(default_factory=dict)
    workload_headers: dict[str, str] = Field(default_factory=dict)
    attach_target: AttachTargetSpec | None = None
    tags: list[str] = Field(default_factory=list)


class ExecutionStepResult(BenchmarkBaseModel):
    name: str
    category: str
    output_path: str
    command: list[str]
    command_display: str
    started_at: str | None = None
    finished_at: str | None = None
    duration_ms: int | None = None
    returncode: int | None = None
    status: str


class ExperimentExecutionResult(BenchmarkBaseModel):
    run_spec: ResolvedRunSpec
    generated_at: str
    status: Literal["ok", "failed", "blocked", "skipped", "dry_run"]
    manifest_path: str
    steps: list[ExecutionStepResult] = Field(default_factory=list)
    notes: list[str] = Field(default_factory=list)


class WarmMetricSummary(BenchmarkBaseModel):
    cache_reuse_mode: CacheReuseMode
    workload: str
    ttft_p50_ms: float = 0.0
    ttft_p95_ms: float = 0.0
    stream_total_p50_ms: float = 0.0
    non_stream_total_p50_ms: float = 0.0
    decode_tok_s_p50: float = 0.0
    aggregate_decode_tok_s_p50: float = 0.0
    aggregate_total_tok_s_p50: float = 0.0
    tpot_p50_ms: float = 0.0
    itl_p50_ms: float = 0.0
    request_throughput_rps: float = 0.0
    peak_memory_used_bytes: int = 0
    health_sample_count: int = 0
    failures: int = 0
    source_path: str


class LifecycleMetricSummary(BenchmarkBaseModel):
    stage: str
    summary: dict[str, Any] = Field(default_factory=dict)
    source_path: str


class ExperimentResultRecord(BenchmarkBaseModel):
    run_id: str
    suite_id: str
    status: str
    compatibility_status: CompatibilityStatus
    engine_id: str
    hardware_id: str
    gpu_count: int
    model_id: str
    workload_id: str
    benchmark_profile_id: str
    runtime_preset_id: str
    runtime_options: dict[str, str] = Field(default_factory=dict)
    generic_parameters: dict[str, Any] = Field(default_factory=dict)
    notes: list[str] = Field(default_factory=list)
    warm_summaries: list[WarmMetricSummary] = Field(default_factory=list)
    lifecycle_summaries: list[LifecycleMetricSummary] = Field(default_factory=list)
    manifest_path: str


class ExperimentResultIndex(BenchmarkBaseModel):
    generated_at: str
    suite_id: str
    catalog_root: str
    results: list[ExperimentResultRecord]
    blocked: list[ExperimentResultRecord] = Field(default_factory=list)
    skipped: list[ExperimentResultRecord] = Field(default_factory=list)


class ResultComparisonEntry(BenchmarkBaseModel):
    run_id: str
    objective: RankingObjective
    score: float
    reason: str
    record: ExperimentResultRecord


class ResultComparison(BenchmarkBaseModel):
    generated_at: str
    objective: RankingObjective
    entries: list[ResultComparisonEntry]


class EvalIteration(BenchmarkBaseModel):
    iteration_id: str
    generated_at: str
    label: str = ""
    eval_command: list[str] = Field(default_factory=list)
    eval_command_display: str = ""
    cwd: str = ""
    status: Literal["ok", "failed", "parse_failed"] = "ok"
    change_summary: str = ""
    bottleneck: str = ""
    overall_score: float | None = None
    llm_average_score: float | None = None
    overall_target: float = 90.0
    llm_average_target: float = 90.0
    raw_output_path: str = ""
    artifact_paths: list[str] = Field(default_factory=list)
    remaining_risks: list[str] = Field(default_factory=list)
    notes: list[str] = Field(default_factory=list)


class EvalHistory(BenchmarkBaseModel):
    history_id: str
    generated_at: str
    iterations: list[EvalIteration] = Field(default_factory=list)


def utc_now_iso() -> str:
    return datetime.utcnow().isoformat(timespec="seconds") + "Z"
