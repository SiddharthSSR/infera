"""Tests for worker HTTP server metrics."""

import json
from types import SimpleNamespace
from unittest.mock import AsyncMock
import asyncio

import pytest
from aiohttp.test_utils import make_mocked_request
from prometheus_client import generate_latest

from infera_worker import http_server as http_server_module
from infera_worker.http_server import HTTPServer
from infera_worker.worker import Worker


@pytest.mark.asyncio
async def test_metrics_endpoint_exposes_prometheus_payload(mock_worker_config):
    worker = Worker(mock_worker_config)
    server = HTTPServer(worker, mock_worker_config)

    request = make_mocked_request("GET", "/metrics")
    response = await server.handle_metrics(request)

    assert response.status == 200
    body = response.body.decode()
    assert "infera_worker_inference_requests_total" in body
    assert "infera_worker_inference_duration_seconds" in body
    assert "infera_worker_loaded_models" in body
    assert "infera_worker_gateway_registration_total" in body
    assert response.headers["Content-Type"].startswith("text/plain")


def test_record_inference_metrics_updates_counters(mock_worker_config):
    worker = Worker(mock_worker_config)
    server = HTTPServer(worker, mock_worker_config)

    server._record_inference_metrics(
        stream=False,
        status="success",
        duration_seconds=0.25,
        token_count=64,
    )
    server._record_inference_metrics(
        stream=True,
        status="internal_error",
        duration_seconds=0.15,
        token_count=0,
    )

    metrics = generate_latest(server._metrics_registry).decode()
    assert "infera_worker_inference_requests_total" in metrics
    assert 'status="success"' in metrics
    assert 'stream="false"' in metrics
    assert 'status="internal_error"' in metrics
    assert 'stream="true"' in metrics
    assert "infera_worker_inference_tokens_total 64.0" in metrics


def test_refresh_runtime_gauges_populates_resource_metrics(mock_worker_config):
    worker = Worker(mock_worker_config)
    server = HTTPServer(worker, mock_worker_config)

    server._refresh_runtime_gauges()
    metrics = generate_latest(server._metrics_registry).decode()

    assert "infera_worker_loaded_models" in metrics
    assert "infera_worker_memory_used_bytes" in metrics
    assert "infera_worker_memory_total_bytes" in metrics
    assert "infera_worker_gpu_utilization" in metrics


def test_gateway_metrics_and_worker_info_are_exposed(mock_worker_config):
    worker = Worker(mock_worker_config)
    server = HTTPServer(worker, mock_worker_config)

    server._record_gateway_registration("success")
    server._record_gateway_heartbeat("success")

    metrics = generate_latest(server._metrics_registry).decode()

    assert "infera_worker_gateway_registration_total" in metrics
    assert 'status="success"' in metrics
    assert "infera_worker_gateway_heartbeats_total" in metrics
    assert "infera_worker_info" in metrics
    assert f'worker_id="{worker.worker_id}"' in metrics
    assert f'engine="{mock_worker_config.engine}"' in metrics
    assert 'provider="local"' in metrics
    assert 'env="development"' in metrics
    assert 'version="dev"' in metrics


def test_explicit_worker_address_overrides_provider_derived_address(mock_worker_config, monkeypatch):
    worker = Worker(mock_worker_config)
    server = HTTPServer(worker, mock_worker_config)

    monkeypatch.setenv("INFERA_WORKER_ADDRESS", "worker.internal:9999")
    monkeypatch.setenv("RUNPOD_POD_ID", "pod-123")
    monkeypatch.setenv("RUNPOD_PUBLIC_IP", "203.0.113.10")

    assert server._get_worker_address() == "worker.internal:9999"


@pytest.mark.asyncio
async def test_streaming_invalid_request_returns_error_before_prepare(mock_worker_config):
    worker = Worker(mock_worker_config)
    server = HTTPServer(worker, mock_worker_config)

    request = make_mocked_request("POST", "/infer/stream")
    request.json = AsyncMock(return_value={})

    response = await server.handle_infer_stream(request)

    assert response.status == 400
    assert response.body is not None

    metrics = generate_latest(server._metrics_registry).decode()
    assert "infera_worker_inference_requests_total" in metrics
    assert 'status="invalid_request"' in metrics
    assert 'stream="true"' in metrics


@pytest.mark.asyncio
async def test_gateway_client_is_reused_and_closed_on_stop(mock_worker_config, monkeypatch):
    created_clients: list[object] = []

    class FakeAsyncClient:
        def __init__(self):
            self.is_closed = False
            self.closed = False
            created_clients.append(self)

        async def aclose(self):
            self.is_closed = True
            self.closed = True

        async def post(self, *_args, **_kwargs):
            return SimpleNamespace(status_code=200, text="", is_error=False)

        async def delete(self, *_args, **_kwargs):
            return SimpleNamespace(status_code=200, text="", is_error=False)

    monkeypatch.setattr(http_server_module.httpx, "AsyncClient", FakeAsyncClient)

    worker = Worker(mock_worker_config)
    server = HTTPServer(worker, mock_worker_config)

    client_a = server._ensure_gateway_client()
    client_b = server._ensure_gateway_client()

    assert client_a is client_b
    assert len(created_clients) == 1

    server._gateway_registered = True
    await server.stop()

    assert len(created_clients) == 1
    assert created_clients[0].closed is True
    assert server._gateway_client is None


@pytest.mark.asyncio
async def test_health_endpoint_reports_live_but_not_ready_while_initializing(mock_worker_config):
    worker = Worker(mock_worker_config)
    server = HTTPServer(worker, mock_worker_config)

    request = make_mocked_request("GET", "/health")
    response = await server.handle_health(request)

    assert response.status == 200
    payload = json.loads(response.body.decode())
    assert payload["state"] == "initializing"
    assert payload["live"] is True
    assert payload["ready"] is False
    assert "worker_created" in payload["startup"]["stages"]


@pytest.mark.asyncio
async def test_activate_gateway_reporting_registers_once_worker_is_ready(mock_worker_config, monkeypatch):
    worker = Worker(mock_worker_config)
    server = HTTPServer(worker, mock_worker_config)
    await worker.start()

    register = AsyncMock()
    monkeypatch.setattr(server, "_register_with_gateway", register)

    async def fake_heartbeat_loop():
        await asyncio.Future()

    monkeypatch.setattr(server, "_heartbeat_loop", fake_heartbeat_loop)

    try:
        await server.activate_gateway_reporting()
        register.assert_awaited_once()
        assert server._gateway_registered is True
        assert server._heartbeat_task is not None
        startup = worker.get_startup_status()
        assert "gateway_registered" in startup["stages"]
    finally:
        if server._heartbeat_task is not None:
            server._heartbeat_task.cancel()
            await asyncio.gather(server._heartbeat_task, return_exceptions=True)
