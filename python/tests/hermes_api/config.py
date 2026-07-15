"""Environment-driven configuration for Hermes API tests.

All API assumptions live here so the rest of the suite can stay declarative:
- Hermes agent discovery is exposed at ``/api/agents``.
- Runs are created through ``/api/agents/runs`` and can optionally be exercised
  through ``/v1/agents/runs`` with ``wait=true``.
- Auth, when required, uses a Bearer token.
- The default built-in agent id is ``hermes``.
"""

from __future__ import annotations

import os
from dataclasses import dataclass
from pathlib import Path
from typing import Literal

TestMode = Literal["mock", "live"]


@dataclass(frozen=True)
class HermesTestConfig:
    """Runtime configuration for the Hermes API suite."""

    mode: TestMode
    base_url: str
    auth_token: str
    default_model: str
    timeout_seconds: float
    poll_interval_seconds: float
    max_wait_seconds: float
    rate_limit_retries: int
    report_path: Path | None
    regression_dataset: Path | None

    @property
    def is_live(self) -> bool:
        return self.mode == "live"


def _optional_path(raw: str) -> Path | None:
    value = raw.strip()
    return Path(value).expanduser() if value else None


def load_test_config() -> HermesTestConfig:
    """Build the suite config from environment variables."""

    mode = os.getenv("HERMES_TEST_MODE", "mock").strip().lower() or "mock"
    if mode not in {"mock", "live"}:
        raise ValueError("HERMES_TEST_MODE must be either 'mock' or 'live'")

    return HermesTestConfig(
        mode=mode,  # type: ignore[arg-type]
        base_url=os.getenv("HERMES_API_BASE_URL", "http://localhost:8080").rstrip("/"),
        auth_token=os.getenv("HERMES_API_TOKEN", "").strip(),
        default_model=os.getenv("HERMES_API_MODEL", "Qwen/Qwen2.5-7B-Instruct").strip(),
        timeout_seconds=float(os.getenv("HERMES_API_TIMEOUT_SECONDS", "10")),
        poll_interval_seconds=float(os.getenv("HERMES_API_POLL_INTERVAL_SECONDS", "0.05")),
        max_wait_seconds=float(os.getenv("HERMES_API_MAX_WAIT_SECONDS", "5")),
        rate_limit_retries=int(os.getenv("HERMES_API_RATE_LIMIT_RETRIES", "1")),
        report_path=_optional_path(os.getenv("HERMES_API_REPORT_PATH", "")),
        regression_dataset=_optional_path(os.getenv("HERMES_REGRESSION_DATASET", "")),
    )
