"""Security boundary tests for the worker HTTP API."""

from pathlib import Path

import pytest
from aiohttp.test_utils import TestClient, TestServer

from infera_worker.config import WorkerConfig
from infera_worker.http_server import HTTPServer
from infera_worker.worker import Worker


async def make_client(config: WorkerConfig) -> tuple[Worker, TestClient]:
    worker = Worker(config)
    server = HTTPServer(worker, config)
    client = TestClient(TestServer(server.app))
    await client.start_server()
    return worker, client


@pytest.mark.asyncio
async def test_health_is_public_but_worker_operations_require_token():
    config = WorkerConfig(engine="mock", worker_shared_token="worker-secret")
    _worker, client = await make_client(config)

    try:
        health = await client.get("/health")
        assert health.status == 200

        for method, path in (
            (client.get, "/metrics"),
            (client.get, "/models"),
            (client.get, "/stats"),
            (client.post, "/infer"),
            (client.post, "/infer/stream"),
            (client.post, "/models/load"),
            (client.post, "/models/unload"),
        ):
            response = await method(path)
            assert response.status == 401, path

            response = await method(path, headers={"X-Worker-Token": "wrong-secret"})
            assert response.status == 401, path
    finally:
        await client.close()


@pytest.mark.asyncio
async def test_worker_api_fails_closed_without_configured_token():
    config = WorkerConfig(engine="mock", worker_shared_token="")
    _worker, client = await make_client(config)

    try:
        response = await client.get("/models", headers={"X-Worker-Token": "any-token"})
        assert response.status == 503
    finally:
        await client.close()


@pytest.mark.asyncio
async def test_valid_token_allows_approved_model_load_only():
    config = WorkerConfig(
        engine="mock",
        worker_shared_token="worker-secret",
        preload_models=["approved/model"],
    )
    worker, client = await make_client(config)
    await worker.start()

    try:
        headers = {"X-Worker-Token": "worker-secret"}
        models = await client.get("/models", headers=headers)
        assert models.status == 200

        inference = await client.post(
            "/infer",
            headers=headers,
            json={
                "request_id": "security-control-request",
                "model_id": "approved/model",
                "messages": [{"role": "user", "content": "Hello"}],
                "parameters": {},
            },
        )
        assert inference.status == 200

        unapproved = await client.post(
            "/models/load",
            headers=headers,
            json={"model_id": "attacker/model"},
        )
        assert unapproved.status == 403

        overridden = await client.post(
            "/models/load",
            headers=headers,
            json={
                "model_id": "approved/model",
                "model_path": "attacker/model",
                "revision": "malicious-revision",
            },
        )
        assert overridden.status == 400

        approved = await client.post(
            "/models/load",
            headers=headers,
            json={"model_id": "approved/model"},
        )
        assert approved.status == 200
    finally:
        await worker.stop()
        await client.close()


def test_remote_model_code_is_disabled_by_default():
    assert WorkerConfig().trust_remote_code is False


def test_worker_container_defaults_disable_remote_code_and_bound_model_cache():
    deploy_dir = Path(__file__).resolve().parents[2] / "deploy" / "docker"
    for name in (
        "Dockerfile.worker.vllm",
        "Dockerfile.worker.sglang",
        "Dockerfile.worker.tensorrt_llm",
        "Dockerfile.worker.e2e",
    ):
        dockerfile = (deploy_dir / name).read_text()
        assert "ENV INFERA_TRUST_REMOTE_CODE=false" in dockerfile, name
        assert "ENV INFERA_MODEL_CACHE_SIZE=2" in dockerfile, name
