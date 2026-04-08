"""Engine runtime adapters for the generic benchmark lab."""

from __future__ import annotations

from dataclasses import dataclass
from typing import Any

from .catalog import BenchmarkCatalogBundle
from .schema import (
    CompatibilityIssue,
    CompatibilityStatus,
    EngineSpec,
    HardwareSpec,
    ModelVariantSpec,
    RuntimeParameterSpec,
)

STATUS_PRIORITY: dict[CompatibilityStatus, int] = {
    "ready": 0,
    "unverified": 1,
    "skipped": 2,
    "blocked": 3,
    "unsupported": 4,
    "invalid": 5,
}


@dataclass
class RuntimeResolution:
    status: CompatibilityStatus
    issues: list[CompatibilityIssue]
    runtime_options: dict[str, str]


class EngineRuntimeAdapter:
    """Translates generic runtime parameters into engine-specific worker options."""

    def __init__(self, spec: EngineSpec):
        self.spec = spec
        self.param_by_id = {parameter.id: parameter for parameter in spec.runtime_parameters}

    @property
    def engine_id(self) -> str:
        return self.spec.id

    def resolve(
        self,
        *,
        model: ModelVariantSpec,
        hardware: HardwareSpec,
        gpu_count: int,
        parameters: dict[str, Any],
    ) -> RuntimeResolution:
        issues: list[CompatibilityIssue] = []
        runtime_options: dict[str, str] = {}
        status: CompatibilityStatus = "ready"

        compatibility = model.engine_compatibility.get(self.engine_id)
        if compatibility is None:
            issues.append(
                CompatibilityIssue(
                    status="unverified",
                    field="engine_compatibility",
                    message=f"{model.model_id} has not been verified on engine {self.engine_id}",
                )
            )
            status = "unverified"
        elif compatibility.status == "unsupported":
            issues.append(
                CompatibilityIssue(
                    status="unsupported",
                    field="engine_compatibility",
                    message=f"{model.model_id} is marked unsupported on engine {self.engine_id}",
                )
            )
        elif compatibility.status == "unverified":
            issues.append(
                CompatibilityIssue(
                    status="unverified",
                    field="engine_compatibility",
                    message=f"{model.model_id} is only unverified on engine {self.engine_id}",
                )
            )
            status = "unverified"
        if (
            compatibility
            and compatibility.supported_precisions
            and model.precision not in compatibility.supported_precisions
        ):
            issues.append(
                CompatibilityIssue(
                    status="unsupported",
                    field="precision",
                    message=(
                        f"{self.engine_id} does not advertise support for model precision {model.precision} "
                        f"on {model.model_id}"
                    ),
                )
            )
        if (
            compatibility
            and compatibility.supported_quantizations
            and model.quantization not in compatibility.supported_quantizations
        ):
            issues.append(
                CompatibilityIssue(
                    status="unsupported",
                    field="quantization",
                    message=(
                        f"{self.engine_id} does not advertise support for model quantization {model.quantization} "
                        f"on {model.model_id}"
                    ),
                )
            )

        for key, raw_value in sorted(parameters.items()):
            if raw_value is None or str(raw_value).strip() == "":
                continue
            if key == "context_length":
                try:
                    requested_context = int(raw_value)
                except (TypeError, ValueError):
                    issues.append(
                        CompatibilityIssue(
                            status="invalid",
                            field="context_length",
                            message="context_length must be an integer",
                        )
                    )
                    continue
                if requested_context > model.max_context:
                    issues.append(
                        CompatibilityIssue(
                            status="invalid",
                            field="context_length",
                            message=(
                                f"context_length {requested_context} exceeds model max_context {model.max_context} "
                                f"for {model.model_id}"
                            ),
                        )
                    )
            if key == "tensor_parallelism":
                try:
                    tensor_parallelism = int(raw_value)
                except (TypeError, ValueError):
                    issues.append(
                        CompatibilityIssue(
                            status="invalid",
                            field="tensor_parallelism",
                            message="tensor_parallelism must be an integer",
                        )
                    )
                    continue
                if tensor_parallelism > gpu_count:
                    issues.append(
                        CompatibilityIssue(
                            status="invalid",
                            field="tensor_parallelism",
                            message=f"tensor_parallelism {tensor_parallelism} exceeds gpu_count {gpu_count}",
                        )
                    )

            parameter_spec = self.param_by_id.get(key)
            if parameter_spec is not None:
                normalized_value, parameter_issue = self._validate_parameter(
                    parameter_spec, raw_value
                )
                if parameter_issue is not None:
                    issues.append(parameter_issue)
                    continue
                runtime_options[parameter_spec.env_key] = normalized_value
                continue
            if key.startswith("env:"):
                env_key = key.split(":", 1)[1].strip()
                if env_key:
                    runtime_options[env_key] = str(raw_value).strip()
                    continue
            if key.startswith("INFERA_"):
                runtime_options[key] = str(raw_value).strip()
                continue
            issues.append(
                CompatibilityIssue(
                    status="unsupported",
                    field=key,
                    message=f"{self.engine_id} does not define a tunable named {key}",
                )
            )

        return RuntimeResolution(
            status=self._collapse_status(status, issues),
            issues=issues,
            runtime_options=runtime_options,
        )

    def _validate_parameter(
        self,
        parameter: RuntimeParameterSpec,
        raw_value: Any,
    ) -> tuple[str, CompatibilityIssue | None]:
        value = str(raw_value).strip()
        try:
            if parameter.value_type == "bool":
                normalized = value.lower()
                if normalized not in {"true", "false"}:
                    raise ValueError("must be true or false")
                value = normalized
            elif parameter.value_type == "int":
                parsed = int(value)
                if parameter.min_value is not None and parsed < int(parameter.min_value):
                    raise ValueError(f"must be >= {int(parameter.min_value)}")
                if parameter.max_value is not None and parsed > int(parameter.max_value):
                    raise ValueError(f"must be <= {int(parameter.max_value)}")
                value = str(parsed)
            elif parameter.value_type == "float":
                parsed = float(value)
                if parameter.min_value is not None and parsed < float(parameter.min_value):
                    raise ValueError(f"must be >= {parameter.min_value}")
                if parameter.max_value is not None and parsed > float(parameter.max_value):
                    raise ValueError(f"must be <= {parameter.max_value}")
                value = str(parsed)
        except ValueError as exc:
            return "", CompatibilityIssue(
                status="invalid",
                field=parameter.id,
                message=f"invalid value for {parameter.id}: {exc}",
            )

        if parameter.allowed_values and value not in parameter.allowed_values:
            return "", CompatibilityIssue(
                status="unsupported",
                field=parameter.id,
                message=f"{parameter.id} must be one of: {', '.join(parameter.allowed_values)}",
            )
        return value, None

    def _collapse_status(
        self,
        base_status: CompatibilityStatus,
        issues: list[CompatibilityIssue],
    ) -> CompatibilityStatus:
        status = base_status
        for issue in issues:
            if STATUS_PRIORITY[issue.status] > STATUS_PRIORITY[status]:
                status = issue.status
        return status


class VLLMRuntimeAdapter(EngineRuntimeAdapter):
    pass


class SGLangRuntimeAdapter(EngineRuntimeAdapter):
    pass


class TensorRTLLMRuntimeAdapter(EngineRuntimeAdapter):
    pass


def build_adapter_registry(catalog: BenchmarkCatalogBundle) -> dict[str, EngineRuntimeAdapter]:
    registry: dict[str, EngineRuntimeAdapter] = {}
    for engine_id, spec in catalog.engines.items():
        adapter_name = spec.adapter.lower()
        if adapter_name == "sglang":
            registry[engine_id] = SGLangRuntimeAdapter(spec)
        elif adapter_name in {"tensorrt_llm", "tensorrt-llm"}:
            registry[engine_id] = TensorRTLLMRuntimeAdapter(spec)
        else:
            registry[engine_id] = VLLMRuntimeAdapter(spec)
    return registry
