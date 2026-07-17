"""Deterministic coverage for gateway heartbeat registration recovery."""

from types import SimpleNamespace
from unittest.mock import AsyncMock

import httpx
import pytest
from prometheus_client import generate_latest

from infera_worker.http_server import HTTPServer
from infera_worker.worker import Worker


def heartbeat_metrics(server: HTTPServer) -> str:
    return generate_latest(server._metrics_registry).decode()


@pytest.mark.asyncio
async def test_acknowledged_heartbeat_records_success(mock_worker_config, monkeypatch):
    server = HTTPServer(Worker(mock_worker_config), mock_worker_config)
    server._gateway_registered = True
    server._consecutive_auth_failures = 3
    register = AsyncMock()
    monkeypatch.setattr(server, "_register_with_gateway", register)

    response = httpx.Response(200, json={"acknowledged": True})
    await server._handle_gateway_heartbeat_ack(response, "http://gateway/heartbeat")

    register.assert_not_awaited()
    assert server._gateway_registered is True
    assert server._consecutive_auth_failures == 0
    assert 'status="success"' in heartbeat_metrics(server)


@pytest.mark.asyncio
async def test_unacknowledged_heartbeat_reregisters_with_rollout_identity(
    mock_worker_config, monkeypatch
):
    captured: dict[str, object] = {}

    class FakeGatewayClient:
        is_closed = False

        async def post(self, _url, **kwargs):
            assert server._gateway_registered is False
            captured.update(kwargs)
            return SimpleNamespace(status_code=200, text="", is_error=False)

    mock_worker_config.worker_shared_token = "deployment-credential"
    mock_worker_config.release_id = "release-2"
    mock_worker_config.worker_protocol_version = "2"
    server = HTTPServer(Worker(mock_worker_config), mock_worker_config)
    server._gateway_registered = True
    monkeypatch.setattr(server, "_ensure_gateway_client", lambda: FakeGatewayClient())

    response = httpx.Response(200, json={"acknowledged": False})
    await server._handle_gateway_heartbeat_ack(response, "http://gateway/heartbeat")

    assert server._gateway_registered is True
    assert captured["headers"] == {"X-Worker-Token": "deployment-credential"}
    payload = captured["json"]
    assert payload["release_id"] == "release-2"
    assert payload["protocol_version"] == "2"
    metrics = heartbeat_metrics(server)
    assert 'status="registration_lost"' in metrics
    assert 'infera_worker_gateway_heartbeats_total{status="success"}' not in metrics


@pytest.mark.asyncio
async def test_failed_reregistration_keeps_heartbeats_paused(mock_worker_config, monkeypatch):
    server = HTTPServer(Worker(mock_worker_config), mock_worker_config)
    server._gateway_registered = True
    register = AsyncMock(side_effect=RuntimeError("gateway unavailable"))
    monkeypatch.setattr(server, "_register_with_gateway", register)

    response = httpx.Response(200, json={"acknowledged": False})
    await server._handle_gateway_heartbeat_ack(response, "http://gateway/heartbeat")

    register.assert_awaited_once()
    assert server._gateway_registered is False
    assert 'status="registration_lost"' in heartbeat_metrics(server)


@pytest.mark.asyncio
async def test_malformed_heartbeat_ack_is_not_treated_as_success(
    mock_worker_config, monkeypatch
):
    server = HTTPServer(Worker(mock_worker_config), mock_worker_config)
    server._gateway_registered = True
    register = AsyncMock()
    monkeypatch.setattr(server, "_register_with_gateway", register)

    response = httpx.Response(200, content=b"not-json")
    await server._handle_gateway_heartbeat_ack(response, "http://gateway/heartbeat")

    register.assert_awaited_once()
    assert server._gateway_registered is True
    metrics = heartbeat_metrics(server)
    assert 'status="invalid_response"' in metrics
    assert 'infera_worker_gateway_heartbeats_total{status="success"}' not in metrics


@pytest.mark.asyncio
async def test_heartbeat_auth_rejections_still_request_shutdown(
    mock_worker_config, monkeypatch
):
    client = SimpleNamespace(
        post=AsyncMock(return_value=SimpleNamespace(status_code=401, text="unauthorized"))
    )
    server = HTTPServer(Worker(mock_worker_config), mock_worker_config)
    server._gateway_registered = True
    monkeypatch.setattr(server, "_ensure_gateway_client", lambda: client)
    monkeypatch.setattr("infera_worker.http_server.asyncio.sleep", AsyncMock())

    await server._heartbeat_loop()

    assert client.post.await_count == 10
    assert server._consecutive_auth_failures == 10
    assert server.worker._shutdown_event.is_set()
    assert server._gateway_registered is True
    assert 'status="auth_rejected"' in heartbeat_metrics(server)
