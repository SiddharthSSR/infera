"""Tests for the benchmark chat helper script."""

from __future__ import annotations

import importlib.util
import json
import sys
from pathlib import Path

import pytest

REPO_ROOT = Path(__file__).resolve().parents[2]
SCRIPT_PATH = REPO_ROOT / "scripts" / "benchmark-chat.py"


def load_benchmark_chat_module():
    spec = importlib.util.spec_from_file_location("benchmark_chat", SCRIPT_PATH)
    assert spec is not None
    module = importlib.util.module_from_spec(spec)
    assert spec.loader is not None
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    return module


def test_parse_args_defaults_to_inferai(monkeypatch):
    module = load_benchmark_chat_module()
    monkeypatch.setattr("sys.argv", ["benchmark-chat.py", "--model", "Qwen/Qwen2.5-7B-Instruct"])
    args = module.parse_args()
    assert args.base_url == "https://inferai.co.in"
    assert args.engine_label == ""
    assert args.provider_label == ""
    assert args.gpu_label == ""
    assert args.preset == "all"
    assert args.concurrency == 1
    assert args.warmup == 0
    assert args.cache_reuse_mode == "none"


def test_conversation_preset_is_available(monkeypatch):
    module = load_benchmark_chat_module()
    monkeypatch.setattr(
        "sys.argv",
        ["benchmark-chat.py", "--model", "Qwen/Qwen2.5-7B-Instruct", "--preset", "conversation"],
    )
    args = module.parse_args()
    assert args.preset == "conversation"
    assert "conversation" in module.PROMPTS
    assert "Latest user turn" in module.PROMPTS["conversation"]


def test_parse_args_accepts_repeatable_headers(monkeypatch):
    module = load_benchmark_chat_module()
    monkeypatch.setattr(
        "sys.argv",
        [
            "benchmark-chat.py",
            "--model",
            "Qwen/Qwen2.5-7B-Instruct",
            "--header",
            "X-Infera-Affinity-Key: chat-123",
            "--header",
            "X-Debug: on",
        ],
    )
    args = module.parse_args()
    assert args.header == ["X-Infera-Affinity-Key: chat-123", "X-Debug: on"]


def test_parse_extra_headers_parses_and_normalizes_values():
    module = load_benchmark_chat_module()
    headers = module.parse_extra_headers(["X-Infera-Affinity-Key: chat-123", "X-Debug: on"])
    assert headers == {"X-Infera-Affinity-Key": "chat-123", "X-Debug": "on"}


def test_parse_extra_headers_rejects_invalid_format():
    module = load_benchmark_chat_module()
    try:
        module.parse_extra_headers(["not-a-header"])
    except ValueError as exc:
        assert "expected 'Name: Value'" in str(exc)
    else:
        raise AssertionError("expected invalid header to raise ValueError")


def test_build_headers_merges_extra_headers():
    module = load_benchmark_chat_module()
    headers = module.build_headers(
        "test-key",
        stream=True,
        extra_headers={"X-Infera-Affinity-Key": "chat-123"},
    )
    assert headers["Authorization"] == "Bearer test-key"
    assert headers["Accept"] == "text/event-stream"
    assert headers["X-Infera-Affinity-Key"] == "chat-123"


def test_build_request_headers_generates_stable_affinity_keys():
    module = load_benchmark_chat_module()

    headers = module.build_request_headers(
        {"X-Debug": "on"},
        "affinity",
        "benchmark",
        "conversation",
        2,
    )

    assert headers == {
        "X-Debug": "on",
        "X-Infera-Affinity-Key": "benchmark:conversation:client-2",
    }


def test_build_result_row_computes_cost_and_throughput():
    module = load_benchmark_chat_module()
    stream = module.StreamResult(ttft_ms=500.0, total_ms=2500.0, content="hello")
    non_stream = module.NonStreamResult(
        total_ms=3000.0,
        prompt_tokens=50,
        completion_tokens=100,
        total_tokens=150,
        content="world",
    )

    row = module.build_result_row(2, 3, 4, stream, non_stream, 0.79)
    rows = [dict(row, client_index=index) for index in range(1, 5)]
    module.annotate_group_metrics(rows, None, 0.79, 6000.0)
    row = rows[2]

    assert row["run"] == 2
    assert row["group_run"] == 2
    assert row["client_index"] == 3
    assert row["concurrency"] == 4
    assert row["ttft_ms"] == 500.0
    assert row["decode_tok_s"] == 50.0
    assert row["total_tok_s"] == 50.0
    assert row["cost_per_request_usd"] == pytest.approx(0.000164583, abs=1e-12)
    assert row["cost_per_paired_sample_usd"] == pytest.approx(0.000329167, abs=1e-12)
    assert row["cost_accuracy"] == "estimated"
    assert row["cost_observed_concurrency"] == 4
    assert row["cost_group_physical_request_count"] == 8
    assert row["cost_group_paired_sample_count"] == 4
    assert row["cost_group_wall_ms"] == 6000.0
    assert row["cost_token_denominator"] == 804
    assert row["cost_token_accuracy"] == "estimated"
    assert row["cost_per_token_usd"] > 0
    assert row["cost_per_1m_tokens_usd"] == row["cost_per_token_usd"] * 1_000_000
    assert row["decode_tok_s_per_dollar_hour"] > 0


def test_summarize_rows_handles_decode_percentiles():
    module = load_benchmark_chat_module()
    rows = [
        {
            "ttft_ms": 400.0,
            "stream_total_ms": 2500.0,
            "non_stream_total_ms": 3000.0,
            "decode_tok_s": 50.0,
        },
        {
            "ttft_ms": 600.0,
            "stream_total_ms": 2700.0,
            "non_stream_total_ms": 3200.0,
            "decode_tok_s": 60.0,
        },
        {
            "ttft_ms": 800.0,
            "stream_total_ms": 2900.0,
            "non_stream_total_ms": 3400.0,
            "decode_tok_s": 0.0,
        },
    ]

    summary = module.summarize_rows(rows)

    assert summary["ttft_p50_ms"] == 600.0
    assert summary["ttft_p95_ms"] == 800.0
    assert summary["stream_total_p50_ms"] == 2700.0
    assert summary["decode_tok_s_p50"] == 55.0
    assert summary["decode_tok_s_p95"] == 60.0


def test_summarize_rows_deduplicates_group_metrics():
    module = load_benchmark_chat_module()
    rows = [
        {
            "run": 1,
            "group_run": 1,
            "ttft_ms": 400.0,
            "stream_total_ms": 2500.0,
            "non_stream_total_ms": 3000.0,
            "decode_tok_s": 50.0,
            "aggregate_decode_tok_s": 90.0,
            "aggregate_total_tok_s": 100.0,
            "contention_ratio": 0.8,
        },
        {
            "run": 1,
            "group_run": 1,
            "ttft_ms": 600.0,
            "stream_total_ms": 2700.0,
            "non_stream_total_ms": 3200.0,
            "decode_tok_s": 60.0,
            "aggregate_decode_tok_s": 90.0,
            "aggregate_total_tok_s": 100.0,
            "contention_ratio": 0.8,
        },
        {
            "run": 2,
            "group_run": 2,
            "ttft_ms": 800.0,
            "stream_total_ms": 2900.0,
            "non_stream_total_ms": 3400.0,
            "decode_tok_s": 70.0,
            "aggregate_decode_tok_s": 110.0,
            "aggregate_total_tok_s": 120.0,
            "contention_ratio": 0.9,
        },
    ]

    summary = module.summarize_rows(rows)

    assert summary["ttft_p99_ms"] == 800.0
    assert summary["aggregate_decode_tok_s_p50"] == 100.0
    assert summary["aggregate_decode_tok_s_p95"] == 110.0
    assert summary["aggregate_total_tok_s_p50"] == 110.0
    assert summary["contention_ratio_p50"] == pytest.approx(0.85)


def test_run_benchmark_group_annotates_concurrent_group_metrics(monkeypatch):
    module = load_benchmark_chat_module()
    seen_headers: list[dict[str, str] | None] = []

    def fake_run_non_stream(*_args, **_kwargs):
        seen_headers.append(_args[-1])
        return module.NonStreamResult(
            total_ms=2000.0,
            prompt_tokens=100,
            completion_tokens=80,
            total_tokens=180,
            content="non-stream",
        )

    def fake_run_stream(*_args, **_kwargs):
        seen_headers.append(_args[-1])
        return module.StreamResult(
            ttft_ms=500.0,
            total_ms=2500.0,
            content="stream",
        )

    monkeypatch.setattr(module, "run_non_stream", fake_run_non_stream)
    monkeypatch.setattr(module, "run_stream", fake_run_stream)
    clock = iter([100.0, 110.0])
    monkeypatch.setattr(module.time, "perf_counter", lambda: next(clock))

    rows = module.run_benchmark_group(
        2,
        2,
        "https://inferai.co.in",
        None,
        "Qwen/Qwen2.5-7B-Instruct",
        "prompt",
        256,
        0.2,
        180,
        None,
        "affinity",
        "benchmark",
        "conversation",
        3.6,
    )

    assert len(rows) == 2
    assert rows[0]["group_run"] == 2
    assert rows[0]["concurrency"] == 2
    assert rows[0]["aggregate_decode_tok_s"] == 80.0
    assert rows[0]["aggregate_total_tok_s"] == 180.0
    assert rows[0]["contention_ratio"] == 1.0
    assert rows[0]["cost_group_wall_ms"] == 10_000.0
    assert rows[0]["cost_group_usd"] == 0.01
    assert rows[0]["cost_group_physical_request_count"] == 4
    assert rows[0]["cost_group_paired_sample_count"] == 2
    assert rows[0]["cost_per_request_usd"] == 0.0025
    assert rows[0]["cost_per_paired_sample_usd"] == 0.005
    assert rows[0]["cost_token_denominator"] == 562
    assert rows[1]["client_index"] == 2
    assert seen_headers == [
        {"X-Infera-Affinity-Key": "benchmark:conversation:client-1"},
        {"X-Infera-Affinity-Key": "benchmark:conversation:client-2"},
        {"X-Infera-Affinity-Key": "benchmark:conversation:client-1"},
        {"X-Infera-Affinity-Key": "benchmark:conversation:client-2"},
    ]


@pytest.mark.parametrize("value", [0.0, -1.0, float("nan"), float("inf"), float("-inf")])
def test_invalid_cost_per_hour_is_unavailable(value):
    module = load_benchmark_chat_module()
    stream = module.StreamResult(ttft_ms=10.0, total_ms=20.0, content="stream")
    non_stream = module.NonStreamResult(
        total_ms=20.0,
        prompt_tokens=10,
        completion_tokens=5,
        total_tokens=15,
        content="non-stream",
    )
    row = module.build_result_row(1, 1, 1, stream, non_stream, value)
    module.annotate_group_metrics([row], None, value, 40.0)

    assert row["cost_accuracy"] == "unavailable"
    assert "cost_per_request_usd" not in row
    assert "cost_per_token_usd" not in row
    payload = module.build_output_payload(
        "https://inferai.co.in",
        "model",
        "",
        "",
        "",
        1,
        1,
        0,
        "none",
        {"short": [row]},
        value,
    )
    assert payload["cost_per_hour"] is None
    assert payload["price_snapshot"] == {"accuracy": "unavailable"}


@pytest.mark.parametrize("group_wall_ms", [0.0, -1.0, float("nan"), float("inf")])
def test_invalid_group_timing_suppresses_cost_evidence(group_wall_ms):
    module = load_benchmark_chat_module()
    stream = module.StreamResult(ttft_ms=10.0, total_ms=20.0, content="stream")
    non_stream = module.NonStreamResult(
        total_ms=20.0,
        prompt_tokens=10,
        completion_tokens=5,
        total_tokens=15,
        content="non-stream",
    )
    row = module.build_result_row(1, 1, 1, stream, non_stream, 1.0)
    module.annotate_group_metrics([row], None, 1.0, group_wall_ms)

    assert row["cost_accuracy"] == "unavailable"
    assert "price_amount" not in row
    assert "cost_per_request_usd" not in row
    assert "cost_per_token_usd" not in row


@pytest.mark.parametrize("value", ["0", "-1", "nan", "inf", "-inf"])
def test_cli_rejects_invalid_cost_per_hour(value):
    module = load_benchmark_chat_module()
    with pytest.raises(module.argparse.ArgumentTypeError):
        module.positive_finite_float(value)


def test_write_json_output_creates_parent_directories(tmp_path):
    module = load_benchmark_chat_module()
    payload = module.build_output_payload(
        "https://inferai.co.in",
        "Qwen/Qwen2.5-7B-Instruct",
        "vllm",
        "runpod",
        "A100_80GB",
        3,
        4,
        2,
        "affinity",
        {"medium": [{"run": 1, "ttft_ms": 400.0}]},
        0.79,
    )
    output = tmp_path / "nested" / "bench" / "result.json"

    written_path = module.write_json_output(str(output), payload)

    assert written_path == output
    assert output.exists()
    assert payload["engine"] == "vllm"
    assert payload["provider"] == "runpod"
    assert payload["gpu_type"] == "A100_80GB"
    assert payload["price_snapshot"] == {
        "version": "benchmark-cli-hourly-v1",
        "amount": 0.79,
        "currency": "USD",
        "time_unit": "hour",
        "accuracy": "estimated",
        "attribution_method": "active_instance_group_time_share_v1",
    }
    assert json.loads(output.read_text(encoding="utf-8")) == payload
