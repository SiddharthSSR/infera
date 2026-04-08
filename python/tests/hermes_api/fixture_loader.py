"""Fixture loading and validation for Hermes API scenarios."""

from __future__ import annotations

import json
from functools import lru_cache
from pathlib import Path

from pydantic import TypeAdapter

from .models import ConditionCase, ScenarioCase, ScenarioCatalog

DATA_DIR = Path(__file__).resolve().parent / "data"


def _read_json(path: Path) -> object:
    return json.loads(path.read_text(encoding="utf-8"))


def load_scenario_cases(name: str) -> list[ScenarioCase]:
    adapter = TypeAdapter(list[ScenarioCase])
    return adapter.validate_python(_read_json(DATA_DIR / name))


def load_condition_cases(name: str) -> list[ConditionCase]:
    adapter = TypeAdapter(list[ConditionCase])
    return adapter.validate_python(_read_json(DATA_DIR / name))


@lru_cache(maxsize=8)
def load_catalog(regression_override: Path | None = None) -> ScenarioCatalog:
    regression_path = regression_override or (DATA_DIR / "regression_cases.json")
    adapter = TypeAdapter(list[ScenarioCase])
    return ScenarioCatalog(
        tool_cases=load_scenario_cases("tool_cases.json"),
        prompt_cases=load_scenario_cases("prompt_cases.json"),
        condition_cases=load_condition_cases("condition_cases.json"),
        regression_cases=adapter.validate_python(_read_json(regression_path)),
    )
