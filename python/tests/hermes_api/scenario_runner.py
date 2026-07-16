"""Shared helpers for executing Hermes API scenarios.

This module keeps test bodies compact and makes it easier to add new prompt,
tool, and condition cases without duplicating request construction logic.
"""

from __future__ import annotations

from typing import Any

from .assertions import extract_tool_calls
from .client import HermesAPIClient
from .models import ConditionCase, RunDetailResponse, ScenarioCase


def actual_tool_names(detail: RunDetailResponse) -> list[str]:
    """Return the ordered list of tool names used by a run."""

    return [tool_name for tool_name, _ in extract_tool_calls(detail)]


def upload_attachment_ids(
    client: HermesAPIClient,
    *,
    needs_attachment: bool,
    sample_png_bytes: bytes,
) -> list[str]:
    """Upload a representative screenshot when a scenario needs one."""

    if not needs_attachment:
        return []
    upload = client.upload_attachment("console.png", sample_png_bytes, "image/png")
    if upload.status_code != 201 or upload.data is None:  # pragma: no cover - guarded by callers
        raise AssertionError(
            f"attachment upload failed unexpectedly: HTTP {upload.status_code} {upload.text}"
        )
    return [upload.data.attachment.id]


def build_run_payload(
    client: HermesAPIClient,
    *,
    prompt: str,
    mode: str,
    analysis_depth: str,
    max_steps: int,
    attachments: list[str] | None = None,
    overrides: dict[str, Any] | None = None,
) -> dict[str, Any]:
    """Build a standard Hermes create-run payload with optional overrides."""

    payload: dict[str, Any] = {
        "agent_id": "hermes",
        "mode": mode,
        "analysis_depth": analysis_depth,
        "model": client.config.default_model,
        "input": prompt,
        "max_steps": max_steps,
        "attachments": attachments or [],
    }
    if overrides:
        payload.update(overrides)
    return payload


def run_scenario_case(
    client: HermesAPIClient,
    case: ScenarioCase,
    *,
    sample_png_bytes: bytes,
) -> RunDetailResponse:
    """Create a run for a prompt fixture and wait for the terminal result."""

    attachments = upload_attachment_ids(
        client,
        needs_attachment=case.needs_attachment,
        sample_png_bytes=sample_png_bytes,
    )
    create = client.create_run(
        build_run_payload(
            client,
            prompt=case.prompt,
            mode=case.mode,
            analysis_depth=case.analysis_depth,
            max_steps=case.max_steps,
            attachments=attachments,
        )
    )
    if create.status_code != 201 or create.data is None:  # pragma: no cover - guarded by callers
        raise AssertionError(
            f"run creation failed unexpectedly: HTTP {create.status_code} {create.text}"
        )
    return client.wait_for_run(create.data.run.id)


def scenario_from_condition(case: ConditionCase, *, expected_status: str) -> ScenarioCase:
    """Adapt a condition case into a ScenarioCase for shared assertions."""

    return ScenarioCase(
        id=case.id,
        title=case.title,
        category=case.category,
        prompt=case.prompt or case.operation,
        mode=case.mode,
        analysis_depth=case.analysis_depth,
        max_steps=8,
        needs_attachment=case.needs_attachment,
        expected_status=expected_status,  # type: ignore[arg-type]
        expected_tools=case.expected_tools,
        allowed_tools=[],
        disallowed_tools=[],
        expected_arguments=case.expected_arguments,
        response_keywords=case.response_keywords,
        final_output=case.final_output or "",
        mock_behavior=case.mock_behavior,
        notes=case.notes,
    )
