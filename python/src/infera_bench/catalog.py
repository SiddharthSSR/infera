"""Catalog loading helpers for the generic inference benchmark lab."""

from __future__ import annotations

import json
from pathlib import Path

from .schema import (
    BenchmarkCatalogEnvelope,
    BenchmarkProfile,
    EngineSpec,
    ExperimentSuite,
    HardwareSpec,
    ModelVariantSpec,
    RuntimePresetSpec,
    WorkloadProfile,
)


CATALOG_FILENAMES = {
    "engines": "engines.json",
    "hardware": "hardware.json",
    "models": "models.json",
    "workloads": "workloads.json",
    "benchmark_profiles": "benchmark_profiles.json",
}


def repository_root() -> Path:
    return Path(__file__).resolve().parents[3]


def default_catalog_root() -> Path:
    return repository_root() / "configs" / "benchmark_lab"


def load_json(path: Path) -> dict:
    return json.loads(path.read_text(encoding="utf-8"))


class BenchmarkCatalogBundle:
    """Convenient lookup wrapper around the checked-in benchmark catalogs."""

    def __init__(self, root: Path, envelope: BenchmarkCatalogEnvelope):
        self.root = root
        self.envelope = envelope
        self.engines: dict[str, EngineSpec] = {}
        self.hardware: dict[str, HardwareSpec] = {}
        self.models: dict[str, ModelVariantSpec] = {}
        self.workloads: dict[str, WorkloadProfile] = {}
        self.benchmark_profiles: dict[str, BenchmarkProfile] = {}
        self.engine_aliases: dict[str, str] = {}
        self.hardware_aliases: dict[str, str] = {}
        self.model_aliases: dict[str, str] = {}
        self._index()

    def _index(self) -> None:
        for engine in self.envelope.engines.engines:
            self.engines[engine.id] = engine
            self.engine_aliases[engine.id] = engine.id
        for item in self.envelope.hardware.hardware:
            self.hardware[item.hardware_id] = item
            self.hardware_aliases[item.hardware_id] = item.hardware_id
            for alias in item.aliases:
                self.hardware_aliases[alias] = item.hardware_id
        for item in self.envelope.models.models:
            self.models[item.model_id] = item
            self.model_aliases[item.model_id] = item.model_id
            for alias in item.aliases:
                self.model_aliases[alias] = item.model_id
        for workload in self.envelope.workloads.workloads:
            self.workloads[workload.id] = workload
        for profile in self.envelope.benchmark_profiles.benchmark_profiles:
            self.benchmark_profiles[profile.id] = profile

    def resolve_engine(self, value: str) -> EngineSpec:
        key = value.strip()
        if key not in self.engines:
            raise KeyError(f"unknown engine {value!r}")
        return self.engines[key]

    def resolve_hardware(self, value: str) -> HardwareSpec:
        key = self.hardware_aliases.get(value.strip())
        if key is None:
            raise KeyError(f"unknown hardware {value!r}")
        return self.hardware[key]

    def resolve_model(self, value: str) -> ModelVariantSpec:
        key = self.model_aliases.get(value.strip())
        if key is None:
            raise KeyError(f"unknown model {value!r}")
        return self.models[key]

    def resolve_workload(self, value: str) -> WorkloadProfile:
        key = value.strip()
        if key not in self.workloads:
            raise KeyError(f"unknown workload {value!r}")
        return self.workloads[key]

    def resolve_benchmark_profile(self, value: str) -> BenchmarkProfile:
        key = value.strip()
        if key not in self.benchmark_profiles:
            raise KeyError(f"unknown benchmark profile {value!r}")
        return self.benchmark_profiles[key]


def load_catalog_bundle(root: Path | None = None) -> BenchmarkCatalogBundle:
    catalog_root = (root or default_catalog_root()).expanduser().resolve()
    payload = BenchmarkCatalogEnvelope.model_validate(
        {
            "engines": load_json(catalog_root / CATALOG_FILENAMES["engines"]),
            "hardware": load_json(catalog_root / CATALOG_FILENAMES["hardware"]),
            "models": load_json(catalog_root / CATALOG_FILENAMES["models"]),
            "workloads": load_json(catalog_root / CATALOG_FILENAMES["workloads"]),
            "benchmark_profiles": load_json(catalog_root / CATALOG_FILENAMES["benchmark_profiles"]),
        }
    )
    return BenchmarkCatalogBundle(catalog_root, payload)


def load_suite(path: Path) -> ExperimentSuite:
    return ExperimentSuite.model_validate(load_json(path.expanduser().resolve()))


def load_runtime_presets(path: Path) -> list[RuntimePresetSpec]:
    payload = load_json(path.expanduser().resolve())
    raw_presets = payload.get("runtime_presets") or payload.get("profiles") or []
    return [RuntimePresetSpec.model_validate(item) for item in raw_presets]
