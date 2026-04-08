"""Condition and failure-mode coverage for the Hermes API."""

from __future__ import annotations

import pytest

from .assertions import (
    assert_error_type,
    assert_final_output_matches,
    assert_scenario_tools,
    assert_status_code,
    assert_terminal_status,
    mark_mock_only_if_live,
)
from .client import HermesSchemaError, HermesTransportError
from .config import load_test_config
from .fixture_loader import load_catalog
from .models import ConditionCase
from .scenario_runner import (
    actual_tool_names,
    build_run_payload,
    scenario_from_condition,
    upload_attachment_ids,
)

pytestmark = [pytest.mark.hermes_api, pytest.mark.mock_only]

_CATALOG = load_catalog(load_test_config().regression_dataset)


def _condition_cases_for(operation: str) -> list[ConditionCase]:
    return [case for case in _CATALOG.condition_cases if case.operation == operation]


@pytest.mark.parametrize(
    "case",
    _condition_cases_for("create_run") + _condition_cases_for("upload_attachment"),
    ids=lambda case: case.id,
)
def test_create_run_request_validation(
    hermes_client,
    hermes_config,
    sample_png_bytes,
    case: ConditionCase,
    report_observation,
) -> None:
    mark_mock_only_if_live(case.id, hermes_config.is_live)
    report_observation.set_case(case.id, case.category, case.prompt or case.operation, case.expected_tools)

    if case.operation == "upload_attachment":
        result = hermes_client.upload_attachment("notes.txt", b"plain text", "text/plain")
        assert_status_code(result, case.expected_status_code)
        assert_error_type(result, case.expected_error_type or "invalid_request")
        return

    payload = dict(case.request_body)
    if case.needs_attachment:
        payload["attachments"] = upload_attachment_ids(
            hermes_client,
            needs_attachment=True,
            sample_png_bytes=sample_png_bytes,
        )

    result = hermes_client.create_run(payload)
    assert_status_code(result, case.expected_status_code)
    assert_error_type(result, case.expected_error_type or "invalid_request")


@pytest.mark.parametrize(
    "case",
    _condition_cases_for("get_run_detail") + _condition_cases_for("wait_for_run"),
    ids=lambda case: case.id,
)
def test_detail_and_wait_failure_modes(
    hermes_client,
    hermes_config,
    sample_png_bytes,
    case: ConditionCase,
    report_observation,
) -> None:
    mark_mock_only_if_live(case.id, hermes_config.is_live)
    report_observation.set_case(case.id, case.category, case.prompt or case.operation, case.expected_tools)

    attachments = upload_attachment_ids(
        hermes_client,
        needs_attachment=case.needs_attachment,
        sample_png_bytes=sample_png_bytes,
    )
    create = hermes_client.create_run(
        build_run_payload(
            hermes_client,
            prompt=case.prompt or case.operation,
            mode=case.mode,
            analysis_depth=case.analysis_depth,
            max_steps=8,
            attachments=attachments,
        ),
        retry_on_rate_limit=case.retry_on_rate_limit,
    )
    assert_status_code(create, 201)
    assert create.data is not None

    if case.mock_behavior == "malformed_detail":
        with pytest.raises(HermesSchemaError):
            hermes_client.get_run_detail(create.data.run.id)
        return
    if case.mock_behavior == "transport_timeout":
        with pytest.raises(HermesTransportError):
            hermes_client.get_run_detail(create.data.run.id)
        return

    detail = hermes_client.wait_for_run(create.data.run.id)
    report_observation.set_actual_tools(actual_tool_names(detail))

    expected_status = "failed" if case.mock_behavior == "tool_invalid_arguments" else "succeeded"
    assert_terminal_status(detail, expected_status)
    assert_scenario_tools(detail, scenario_from_condition(case, expected_status=expected_status))
    if case.response_keywords:
        assert_final_output_matches(detail, case.response_keywords)


def test_rate_limit_retry_behavior(hermes_client, hermes_config, report_observation) -> None:
    case = next(case for case in _CATALOG.condition_cases if case.mock_behavior == "rate_limit_then_success")
    mark_mock_only_if_live(case.id, hermes_config.is_live)
    report_observation.set_case(case.id, case.category, case.prompt or case.operation, case.expected_tools)

    result = hermes_client.create_run(
        build_run_payload(
            hermes_client,
            prompt=case.prompt or case.operation,
            mode=case.mode,
            analysis_depth=case.analysis_depth,
            max_steps=8,
        ),
        retry_on_rate_limit=True,
    )
    assert_status_code(result, 201)
    assert result.data is not None

    detail = hermes_client.wait_for_run(result.data.run.id)
    report_observation.set_actual_tools(actual_tool_names(detail))
    assert_terminal_status(detail, "succeeded")
    assert_final_output_matches(detail, case.response_keywords)


@pytest.mark.parametrize("case", _condition_cases_for("external_run_wait"), ids=lambda case: case.id)
def test_external_wait_timeout(hermes_client, hermes_config, case: ConditionCase, report_observation) -> None:
    mark_mock_only_if_live(case.id, hermes_config.is_live)
    report_observation.set_case(case.id, case.category, case.prompt or case.operation, case.expected_tools)

    result = hermes_client.create_external_run(
        build_run_payload(
            hermes_client,
            prompt=case.prompt or case.operation,
            mode=case.mode,
            analysis_depth=case.analysis_depth,
            max_steps=8,
        ),
        wait=True,
    )
    assert_status_code(result, case.expected_status_code)
    assert result.data is not None
    assert result.data.timed_out is True


def test_cancel_run_transitions_to_canceled(hermes_client, hermes_config, report_observation) -> None:
    mark_mock_only_if_live("condition-cancel-run", hermes_config.is_live)
    report_observation.set_case(
        "condition-cancel-run",
        "cancellation",
        "Create then cancel a run",
        ["list_workers"],
    )

    create = hermes_client.create_run(
        build_run_payload(
            hermes_client,
            prompt="Check current worker health for this workspace.",
            mode="operations",
            analysis_depth="standard",
            max_steps=4,
        )
    )
    assert_status_code(create, 201)
    assert create.data is not None

    canceled = hermes_client.cancel_run(create.data.run.id)
    assert_status_code(canceled, 200)
    assert canceled.data is not None
    assert canceled.data.run.status == "canceled"
    assert "canceled" in (canceled.data.run.failure_reason or "").lower()


def test_list_runs_returns_newest_first(hermes_client, hermes_config, report_observation) -> None:
    mark_mock_only_if_live("condition-list-runs-order", hermes_config.is_live)
    report_observation.set_case(
        "condition-list-runs-order",
        "listing",
        "Create two runs and verify ordering",
        [],
    )

    first = hermes_client.create_run(
        build_run_payload(
            hermes_client,
            prompt="What modes does Hermes support?",
            mode="operations",
            analysis_depth="standard",
            max_steps=4,
        )
    )
    second = hermes_client.create_run(
        build_run_payload(
            hermes_client,
            prompt="List the models available in this workspace.",
            mode="operations",
            analysis_depth="standard",
            max_steps=4,
        )
    )
    assert_status_code(first, 201)
    assert_status_code(second, 201)
    assert first.data is not None and second.data is not None

    listed = hermes_client.list_runs()
    assert_status_code(listed, 200)
    assert listed.data is not None
    assert listed.data.total >= 2
    assert listed.data.runs[0].id == second.data.run.id
    assert listed.data.runs[1].id == first.data.run.id
