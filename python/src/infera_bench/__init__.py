"""Config-driven benchmark orchestration for the Infera inference lab."""

from .adapters import EngineRuntimeAdapter, build_adapter_registry
from .catalog import BenchmarkCatalogBundle, default_catalog_root, load_catalog_bundle
from .evals import (
    best_scores,
    load_eval_history,
    parse_eval_scores,
    record_eval_iteration,
    summarize_eval_history,
    write_eval_history,
    write_eval_summary,
)
from .execution import AttachExecutor, ProvisionExecutor
from .lab import BenchmarkLab, BenchmarkLabPaths
from .results import (
    compare_result_indexes,
    format_comparison_markdown,
    write_comparison_markdown,
    write_result_artifacts,
)
from .schema import (
    BenchmarkCatalogEnvelope,
    EvalHistory,
    EvalIteration,
    ExperimentExecutionResult,
    ExperimentResultIndex,
    ExperimentSuite,
    ResolvedRunSpec,
)

__all__ = [
    "AttachExecutor",
    "BenchmarkLab",
    "BenchmarkCatalogBundle",
    "BenchmarkCatalogEnvelope",
    "BenchmarkLabPaths",
    "EngineRuntimeAdapter",
    "EvalHistory",
    "EvalIteration",
    "ExperimentExecutionResult",
    "ExperimentResultIndex",
    "ExperimentSuite",
    "ProvisionExecutor",
    "ResolvedRunSpec",
    "best_scores",
    "build_adapter_registry",
    "compare_result_indexes",
    "default_catalog_root",
    "format_comparison_markdown",
    "load_eval_history",
    "load_catalog_bundle",
    "parse_eval_scores",
    "record_eval_iteration",
    "summarize_eval_history",
    "write_comparison_markdown",
    "write_eval_history",
    "write_eval_summary",
    "write_result_artifacts",
]
