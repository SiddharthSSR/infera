"""HTTP client abstraction for Hermes Agents API tests."""

from __future__ import annotations

import json
import logging
import time
from typing import Any, TypeVar

import httpx
from pydantic import BaseModel

from .config import HermesTestConfig
from .models import (
    AgentsListResponse,
    APICallResult,
    AttachmentEnvelope,
    ModelsListResponse,
    RunDetailResponse,
    RunEnvelope,
    RunsListResponse,
)

LOGGER = logging.getLogger(__name__)
ResponseModelT = TypeVar("ResponseModelT", bound=BaseModel)
TERMINAL_STATUSES = {"succeeded", "failed", "canceled"}


class HermesAPIError(RuntimeError):
    """Base error for Hermes API test failures."""


class HermesTransportError(HermesAPIError):
    """Raised when the HTTP transport itself fails."""


class HermesSchemaError(HermesAPIError):
    """Raised when the API returns malformed JSON or an unexpected schema."""


class HermesAPIClient:
    """Typed client for Hermes Agents API endpoints."""

    def __init__(self, config: HermesTestConfig, transport: httpx.BaseTransport | None = None) -> None:
        self.config = config
        headers: dict[str, str] = {}
        if config.auth_token:
            headers["Authorization"] = f"Bearer {config.auth_token}"
        self._client = httpx.Client(
            base_url=config.base_url,
            timeout=config.timeout_seconds,
            headers=headers,
            transport=transport,
        )

    def close(self) -> None:
        self._client.close()

    def _parse_response(self, response: httpx.Response, model: type[ResponseModelT] | None = None) -> APICallResult[ResponseModelT]:
        try:
            payload = response.json()
        except json.JSONDecodeError as exc:
            raise HermesSchemaError(f"Expected JSON from {response.request.method} {response.request.url.path}, got: {response.text}") from exc

        parsed = None
        if model is not None and response.status_code < 400:
            try:
                parsed = model.model_validate(payload)
            except Exception as exc:  # pragma: no cover - defensive wrapper
                raise HermesSchemaError(f"Schema validation failed for {response.request.url.path}: {exc}") from exc

        return APICallResult(
            status_code=response.status_code,
            headers=dict(response.headers),
            text=response.text,
            json_payload=payload,
            data=parsed,
        )

    def _request(
        self,
        method: str,
        path: str,
        *,
        json_body: dict[str, Any] | None = None,
        data: Any = None,
        files: Any = None,
        params: dict[str, Any] | None = None,
        model: type[ResponseModelT] | None = None,
        retry_on_rate_limit: bool = False,
    ) -> APICallResult[ResponseModelT]:
        attempts = self.config.rate_limit_retries if retry_on_rate_limit else 0
        last_response: httpx.Response | None = None

        for attempt in range(attempts + 1):
            try:
                response = self._client.request(
                    method,
                    path,
                    json=json_body,
                    data=data,
                    files=files,
                    params=params,
                )
            except httpx.TimeoutException as exc:
                raise HermesTransportError(f"{method} {path} timed out after {self.config.timeout_seconds}s") from exc
            except httpx.HTTPError as exc:
                raise HermesTransportError(f"{method} {path} failed: {exc}") from exc

            if response.status_code == 429 and attempt < attempts:
                retry_after = float(response.headers.get("Retry-After", "0") or 0)
                sleep_seconds = retry_after if retry_after > 0 else min(0.05 * (attempt + 1), 0.2)
                LOGGER.debug("Hermes API rate limited, retrying", extra={"path": path, "attempt": attempt + 1})
                time.sleep(sleep_seconds)
                last_response = response
                continue

            return self._parse_response(response, model=model)

        assert last_response is not None
        return self._parse_response(last_response, model=model)

    def list_agents(self) -> APICallResult[AgentsListResponse]:
        return self._request("GET", "/api/agents", model=AgentsListResponse)

    def list_inference_models(self) -> APICallResult[ModelsListResponse]:
        return self._request("GET", "/v1/models", model=ModelsListResponse)

    def list_runs(self) -> APICallResult[RunsListResponse]:
        return self._request("GET", "/api/agents/runs", model=RunsListResponse)

    def create_run(self, payload: dict[str, Any], *, retry_on_rate_limit: bool = False) -> APICallResult[RunEnvelope]:
        return self._request(
            "POST",
            "/api/agents/runs",
            json_body=payload,
            model=RunEnvelope,
            retry_on_rate_limit=retry_on_rate_limit,
        )

    def create_external_run(self, payload: dict[str, Any], *, wait: bool = False) -> APICallResult[RunDetailResponse]:
        return self._request(
            "POST",
            "/v1/agents/runs",
            json_body=payload,
            params={"wait": str(wait).lower()},
            model=RunDetailResponse if wait else RunEnvelope,
        )

    def get_run_detail(self, run_id: str) -> APICallResult[RunDetailResponse]:
        return self._request("GET", f"/api/agents/runs/{run_id}", model=RunDetailResponse)

    def cancel_run(self, run_id: str) -> APICallResult[RunEnvelope]:
        return self._request("POST", f"/api/agents/runs/{run_id}/cancel", model=RunEnvelope)

    def upload_attachment(self, file_name: str, content: bytes, mime_type: str) -> APICallResult[AttachmentEnvelope]:
        return self._request(
            "POST",
            "/api/agent-attachments",
            files={"file": (file_name, content, mime_type)},
            model=AttachmentEnvelope,
        )

    def wait_for_run(self, run_id: str, *, timeout_seconds: float | None = None) -> RunDetailResponse:
        deadline = time.time() + (timeout_seconds or self.config.max_wait_seconds)
        while time.time() < deadline:
            detail = self.get_run_detail(run_id).data
            assert detail is not None
            if detail.run.status in TERMINAL_STATUSES:
                return detail
            time.sleep(self.config.poll_interval_seconds)
        raise HermesTransportError(f"Run {run_id} did not reach a terminal state within the allotted time")
