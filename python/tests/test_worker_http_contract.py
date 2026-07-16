"""Shared worker HTTP contract tests."""

from __future__ import annotations

import json
from pathlib import Path

from infera_worker.http_server import HTTPServer
from infera_worker.types import (
    Choice,
    FinishReason,
    FunctionCall,
    InferenceResponse,
    LatencyStats,
    Message,
    Role,
    TokenChunk,
    ToolCall,
    ToolCallChunkDelta,
    UsageStats,
)
from infera_worker.worker import Worker

FIXTURE_DIR = Path(__file__).resolve().parents[2] / "contracts" / "worker_http"


def load_fixture(name: str) -> dict:
    return json.loads((FIXTURE_DIR / name).read_text())


def test_parse_request_matches_shared_contract_fixture(mock_worker_config):
    worker = Worker(mock_worker_config)
    server = HTTPServer(worker, mock_worker_config)

    data = load_fixture("infer_request_tool_calls.json")
    parsed = server._parse_request(data)

    assert parsed.request_id == data["request_id"]
    assert parsed.model_id == data["model_id"]
    assert parsed.stream is False
    assert parsed.tool_choice == data["tool_choice"]
    assert parsed.parameters.max_tokens == data["parameters"]["max_tokens"]
    assert parsed.parameters.temperature == data["parameters"]["temperature"]
    assert parsed.parameters.top_p == data["parameters"]["top_p"]
    assert len(parsed.messages) == 3
    assert parsed.messages[1].tool_calls is not None
    assert parsed.messages[1].tool_calls[0].function.name == "web_search"
    assert parsed.messages[2].tool_call_id == "call_1"
    assert parsed.tools is not None
    assert parsed.tools[0].function == data["tools"][0]["function"]


def test_format_response_matches_shared_contract_fixture(mock_worker_config):
    worker = Worker(mock_worker_config)
    server = HTTPServer(worker, mock_worker_config)

    expected = load_fixture("infer_response_tool_calls.json")
    response = InferenceResponse(
        request_id=expected["request_id"],
        model_id=expected["model_id"],
        choices=[
            Choice(
                index=0,
                message=Message(
                    role=Role.ASSISTANT,
                    content="",
                    tool_calls=[
                        ToolCall(
                            id="call_2",
                            type="function",
                            function=FunctionCall(
                                name="web_search",
                                arguments='{"query":"Rust async runtimes"}',
                            ),
                        )
                    ],
                ),
                finish_reason=FinishReason.TOOL_CALLS,
            )
        ],
        usage=UsageStats(prompt_tokens=5, completion_tokens=1, total_tokens=6),
        latency=LatencyStats(queue_ms=1, inference_ms=2, total_ms=3, time_to_first_token_ms=1),
    )

    assert server._format_response(response) == expected


def test_format_stream_chunk_matches_shared_contract_fixture(mock_worker_config):
    worker = Worker(mock_worker_config)
    server = HTTPServer(worker, mock_worker_config)

    expected = load_fixture("infer_stream_chunk_tool_calls.json")
    chunk = TokenChunk(
        request_id=expected["request_id"],
        index=expected["index"],
        delta=expected["delta"],
        finish_reason=FinishReason.TOOL_CALLS,
        usage=UsageStats(prompt_tokens=5, completion_tokens=1, total_tokens=6),
        tool_calls=[
            ToolCallChunkDelta(
                index=0,
                id="call_1",
                type="function",
                function={
                    "name": "web_search",
                    "arguments": '{"query":"Go async runtimes"}',
                },
            )
        ],
    )

    assert server._format_stream_chunk(chunk) == expected
