"""Pytest fixtures and reporting hooks for Hermes API tests."""

from __future__ import annotations

from base64 import b64decode

import pytest

from .client import HermesAPIClient
from .config import HermesTestConfig, load_test_config
from .fixture_loader import load_catalog
from .mock_api import MockHermesTransport
from .models import ConditionCase, ScenarioCase, ScenarioCatalog
from .reporting import HermesReportRecorder, ScenarioObservation

REPORTER = HermesReportRecorder()


def pytest_runtest_makereport(item: pytest.Item, call: pytest.CallInfo[object]) -> None:
    """Attach the per-phase report to the test item for fixture finalizers."""

    if call.when != "call":
        return
    item.rep_call = call


def pytest_terminal_summary(terminalreporter: pytest.TerminalReporter) -> None:
    for line in REPORTER.terminal_summary_lines():
        terminalreporter.write_line(line)


@pytest.fixture(scope="session")
def hermes_config() -> HermesTestConfig:
    return load_test_config()


@pytest.fixture(scope="session")
def hermes_catalog(hermes_config: HermesTestConfig) -> ScenarioCatalog:
    return load_catalog(hermes_config.regression_dataset)


@pytest.fixture(scope="session")
def hermes_client(hermes_config: HermesTestConfig, hermes_catalog: ScenarioCatalog) -> HermesAPIClient:
    transport = None if hermes_config.is_live else MockHermesTransport(hermes_catalog, hermes_config.default_model)
    client = HermesAPIClient(hermes_config, transport=transport)
    yield client
    client.close()
    if hermes_config.report_path:
        REPORTER.write_json(hermes_config.report_path)


@pytest.fixture(scope="session")
def tool_cases(hermes_catalog: ScenarioCatalog) -> list[ScenarioCase]:
    return hermes_catalog.tool_cases


@pytest.fixture(scope="session")
def prompt_cases(hermes_catalog: ScenarioCatalog) -> list[ScenarioCase]:
    return hermes_catalog.prompt_cases


@pytest.fixture(scope="session")
def condition_cases(hermes_catalog: ScenarioCatalog) -> list[ConditionCase]:
    return hermes_catalog.condition_cases


@pytest.fixture(scope="session")
def regression_cases(hermes_catalog: ScenarioCatalog) -> list[ScenarioCase]:
    return hermes_catalog.regression_cases


@pytest.fixture
def report_observation(request: pytest.FixtureRequest) -> ScenarioObservation:
    observation = ScenarioObservation()
    yield observation
    report = getattr(request.node, "rep_call", None)
    passed = bool(report and report.excinfo is None)
    reason = ""
    if report and report.excinfo is not None:
        reason = str(report.excinfo.value)
    REPORTER.record(request.node.nodeid, observation, passed=passed, reason=reason)


@pytest.fixture(scope="session")
def sample_png_bytes() -> bytes:
    """1x1 PNG used for attachment upload scenarios."""

    return b64decode(
        "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Y9F9dYAAAAASUVORK5CYII="
    )


@pytest.fixture
def require_live_mode(hermes_config: HermesTestConfig) -> None:
    if not hermes_config.is_live:
        pytest.skip("live smoke tests require HERMES_TEST_MODE=live")


@pytest.fixture
def require_live_token(hermes_config: HermesTestConfig) -> None:
    if not hermes_config.auth_token:
        pytest.skip("live smoke tests require HERMES_API_TOKEN")

