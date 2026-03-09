"""Tests for worker-to-gateway auth headers."""

from infera_worker.config import WorkerConfig


def test_get_gateway_headers_empty_token() -> None:
    config = WorkerConfig(worker_shared_token="")
    assert config.gateway_headers() == {}


def test_get_gateway_headers_with_token() -> None:
    config = WorkerConfig(worker_shared_token="worker-secret")
    assert config.gateway_headers() == {"X-Worker-Token": "worker-secret"}
