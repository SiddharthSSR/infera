"""Pytest configuration and fixtures."""

import asyncio
import sys
from pathlib import Path

import pytest

REPO_ROOT = Path(__file__).resolve().parents[2]
PYTHON_SRC = REPO_ROOT / "python" / "src"
if str(PYTHON_SRC) not in sys.path:
    sys.path.insert(0, str(PYTHON_SRC))


@pytest.fixture(scope="session")
def event_loop():
    """Create an event loop for async tests."""
    loop = asyncio.new_event_loop()
    yield loop
    loop.close()


@pytest.fixture
def mock_worker_config():
    """Create a mock worker configuration."""
    from infera_worker.config import WorkerConfig

    return WorkerConfig(
        engine="mock",
        http_port=8081,
        router_address="localhost:8080",
        max_concurrent_requests=32,
    )


@pytest.fixture
def sample_messages():
    """Create sample messages for testing."""
    from infera_worker.types import Message, Role

    return [
        Message(role=Role.SYSTEM, content="You are a helpful assistant."),
        Message(role=Role.USER, content="Hello!"),
    ]
