"""Config-driven benchmark orchestration for the Infera inference lab."""

from .adapters import EngineRuntimeAdapter, build_adapter_registry
from .catalog import BenchmarkCatalogBundle, default_catalog_root, load_catalog_bundle
from .execution import AttachExecutor, ProvisionExecutor
from .lab import BenchmarkLab, BenchmarkLabPaths
from .results import compare_result_indexes, write_result_artifacts
from .schema import (
    BenchmarkCatalogEnvelope,
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
    "ExperimentExecutionResult",
    "ExperimentResultIndex",
    "ExperimentSuite",
    "ProvisionExecutor",
    "ResolvedRunSpec",
    "build_adapter_registry",
    "compare_result_indexes",
    "default_catalog_root",
    "load_catalog_bundle",
    "write_result_artifacts",
]
