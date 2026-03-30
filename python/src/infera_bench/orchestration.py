"""High-level orchestration helpers for benchmark suites."""

from __future__ import annotations

from pathlib import Path
import sys

from .adapters import build_adapter_registry
from .catalog import BenchmarkCatalogBundle
from .execution import (
    AttachExecutor,
    ProvisionExecutor,
    wait_for_model_registry_drain,
    write_json_output,
)
from .matrix import expand_suite
from .results import build_result_index, write_result_artifacts
from .schema import ExperimentExecutionResult, ExperimentResultIndex, ExperimentSuite


def execute_suite(
    *,
    base_url: str,
    api_key: str,
    suite: ExperimentSuite,
    catalog: BenchmarkCatalogBundle,
    workload_file: Path,
    python_bin: str | None = None,
    cost_per_hour: float | None = None,
    health_insecure: bool = False,
    quiet_progress: bool = False,
    terminate_final_instance: bool = False,
    dry_run: bool = False,
    continue_on_error: bool = False,
) -> tuple[ExperimentResultIndex, dict[str, Path], list[ExperimentExecutionResult]]:
    adapters = build_adapter_registry(catalog)
    run_specs = expand_suite(suite, catalog, adapters)
    execution_results: list[ExperimentExecutionResult] = []
    for index, run_spec in enumerate(run_specs):
        benchmark_profile = catalog.resolve_benchmark_profile(run_spec.benchmark_profile_id)
        if run_spec.execution_mode == "attach":
            executor = AttachExecutor(
                base_url=base_url,
                api_key=api_key,
                workload_file=workload_file,
                python_bin=python_bin or sys.executable,
                cost_per_hour=cost_per_hour,
                health_insecure=health_insecure,
                dry_run=dry_run,
            )
        else:
            executor = ProvisionExecutor(
                base_url=base_url,
                api_key=api_key,
                workload_file=workload_file,
                python_bin=python_bin or sys.executable,
                cost_per_hour=cost_per_hour,
                health_insecure=health_insecure,
                quiet_progress=quiet_progress,
                terminate_final_instance=terminate_final_instance,
                dry_run=dry_run,
                continue_on_error=continue_on_error,
            )
        execution = executor.execute(run_spec, benchmark_profile)
        execution_results.append(execution)
        has_following_run = index < len(run_specs) - 1
        if (
            has_following_run
            and terminate_final_instance
            and not dry_run
            and run_spec.execution_mode == "provision"
        ):
            try:
                wait_for_model_registry_drain(
                    base_url=base_url,
                    api_key=api_key,
                    model_id=run_spec.model_id,
                    timeout_s=benchmark_profile.registry_drain_timeout_s,
                    poll_interval_ms=benchmark_profile.registry_drain_poll_interval_ms,
                    log_prefix="benchmark-suite",
                )
            except Exception as exc:
                execution.status = "blocked"
                execution.notes.append(
                    "Post-run gateway drain failed before the next suite run could start: "
                    f"{exc}"
                )
                write_json_output(Path(execution.manifest_path), execution.model_dump())
                break
    index = build_result_index(
        suite_id=suite.suite_id,
        catalog_root=catalog.root,
        execution_results=execution_results,
    )
    artifacts = write_result_artifacts(index, Path(suite.output_root).expanduser() / suite.suite_id)
    return index, artifacts, execution_results
