"""Matrix expansion and compatibility validation for benchmark suites."""

from __future__ import annotations

from itertools import product
from pathlib import Path
from typing import Any

from .adapters import EngineRuntimeAdapter
from .catalog import BenchmarkCatalogBundle
from .schema import (
    AttachTargetSpec,
    BenchmarkProfile,
    CompatibilityIssue,
    ExperimentSuite,
    ResolvedRunSpec,
    RuntimePresetSpec,
)


def slugify(value: str) -> str:
    slug = value.strip().lower().replace("/", "-").replace("_", "-").replace(".", "-")
    while "--" in slug:
        slug = slug.replace("--", "-")
    return slug


def preset_map_for_suite(suite: ExperimentSuite) -> dict[str, RuntimePresetSpec]:
    presets = {preset.id: preset for preset in suite.runtime_presets}
    presets.setdefault(
        "baseline",
        RuntimePresetSpec(
            id="baseline",
            display_name="Baseline",
            description="No additional runtime overrides.",
            parameters={},
        ),
    )
    return presets


def matches_filter(candidate: dict[str, Any], filters: list[dict[str, Any]]) -> bool:
    if not filters:
        return True
    for matcher in filters:
        if all(candidate.get(key) == value for key, value in matcher.items() if value is not None):
            return True
    return False


def build_run_id(parts: list[str]) -> str:
    return "-".join(slugify(part) for part in parts if part)


def _select_provider(
    *,
    benchmark_profile: BenchmarkProfile,
    hardware,
    preferred_provider: str | None,
) -> tuple[str | None, str | None, str | None, list[str], list[CompatibilityIssue]]:
    if benchmark_profile.execution_mode == "attach":
        return None, None, None, [], []
    selectors = list(hardware.provider_selectors)
    if preferred_provider:
        selectors = [selector for selector in selectors if selector.provider == preferred_provider]
    else:
        selectors.sort(key=lambda selector: (selector.provider == "mock", selector.provider))
    if not selectors:
        return None, None, None, [], [
            CompatibilityIssue(
                status="blocked",
                field="provider_selectors",
                message=(
                    f"hardware {hardware.hardware_id} has no provider selector for "
                    f"{preferred_provider or 'the requested benchmark profile'}"
                ),
            )
        ]
    selector = selectors[0]
    return (
        selector.provider,
        selector.gpu_type,
        selector.provider_gpu_type_id or None,
        list(selector.allowed_cuda_versions),
        [],
    )


def expand_suite(
    suite: ExperimentSuite,
    catalog: BenchmarkCatalogBundle,
    adapters: dict[str, EngineRuntimeAdapter],
) -> list[ResolvedRunSpec]:
    presets = preset_map_for_suite(suite)
    results: list[ResolvedRunSpec] = []
    output_root = Path(suite.output_root).expanduser()

    include_filters = [filter_item.model_dump() for filter_item in suite.include]
    exclude_filters = [filter_item.model_dump() for filter_item in suite.exclude]

    for engine_id, hardware_id, gpu_count, model_id, workload_id, benchmark_profile_id, runtime_preset_id in product(
        suite.matrix.engines,
        suite.matrix.hardware,
        suite.matrix.gpu_counts,
        suite.matrix.models,
        suite.matrix.workloads,
        suite.matrix.benchmark_profiles,
        suite.matrix.runtime_presets,
    ):
        candidate = {
            "engine": engine_id,
            "hardware": hardware_id,
            "gpu_count": gpu_count,
            "model": model_id,
            "workload": workload_id,
            "benchmark_profile": benchmark_profile_id,
            "runtime_preset": runtime_preset_id,
        }
        if include_filters and not matches_filter(candidate, include_filters):
            continue
        if exclude_filters and matches_filter(candidate, exclude_filters):
            continue

        issues: list[CompatibilityIssue] = []
        status = "ready"
        runtime_options: dict[str, str] = {}
        provider = provider_gpu_type = provider_gpu_type_id = None
        allowed_cuda_versions: list[str] = []

        try:
            engine = catalog.resolve_engine(engine_id)
            adapter = adapters[engine.id]
        except KeyError as exc:
            issues.append(CompatibilityIssue(status="invalid", field="engine", message=str(exc)))
            engine = None
            adapter = None
        try:
            hardware = catalog.resolve_hardware(hardware_id)
        except KeyError as exc:
            issues.append(CompatibilityIssue(status="invalid", field="hardware", message=str(exc)))
            hardware = None
        try:
            model = catalog.resolve_model(model_id)
        except KeyError as exc:
            issues.append(CompatibilityIssue(status="invalid", field="model", message=str(exc)))
            model = None
        try:
            workload = catalog.resolve_workload(workload_id)
        except KeyError as exc:
            issues.append(CompatibilityIssue(status="invalid", field="workload", message=str(exc)))
            workload = None
        try:
            benchmark_profile = catalog.resolve_benchmark_profile(benchmark_profile_id)
        except KeyError as exc:
            issues.append(CompatibilityIssue(status="invalid", field="benchmark_profile", message=str(exc)))
            benchmark_profile = None

        preset = presets.get(runtime_preset_id)
        if preset is None:
            issues.append(
                CompatibilityIssue(
                    status="invalid",
                    field="runtime_preset",
                    message=f"unknown runtime preset {runtime_preset_id!r}",
                )
            )

        if engine and preset and preset.engine_ids and engine.id not in preset.engine_ids:
            issues.append(
                CompatibilityIssue(
                    status="unsupported",
                    field="runtime_preset",
                    message=f"runtime preset {preset.id} is not defined for engine {engine.id}",
                )
            )

        if benchmark_profile and hardware:
            provider, provider_gpu_type, provider_gpu_type_id, allowed_cuda_versions, provider_issues = _select_provider(
                benchmark_profile=benchmark_profile,
                hardware=hardware,
                preferred_provider=suite.default_provider,
            )
            issues.extend(provider_issues)

        if benchmark_profile and benchmark_profile.execution_mode == "attach" and suite.attach_target is None:
            issues.append(
                CompatibilityIssue(
                    status="invalid",
                    field="attach_target",
                    message="attach execution mode requires suite.attach_target",
                )
            )

        generic_parameters = dict(suite.default_runtime_parameters)
        if preset is not None:
            generic_parameters.update(preset.parameters)

        if adapter is not None and model is not None and hardware is not None:
            resolution = adapter.resolve(
                model=model,
                hardware=hardware,
                gpu_count=int(gpu_count),
                parameters=generic_parameters,
            )
            issues.extend(resolution.issues)
            runtime_options.update(resolution.runtime_options)
            status = resolution.status

        if issues:
            status = max((issue.status for issue in issues), key=lambda item: _status_rank(item))

        run_id = build_run_id(
            [
                suite.suite_id,
                engine_id,
                hardware_id,
                str(gpu_count),
                workload_id,
                benchmark_profile_id,
                runtime_preset_id,
            ]
        )
        output_dir = output_root / slugify(suite.suite_id) / slugify(engine_id) / slugify(runtime_preset_id) / run_id
        results.append(
            ResolvedRunSpec(
                suite_id=suite.suite_id,
                run_id=run_id,
                engine_id=engine_id,
                hardware_id=hardware_id,
                gpu_count=int(gpu_count),
                model_id=model_id,
                workload_id=workload_id,
                benchmark_profile_id=benchmark_profile_id,
                runtime_preset_id=runtime_preset_id,
                provider=provider,
                provider_gpu_type=provider_gpu_type,
                provider_gpu_type_id=provider_gpu_type_id,
                allowed_cuda_versions=allowed_cuda_versions,
                execution_mode=benchmark_profile.execution_mode if benchmark_profile else "provision",
                compatibility_status=status,
                compatibility_issues=issues,
                generic_parameters=generic_parameters,
                runtime_options=runtime_options,
                output_dir=str(output_dir),
                benchmark_headers=dict((benchmark_profile.benchmark_headers if benchmark_profile else {})),
                workload_headers=dict((workload.headers if workload else {})),
                attach_target=_clone_attach_target(suite.attach_target),
                tags=_build_tags(engine_id, hardware_id, model_id, workload_id, runtime_preset_id),
            )
        )

    return results


def _build_tags(
    engine_id: str,
    hardware_id: str,
    model_id: str,
    workload_id: str,
    runtime_preset_id: str,
) -> list[str]:
    return [engine_id, hardware_id, model_id, workload_id, runtime_preset_id]


def _status_rank(value: str) -> int:
    return {
        "ready": 0,
        "unverified": 1,
        "skipped": 2,
        "blocked": 3,
        "unsupported": 4,
        "invalid": 5,
    }[value]


def _clone_attach_target(target: AttachTargetSpec | None) -> AttachTargetSpec | None:
    if target is None:
        return None
    return AttachTargetSpec.model_validate(target.model_dump())
