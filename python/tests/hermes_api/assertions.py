"""Shared assertions for Hermes API scenarios."""

from __future__ import annotations

from typing import Any

import pytest

from .models import APICallResult, ErrorResponse, RunDetailResponse, ScenarioCase


def assert_status_code(result: APICallResult[Any], expected: int) -> None:
    assert result.status_code == expected, (
        f"expected HTTP {expected}, got {result.status_code}: {result.text}"
    )


def assert_error_type(result: APICallResult[Any], expected: str) -> None:
    payload = ErrorResponse.model_validate(result.json_payload or {})
    assert payload.error.type == expected, (
        f"expected error type {expected!r}, got {payload.error.type!r}"
    )
    assert payload.error.message.strip(), "error message should be explicit"


def extract_tool_calls(detail: RunDetailResponse) -> list[tuple[str, dict[str, Any]]]:
    calls: list[tuple[str, dict[str, Any]]] = []
    for step in detail.steps:
        if step.type == "tool_call":
            payload = step.payload if isinstance(step.payload, dict) else {}
            arguments = payload.get("arguments", {}) if isinstance(payload, dict) else {}
            calls.append((step.tool_name or "", arguments if isinstance(arguments, dict) else {}))
    return calls


def _normalize_expected_argument(value: Any, detail: RunDetailResponse) -> Any:
    if value == "$attachment_id":
        assert detail.attachments, "expected an uploaded attachment to resolve $attachment_id"
        return detail.attachments[0].id
    return value


def assert_argument_subset(actual: Any, expected: Any, detail: RunDetailResponse) -> None:
    if isinstance(expected, dict):
        assert isinstance(actual, dict), f"expected dict arguments, got {type(actual).__name__}"
        for key, expected_value in expected.items():
            assert key in actual, f"missing tool argument key {key!r} in {actual!r}"
            assert_argument_subset(actual[key], expected_value, detail)
        return
    if isinstance(expected, list):
        assert isinstance(actual, list), f"expected list argument, got {type(actual).__name__}"
        assert len(actual) >= len(expected), (
            f"expected list of at least {len(expected)}, got {len(actual)}"
        )
        for actual_item, expected_item in zip(actual, expected, strict=False):
            assert_argument_subset(actual_item, expected_item, detail)
        return
    assert actual == _normalize_expected_argument(expected, detail), (
        f"expected argument {expected!r}, got {actual!r}"
    )


def assert_scenario_tools(detail: RunDetailResponse, case: ScenarioCase) -> None:
    calls = extract_tool_calls(detail)
    actual_tool_names = [tool_name for tool_name, _ in calls]

    for tool in case.expected_tools:
        assert tool in actual_tool_names, f"expected tool {tool!r}, got {actual_tool_names!r}"
    for tool in case.disallowed_tools:
        assert tool not in actual_tool_names, f"tool {tool!r} should not have been used"
    if case.allowed_tools:
        unexpected = [tool for tool in actual_tool_names if tool not in case.allowed_tools]
        assert not unexpected, f"unexpected tools used: {unexpected!r}"

    for tool_name, expected_arguments in case.expected_arguments.items():
        matching_arguments = [
            arguments for actual_tool_name, arguments in calls if actual_tool_name == tool_name
        ]
        assert matching_arguments, (
            f"expected arguments for tool {tool_name!r}, but no tool call was recorded"
        )
        assert_argument_subset(matching_arguments[0], expected_arguments, detail)


def assert_final_output_matches(detail: RunDetailResponse, expected_keywords: list[str]) -> None:
    output = (detail.run.final_output or "").lower()
    assert output.strip(), "final_output should not be empty for successful or explanatory runs"
    missing = [keyword for keyword in expected_keywords if keyword.lower() not in output]
    assert not missing, (
        f"final_output is missing expected keywords {missing!r}: {detail.run.final_output!r}"
    )


def assert_terminal_status(detail: RunDetailResponse, expected_status: str) -> None:
    assert detail.run.status == expected_status, (
        f"expected terminal status {expected_status!r}, got {detail.run.status!r}"
    )
    if expected_status == "failed":
        assert detail.run.failure_reason, "failed runs should include a failure_reason"


def assert_schema_coherent(detail: RunDetailResponse) -> None:
    if detail.run.status == "succeeded":
        assert detail.run.final_output, "succeeded runs should include final_output"
    if detail.sources:
        assert any(step.tool_name == "web_search" for step in detail.steps), (
            "sources should come from web_search"
        )


def mark_mock_only_if_live(case_id: str, is_live: bool) -> None:
    if is_live:
        pytest.skip(
            f"{case_id} is a deterministic mock scenario; use the live smoke tests for deployed Hermes"
        )
