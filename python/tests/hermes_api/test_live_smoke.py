"""Optional smoke coverage against a live Hermes deployment."""

from __future__ import annotations

from base64 import b64decode

import pytest

from .assertions import assert_status_code
from .scenario_runner import actual_tool_names
from .tool_catalog import CURRENT_HERMES_TOOL_ORDER

pytestmark = [pytest.mark.hermes_api, pytest.mark.live]


def _select_loaded_model(hermes_client) -> str:
    result = hermes_client.list_inference_models()
    assert_status_code(result, 200)
    assert result.data is not None

    loaded_models = [model.id for model in result.data.data if model.loaded]
    if not loaded_models:
        pytest.skip("live smoke requires at least one loaded model in /v1/models")

    if hermes_client.config.default_model in loaded_models:
        return hermes_client.config.default_model
    return loaded_models[0]


@pytest.fixture
def live_loaded_model(hermes_client, require_live_mode, require_live_token) -> str:
    return _select_loaded_model(hermes_client)


@pytest.fixture(scope="session")
def live_sample_png_bytes() -> bytes:
    return b64decode(
        "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Y9F9dYAAAAASUVORK5CYII="
    )


def test_live_agent_catalog_matches_expected_tools(
    hermes_client,
    require_live_mode,
    require_live_token,
    report_observation,
) -> None:
    result = hermes_client.list_agents()
    assert_status_code(result, 200)
    assert result.data is not None

    report_observation.set_case(
        "live-agent-catalog",
        "live_catalog",
        "GET /api/agents",
        CURRENT_HERMES_TOOL_ORDER,
    )

    actual_tools = [tool.name for tool in result.data.agents[0].tools]
    report_observation.set_actual_tools(actual_tools)
    for tool_name in CURRENT_HERMES_TOOL_ORDER:
        assert tool_name in actual_tools


def test_live_simple_run_succeeds_with_loaded_model(
    hermes_client,
    live_loaded_model,
    require_live_mode,
    require_live_token,
    report_observation,
) -> None:
    report_observation.set_case(
        "live-simple-run",
        "live_run",
        "Give a one-sentence overview of Hermes.",
        [],
    )

    result = hermes_client.create_run(
        {
            "agent_id": "hermes",
            "mode": "operations",
            "analysis_depth": "standard",
            "model": live_loaded_model,
            "input": "Give a one-sentence overview of Hermes.",
            "max_steps": 4,
            "attachments": [],
        }
    )
    assert_status_code(result, 201)
    assert result.data is not None

    detail = hermes_client.wait_for_run(result.data.run.id, timeout_seconds=max(hermes_client.config.max_wait_seconds, 20))
    report_observation.set_actual_tools(actual_tool_names(detail))

    assert detail.run.status == "succeeded"
    assert detail.run.final_output
    output = detail.run.final_output.lower()
    assert "hermes" in output
    assert output.strip()


def test_live_research_run_returns_sources(
    hermes_client,
    live_loaded_model,
    require_live_mode,
    require_live_token,
    report_observation,
) -> None:
    report_observation.set_case(
        "live-research-run",
        "live_research",
        "Check the official RunPod status page and cite it.",
        ["web_search"],
    )

    result = hermes_client.create_run(
        {
            "agent_id": "hermes",
            "mode": "research",
            "analysis_depth": "standard",
            "model": live_loaded_model,
            "input": "Check the official RunPod status page and cite it.",
            "max_steps": 6,
            "attachments": [],
        }
    )
    assert_status_code(result, 201)
    assert result.data is not None

    detail = hermes_client.wait_for_run(result.data.run.id, timeout_seconds=max(hermes_client.config.max_wait_seconds, 20))
    report_observation.set_actual_tools(actual_tool_names(detail))

    assert detail.run.status == "succeeded"
    assert "web_search" in actual_tool_names(detail)
    assert detail.sources, "research smoke should return at least one citation source"
    assert any(source.url.startswith("https://") for source in detail.sources)
    assert detail.run.final_output


def test_live_multimodal_run_links_attachment(
    hermes_client,
    live_loaded_model,
    live_sample_png_bytes,
    require_live_mode,
    require_live_token,
    report_observation,
) -> None:
    upload = hermes_client.upload_attachment("smoke.png", live_sample_png_bytes, "image/png")
    assert_status_code(upload, 201)
    assert upload.data is not None
    attachment_id = upload.data.attachment.id

    report_observation.set_case(
        "live-multimodal-run",
        "live_multimodal",
        "Inspect this screenshot and explain what it shows.",
        ["vision_analyze"],
    )

    result = hermes_client.create_run(
        {
            "agent_id": "hermes",
            "mode": "multimodal",
            "analysis_depth": "standard",
            "model": live_loaded_model,
            "input": "Inspect this screenshot and explain what it shows.",
            "max_steps": 6,
            "attachments": [attachment_id],
        }
    )
    assert_status_code(result, 201)
    assert result.data is not None

    detail = hermes_client.wait_for_run(result.data.run.id, timeout_seconds=max(hermes_client.config.max_wait_seconds, 20))
    report_observation.set_actual_tools(actual_tool_names(detail))

    assert detail.run.status == "succeeded"
    assert "vision_analyze" in actual_tool_names(detail)
    assert detail.attachments, "multimodal smoke should include attachment metadata on the run detail"
    assert detail.attachments[0].id == attachment_id
    assert detail.run.final_output
