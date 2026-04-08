"""Session-level reporting for Hermes API scenario runs."""

from __future__ import annotations

import json
from dataclasses import asdict, dataclass, field
from pathlib import Path
from typing import Any

from .models import ScenarioReportRecord


@dataclass
class ScenarioObservation:
    """Mutable per-test observation that is finalized after the test completes."""

    scenario_id: str = ""
    category: str = ""
    prompt: str = ""
    expected_tools: list[str] = field(default_factory=list)
    actual_tools: list[str] = field(default_factory=list)
    reason: str = ""

    def set_case(self, scenario_id: str, category: str, prompt: str, expected_tools: list[str]) -> None:
        self.scenario_id = scenario_id
        self.category = category
        self.prompt = prompt
        self.expected_tools = list(expected_tools)

    def set_actual_tools(self, tools: list[str]) -> None:
        self.actual_tools = list(tools)


class HermesReportRecorder:
    """Collects scenario-level outcomes and emits a compact summary."""

    def __init__(self) -> None:
        self.records: list[ScenarioReportRecord] = []

    def record(self, test_name: str, observation: ScenarioObservation, passed: bool, reason: str) -> None:
        if not observation.scenario_id:
            return
        self.records.append(
            ScenarioReportRecord(
                test_name=test_name,
                scenario_id=observation.scenario_id,
                category=observation.category,
                prompt=observation.prompt,
                expected_tools=observation.expected_tools,
                actual_tools=observation.actual_tools,
                outcome="passed" if passed else "failed",
                reason=reason or observation.reason,
            )
        )

    def terminal_summary_lines(self) -> list[str]:
        if not self.records:
            return ["Hermes API summary: no scenario records were captured."]
        passed = sum(1 for record in self.records if record.outcome == "passed")
        failed = len(self.records) - passed
        lines = [
            f"Hermes API summary: {passed} passed / {failed} failed / {len(self.records)} total recorded scenarios",
        ]
        for record in self.records:
            lines.append(
                f"- {record.scenario_id}: {record.outcome.upper()} | expected={record.expected_tools or ['<none>']} | actual={record.actual_tools or ['<none>']} | reason={record.reason or 'ok'}"
            )
        return lines

    def write_json(self, path: Path) -> None:
        path.parent.mkdir(parents=True, exist_ok=True)
        payload: dict[str, Any] = {
            "records": [asdict(record) for record in self.records],
            "summary": {
                "total": len(self.records),
                "passed": sum(1 for record in self.records if record.outcome == "passed"),
                "failed": sum(1 for record in self.records if record.outcome == "failed"),
            },
        }
        path.write_text(json.dumps(payload, indent=2), encoding="utf-8")

