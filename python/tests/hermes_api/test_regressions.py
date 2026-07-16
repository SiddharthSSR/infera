"""Regression dataset support for Hermes API scenarios."""

from __future__ import annotations

import pytest

from .assertions import (
    assert_final_output_matches,
    assert_scenario_tools,
    assert_schema_coherent,
    assert_terminal_status,
    mark_mock_only_if_live,
)
from .config import load_test_config
from .fixture_loader import load_catalog
from .models import ScenarioCase
from .scenario_runner import actual_tool_names, run_scenario_case

pytestmark = [pytest.mark.hermes_api, pytest.mark.mock_only]

_CATALOG = load_catalog(load_test_config().regression_dataset)


@pytest.mark.parametrize("case", _CATALOG.regression_cases, ids=lambda case: case.id)
def test_regression_cases(
    hermes_client,
    hermes_config,
    sample_png_bytes,
    case: ScenarioCase,
    report_observation,
) -> None:
    mark_mock_only_if_live(case.id, hermes_config.is_live)
    report_observation.set_case(case.id, case.category, case.prompt, case.expected_tools)

    detail = run_scenario_case(hermes_client, case, sample_png_bytes=sample_png_bytes)
    report_observation.set_actual_tools(actual_tool_names(detail))

    assert_terminal_status(detail, case.expected_status)
    assert_schema_coherent(detail)
    assert_scenario_tools(detail, case)
    assert_final_output_matches(detail, case.response_keywords)
