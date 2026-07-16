"""Deterministic API scenarios that exercise every built-in Hermes tool."""

from __future__ import annotations

import pytest

from .assertions import (
    assert_final_output_matches,
    assert_scenario_tools,
    assert_schema_coherent,
    assert_status_code,
    assert_terminal_status,
    mark_mock_only_if_live,
)
from .config import load_test_config
from .fixture_loader import load_catalog
from .models import ScenarioCase
from .scenario_runner import actual_tool_names, run_scenario_case
from .tool_catalog import CURRENT_HERMES_TOOL_ORDER

pytestmark = [pytest.mark.hermes_api, pytest.mark.mock_only]

_CATALOG = load_catalog(load_test_config().regression_dataset)


def test_agent_catalog_exposes_all_current_tools(hermes_client, report_observation) -> None:
    result = hermes_client.list_agents()
    assert_status_code(result, 200)
    assert result.data is not None

    report_observation.set_case(
        scenario_id="catalog-current-tools",
        category="catalog",
        prompt="GET /api/agents",
        expected_tools=CURRENT_HERMES_TOOL_ORDER,
    )

    assert result.data.default_agent_id == "hermes"
    assert len(result.data.agents) == 1
    actual_tools = [tool.name for tool in result.data.agents[0].tools]
    report_observation.set_actual_tools(actual_tools)
    assert actual_tools == CURRENT_HERMES_TOOL_ORDER


@pytest.mark.parametrize("case", _CATALOG.tool_cases, ids=lambda case: case.id)
def test_individual_tool_cases(
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
