"""Tests for worker HTTP server metrics."""

from unittest.mock import AsyncMock

import pytest
from aiohttp.test_utils import make_mocked_request
from prometheus_client import generate_latest

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
