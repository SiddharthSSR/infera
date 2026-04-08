"""Deterministic mock Hermes API transport for CI-friendly API testing."""

from __future__ import annotations

import hashlib
import json
import uuid
from copy import deepcopy
from dataclasses import dataclass
from datetime import datetime, timedelta, timezone
from email.parser import BytesParser
from email.policy import default
from typing import Any
from urllib.parse import parse_qs

import httpx

from .models import ConditionCase, ScenarioCase, ScenarioCatalog
from .tool_catalog import (
    CURRENT_HERMES_TOOL_DESCRIPTIONS,
    CURRENT_HERMES_TOOL_MODES,
    CURRENT_HERMES_TOOL_ORDER,
    build_tool_result,
)


def _utc_now(offset_seconds: int = 0) -> str:
    return (datetime(2026, 4, 9, tzinfo=timezone.utc) + timedelta(seconds=offset_seconds)).isoformat().replace("+00:00", "Z")


@dataclass
class MockRunExecution:
    run: dict[str, Any]
    detail_sequence: list[dict[str, Any]]
    malformed_detail_once: bool = False
    timeout_detail_once: bool = False
    detail_calls: int = 0


class MockHermesTransport(httpx.BaseTransport):
    """A transport-level mock that exercises the public Hermes API contract."""

    def __init__(self, catalog: ScenarioCatalog, default_model: str) -> None:
        self.catalog = catalog
        self.default_model = default_model
        self.attachments: dict[str, dict[str, Any]] = {}
        self.runs: dict[str, MockRunExecution] = {}
        self.run_order: list[str] = []
        self.rate_limit_attempts: dict[str, int] = {}

        self.prompt_cases: dict[str, ScenarioCase] = {
            case.prompt: case
            for case in [*catalog.tool_cases, *catalog.prompt_cases, *catalog.regression_cases]
        }
        self.condition_by_prompt: dict[str, ConditionCase] = {
            case.prompt: case for case in catalog.condition_cases if case.prompt
        }

    def handle_request(self, request: httpx.Request) -> httpx.Response:
        path = request.url.path
        method = request.method.upper()

        if method == "GET" and path == "/api/agents":
            return self._json_response(request, 200, self._agents_payload())
        if method == "POST" and path == "/api/agent-attachments":
            return self._handle_attachment_upload(request)
        if method == "GET" and path == "/api/agents/runs":
            return self._json_response(request, 200, self._list_runs_payload())
        if method == "POST" and path == "/api/agents/runs":
            return self._handle_create_run(request, external=False)
        if method == "POST" and path == "/v1/agents/runs":
            return self._handle_create_run(request, external=True)
        if path.startswith("/api/agents/runs/"):
            return self._handle_run_route(request)

        return self._error_response(request, 404, "not_found", f"Unsupported mock endpoint: {method} {path}")

    def close(self) -> None:
        """Match the transport interface used by httpx.Client."""

        return None

    def _agents_payload(self) -> dict[str, Any]:
        return {
            "default_agent_id": "hermes",
            "agents": [
                {
                    "id": "hermes",
                    "name": "Hermes",
                    "description": "Read-only workspace health copilot for runtime visibility, deployment state, provider connectivity, external research, and screenshot-based investigation.",
                    "default_max_steps": 8,
                    "tools": [
                        {
                            "name": tool_name,
                            "description": CURRENT_HERMES_TOOL_DESCRIPTIONS[tool_name],
                            "modes": CURRENT_HERMES_TOOL_MODES[tool_name],
                        }
                        for tool_name in CURRENT_HERMES_TOOL_ORDER
                    ],
                }
            ],
        }

    def _list_runs_payload(self) -> dict[str, Any]:
        runs = [deepcopy(self.runs[run_id].run) for run_id in reversed(self.run_order)]
        return {"runs": runs, "total": len(runs)}

    def _handle_attachment_upload(self, request: httpx.Request) -> httpx.Response:
        content_type = request.headers.get("content-type", "")
        if "multipart/form-data" not in content_type:
            return self._error_response(request, 400, "invalid_request", "Expected multipart form upload")

        request.read()
        message = BytesParser(policy=default).parsebytes(
            b"Content-Type: " + content_type.encode("utf-8") + b"\r\nMIME-Version: 1.0\r\n\r\n" + request.content
        )
        upload_part = None
        for part in message.iter_parts():
            if part.get_param("name", header="content-disposition") == "file":
                upload_part = part
                break
        if upload_part is None:
            return self._error_response(request, 400, "invalid_request", "file is required")

        filename = upload_part.get_filename() or "upload.bin"
        payload = upload_part.get_payload(decode=True) or b""
        mime_type = upload_part.get_content_type()
        if mime_type not in {"image/png", "image/jpeg", "image/webp"}:
            return self._error_response(request, 400, "invalid_request", "Only PNG, JPEG, and WEBP screenshots are supported")
        if len(payload) > 8 << 20:
            return self._error_response(request, 400, "invalid_request", "Screenshot exceeds the 8 MiB upload limit")

        attachment_id = f"att_{len(self.attachments) + 1}"
        attachment = {
            "id": attachment_id,
            "workspace_id": "ws_alpha",
            "created_by_key_id": "test-key",
            "file_name": filename,
            "mime_type": mime_type,
            "size_bytes": len(payload),
            "width": 1280,
            "height": 720,
            "sha256": hashlib.sha256(payload).hexdigest(),
            "created_at": _utc_now(),
        }
        self.attachments[attachment_id] = attachment
        return self._json_response(request, 201, {"attachment": attachment})

    def _handle_create_run(self, request: httpx.Request, *, external: bool) -> httpx.Response:
        try:
            body = json.loads(request.content.decode("utf-8") or "{}")
        except json.JSONDecodeError:
            return self._error_response(request, 400, "invalid_request", "Invalid JSON")

        model = str(body.get("model", "")).strip()
        prompt = str(body.get("input", "")).strip()
        mode = str(body.get("mode", "")).strip() or "operations"
        analysis_depth = str(body.get("analysis_depth", "")).strip() or "standard"
        attachments = list(body.get("attachments") or [])

        if not model:
            return self._error_response(request, 400, "invalid_request", "model is required")
        if not prompt:
            return self._error_response(request, 400, "invalid_request", "input is required")
        if mode != "multimodal" and attachments:
            return self._error_response(request, 400, "invalid_request", "attachments are only valid for multimodal runs")
        for attachment_id in attachments:
            if attachment_id not in self.attachments:
                return self._error_response(request, 400, "invalid_request", f"attachment {attachment_id!r} is unavailable for this run")

        condition_case = self.condition_by_prompt.get(prompt)
        if condition_case and condition_case.mock_behavior == "rate_limit_then_success":
            seen = self.rate_limit_attempts.get(prompt, 0)
            self.rate_limit_attempts[prompt] = seen + 1
            if seen == 0:
                return self._json_response(
                    request,
                    429,
                    {"error": {"type": "rate_limited", "message": "Rate limit exceeded. Retry after 0 seconds."}},
                    headers={"Retry-After": "0"},
                )

        case = self.prompt_cases.get(prompt)
        if case is None and condition_case is None:
            fallback = ScenarioCase(
                id="fallback-generic",
                title="Fallback generic run",
                category="prompt",
                prompt=prompt,
                mode=mode,  # type: ignore[arg-type]
                analysis_depth=analysis_depth,  # type: ignore[arg-type]
                expected_tools=[],
                final_output=f"Hermes completed the request for: {prompt}",
                response_keywords=["Hermes"],
            )
            case = fallback

        execution = self._build_execution(case=case, condition=condition_case, body=body)
        self.runs[execution.run["id"]] = execution
        self.run_order.append(execution.run["id"])

        wait = parse_qs(request.url.query.decode("utf-8")).get("wait", ["false"])[0] == "true"
        if external and wait:
            if condition_case and condition_case.mock_behavior == "wait_timeout":
                detail = deepcopy(execution.detail_sequence[0])
                detail["timed_out"] = True
                return self._json_response(request, 200, detail)
            return self._json_response(request, 200, deepcopy(execution.detail_sequence[-1]))

        return self._json_response(request, 201, {"run": deepcopy(execution.run)})

    def _handle_run_route(self, request: httpx.Request) -> httpx.Response:
        path = request.url.path.removeprefix("/api/agents/runs/")
        parts = [part for part in path.split("/") if part]
        if not parts:
            return self._error_response(request, 400, "invalid_request", "Run ID is required")

        run_id = parts[0]
        execution = self.runs.get(run_id)
        if execution is None:
            return self._error_response(request, 404, "not_found", "Run not found")

        if request.method.upper() == "POST" and len(parts) == 2 and parts[1] == "cancel":
            execution.run["status"] = "canceled"
            execution.run["failure_reason"] = "canceled by user"
            execution.run["updated_at"] = _utc_now(3)
            execution.run["finished_at"] = _utc_now(3)
            canceled = deepcopy(execution.run)
            return self._json_response(request, 200, {"run": canceled})

        if request.method.upper() != "GET" or len(parts) != 1:
            return self._error_response(request, 405, "method_not_allowed", "Unsupported run action")

        if execution.malformed_detail_once:
            execution.malformed_detail_once = False
            return httpx.Response(
                200,
                text='{"run": invalid',
                headers={"Content-Type": "application/json"},
                request=request,
            )
        if execution.timeout_detail_once:
            execution.timeout_detail_once = False
            raise httpx.ReadTimeout("simulated Hermes detail timeout", request=request)

        index = min(execution.detail_calls, len(execution.detail_sequence) - 1)
        execution.detail_calls += 1
        detail = deepcopy(execution.detail_sequence[index])
        execution.run = deepcopy(detail["run"])
        return self._json_response(request, 200, detail)

    def _build_execution(
        self,
        *,
        case: ScenarioCase | None,
        condition: ConditionCase | None,
        body: dict[str, Any],
    ) -> MockRunExecution:
        run_id = f"run_{uuid.uuid4().hex[:8]}"
        mode = str(body.get("mode", "")).strip() or (case.mode if case else condition.mode if condition else "operations")
        analysis_depth = str(body.get("analysis_depth", "")).strip() or (
            case.analysis_depth if case else condition.analysis_depth if condition else "standard"
        )
        attachments = [deepcopy(self.attachments[attachment_id]) for attachment_id in body.get("attachments", []) if attachment_id in self.attachments]
        prompt = str(body.get("input", "")).strip()
        max_steps = int(body.get("max_steps") or (case.max_steps if case else 8))

        base_run = {
            "id": run_id,
            "workspace_id": "ws_alpha",
            "created_by_key_id": "test-key",
            "agent_id": str(body.get("agent_id") or "hermes"),
            "mode": mode,
            "analysis_depth": analysis_depth,
            "model": body["model"],
            "input": prompt,
            "status": "queued",
            "max_steps": max_steps,
            "current_step": 0,
            "created_at": _utc_now(),
            "updated_at": _utc_now(),
        }

        expected_tools = list(case.expected_tools if case else condition.expected_tools if condition else [])
        expected_arguments = deepcopy(case.expected_arguments if case else condition.expected_arguments if condition else {})
        final_output = case.final_output if case else (condition.final_output or "")
        expected_status = case.expected_status if case else "succeeded"
        behavior = case.mock_behavior if case else condition.mock_behavior if condition else "success"

        steps: list[dict[str, Any]] = []
        for tool_name in expected_tools:
            arguments = deepcopy(expected_arguments.get(tool_name, {}))
            if "attachment_id" in arguments and arguments["attachment_id"] == "$attachment_id" and attachments:
                arguments["attachment_id"] = attachments[0]["id"]
            steps.append(
                {
                    "id": len(steps) + 1,
                    "run_id": run_id,
                    "index": len(steps),
                    "type": "tool_call",
                    "tool_name": tool_name,
                    "payload": {"arguments": arguments},
                    "created_at": _utc_now(len(steps) + 1),
                }
            )

            if behavior == "tool_invalid_arguments":
                steps.append(
                    {
                        "id": len(steps) + 1,
                        "run_id": run_id,
                        "index": len(steps),
                        "type": "error",
                        "tool_name": tool_name,
                        "payload": {"error": "invalid_tool_arguments", "message": f"{tool_name} arguments were rejected"},
                        "created_at": _utc_now(len(steps) + 1),
                    }
                )
                expected_status = "failed"
                final_output = ""
                break

            if behavior == "tool_failure_then_final":
                result_payload = {"ok": False, "error": f"{tool_name} failed in the mock transport"}
            elif behavior == "partial_success" and tool_name == expected_tools[0]:
                result_payload = {"ok": False, "error": f"{tool_name} returned partial data"}
            else:
                attachment_model = None
                if attachments:
                    attachment_model = type("AttachmentShim", (), attachments[0])()  # simple attribute shim
                result_payload = {"ok": True, "result": build_tool_result(tool_name, arguments, attachment_model)}

            steps.append(
                {
                    "id": len(steps) + 1,
                    "run_id": run_id,
                    "index": len(steps),
                    "type": "tool_result",
                    "tool_name": tool_name,
                    "payload": result_payload,
                    "created_at": _utc_now(len(steps) + 1),
                }
            )

        if final_output:
            steps.append(
                {
                    "id": len(steps) + 1,
                    "run_id": run_id,
                    "index": len(steps),
                    "type": "final",
                    "payload": {"message": final_output},
                    "created_at": _utc_now(len(steps) + 1),
                }
            )

        sources = self._derive_sources(steps)
        terminal_status = expected_status if behavior != "missing_attachment_final" else "succeeded"
        failure_reason = None
        if terminal_status == "failed":
            failure_reason = "tool call arguments must be a JSON object"

        queued_detail = {
            "run": deepcopy(base_run),
            "steps": [],
            "attachments": attachments,
            "sources": [],
        }

        running_steps = steps[:-1] if steps and steps[-1]["type"] == "final" else steps
        running_detail = {
            "run": {
                **deepcopy(base_run),
                "status": "running",
                "current_step": max(0, len(running_steps) - 1),
                "updated_at": _utc_now(2),
                "started_at": _utc_now(1),
            },
            "steps": running_steps,
            "attachments": attachments,
            "sources": self._derive_sources(running_steps),
        }

        terminal_detail = {
            "run": {
                **deepcopy(base_run),
                "status": terminal_status,
                "current_step": max(0, len(steps) - 1),
                "updated_at": _utc_now(3),
                "started_at": _utc_now(1),
                "finished_at": _utc_now(3),
                "final_output": final_output or None,
                "failure_reason": failure_reason,
            },
            "steps": steps,
            "attachments": attachments,
            "sources": sources,
        }

        detail_sequence = [queued_detail, terminal_detail if not running_steps else running_detail, terminal_detail]
        if behavior == "missing_attachment_final":
            detail_sequence = [queued_detail, terminal_detail]

        return MockRunExecution(
            run=deepcopy(base_run),
            detail_sequence=detail_sequence,
            malformed_detail_once=bool(condition and condition.mock_behavior == "malformed_detail"),
            timeout_detail_once=bool(condition and condition.mock_behavior == "transport_timeout"),
        )

    def _derive_sources(self, steps: list[dict[str, Any]]) -> list[dict[str, Any]]:
        sources: list[dict[str, Any]] = []
        seen: set[str] = set()
        for step in steps:
            if step.get("type") != "tool_result" or step.get("tool_name") != "web_search":
                continue
            payload = step.get("payload", {})
            result = payload.get("result", {}) if isinstance(payload, dict) else {}
            for source in result.get("results", []) if isinstance(result, dict) else []:
                url = source.get("url", "")
                if url and url not in seen:
                    seen.add(url)
                    sources.append(source)
        return sources

    def _json_response(
        self,
        request: httpx.Request,
        status_code: int,
        payload: dict[str, Any],
        *,
        headers: dict[str, str] | None = None,
    ) -> httpx.Response:
        merged_headers = {"Content-Type": "application/json"}
        if headers:
            merged_headers.update(headers)
        return httpx.Response(status_code, json=payload, headers=merged_headers, request=request)

    def _error_response(self, request: httpx.Request, status_code: int, err_type: str, message: str) -> httpx.Response:
        return self._json_response(request, status_code, {"error": {"type": err_type, "message": message}})
